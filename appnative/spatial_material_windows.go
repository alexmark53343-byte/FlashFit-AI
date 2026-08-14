//go:build windows

package main

import (
	"math"
	"unsafe"
)

// This file is the software material compositor for the Windows frontend.
// It gives the native Win32 app antialiased geometry, Gaussian shadows and
// atmospheric light without shipping a browser or a heavyweight UI runtime.

type spatialMaterialKey struct {
	Width, Height, Radius int32
	Top, Bottom, Stroke   uint32
}

type spatialShadowKey struct {
	Width, Height, Radius int32
	Blur                  int32
	Color                 uint32
	Opacity               uint8
}

type spatialGlowKey struct {
	RadiusX, RadiusY int32
	Color            uint32
	Opacity          uint8
}

type spatialCachedBitmap struct {
	Handle        uintptr
	Width, Height int32
}

const spatialMaterialCacheLimit = 384

var (
	spatialMaterialCache = map[spatialMaterialKey]spatialCachedBitmap{}
	spatialShadowCache   = map[spatialShadowKey]spatialCachedBitmap{}
	spatialGlowCache     = map[spatialGlowKey]spatialCachedBitmap{}
)

func spatialColorChannels(value uintptr) (uint8, uint8, uint8) {
	return uint8(value & 0xff), uint8((value >> 8) & 0xff), uint8((value >> 16) & 0xff)
}

func isDarkMaterial(r, g, b uint8) bool {
	return int(r)+int(g)+int(b) < 300
}

// A cheap deterministic dither, ±1 level. Enough to break up the flat bands a
// smooth gradient shows on 8-bit displays, invisible as texture.
func spatialGrain(x, y int32) float64 {
	h := uint32(x)*374761393 + uint32(y)*668265263
	h = (h ^ (h >> 13)) * 1274126177
	return float64(int32((h>>17)&3)) * 0.5
}

func spatialClamp(value, low, high float64) float64 {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func spatialRoundedDistance(x, y, width, height, radius float64) float64 {
	maxRadius := math.Min(width, height) * 0.5
	radius = spatialClamp(radius, 0, maxRadius)
	qx := math.Abs(x-width*0.5) - (width*0.5 - radius)
	qy := math.Abs(y-height*0.5) - (height*0.5 - radius)
	outside := math.Hypot(math.Max(qx, 0), math.Max(qy, 0))
	inside := math.Min(math.Max(qx, qy), 0)
	return outside + inside - radius
}

func spatialCoverage(distance float64) float64 {
	return spatialClamp(0.5-distance, 0, 1)
}

func createSpatialBitmap(width, height int32, render func(x, y int32) (uint8, uint8, uint8, uint8)) spatialCachedBitmap {
	if width <= 0 || height <= 0 {
		return spatialCachedBitmap{}
	}
	info := spatialBitmapInfo{Header: spatialBitmapInfoHeader{
		Size:        uint32(unsafe.Sizeof(spatialBitmapInfoHeader{})),
		Width:       width,
		Height:      -height,
		Planes:      1,
		BitCount:    32,
		Compression: 0,
		SizeImage:   uint32(width * height * 4),
	}}
	var memory unsafe.Pointer
	bitmap, _, _ := pCreateDIBSection.Call(0, uintptr(unsafe.Pointer(&info)), 0, uintptr(unsafe.Pointer(&memory)), 0, 0)
	if bitmap == 0 || memory == nil {
		return spatialCachedBitmap{}
	}
	pixels := unsafe.Slice((*byte)(memory), int(width*height*4))
	for y := int32(0); y < height; y++ {
		for x := int32(0); x < width; x++ {
			r, g, b, a := render(x, y)
			i := int((y*width + x) * 4)
			alpha := uint16(a)
			pixels[i+0] = uint8(uint16(b) * alpha / 255)
			pixels[i+1] = uint8(uint16(g) * alpha / 255)
			pixels[i+2] = uint8(uint16(r) * alpha / 255)
			pixels[i+3] = a
		}
	}
	return spatialCachedBitmap{Handle: bitmap, Width: width, Height: height}
}

// One memory DC, reused for every composite.
//
// Each rounded surface, shadow and glow is a cached bitmap that has to be
// selected into a DC to be blended. Creating and destroying that DC per surface
// meant thousands of GDI object operations a second on a busy frame — more
// expensive than the blending itself. The DC holds no state worth keeping
// between blits, so one is made on first use and kept.
var compositeDC uintptr

func releaseCompositeDC() {
	if compositeDC != 0 {
		pDeleteDC.Call(compositeDC)
		compositeDC = 0
	}
}

func drawSpatialCachedBitmap(hdc uintptr, bitmap spatialCachedBitmap, x, y int32) bool {
	if bitmap.Handle == 0 || bitmap.Width <= 0 || bitmap.Height <= 0 {
		return false
	}
	if compositeDC == 0 {
		compositeDC, _, _ = pCreateCompatibleDC.Call(hdc)
		if compositeDC == 0 {
			return false
		}
	}
	oldBitmap, _, _ := pSelectObject.Call(compositeDC, bitmap.Handle)
	blend := uintptr(0x01ff0000) // AC_SRC_OVER, constant alpha 255, source alpha.
	ok, _, _ := pAlphaBlend.Call(
		hdc,
		i32arg(x), i32arg(y), uintptr(bitmap.Width), uintptr(bitmap.Height),
		compositeDC,
		0, 0, uintptr(bitmap.Width), uintptr(bitmap.Height),
		blend,
	)
	// The bitmap must not stay selected: it may be deleted when a cache is
	// dropped, and a selected object cannot be freed.
	pSelectObject.Call(compositeDC, oldBitmap)
	return ok != 0
}

func drawSpatialRoundedMaterial(hdc uintptr, r rect, radius int32, top, bottom, stroke uintptr) {
	width, height := width(r), height(r)
	if width <= 0 || height <= 0 {
		return
	}
	key := spatialMaterialKey{
		Width: width, Height: height, Radius: radius,
		Top: uint32(top), Bottom: uint32(bottom), Stroke: uint32(stroke),
	}
	bitmap, ok := spatialMaterialCache[key]
	if !ok {
		// Every distinct size/colour pair costs one GDI bitmap. Hover tints and
		// resizes mint new pairs, so the cache is capped rather than trusted to
		// stay small.
		if len(spatialMaterialCache) > spatialMaterialCacheLimit {
			cleanupSpatialMaterialSystem()
		}
		tr, tg, tb := spatialColorChannels(top)
		br, bg, bb := spatialColorChannels(bottom)
		sr, sg, sb := spatialColorChannels(stroke)
		// A specular top edge sells the "pane of glass" read: light catches the
		// upper lip of a raised surface and falls away toward the bottom.
		highlight := 26.0
		if isDarkMaterial(tr, tg, tb) {
			highlight = 20.0
		}
		bitmap = createSpatialBitmap(width, height, func(x, y int32) (uint8, uint8, uint8, uint8) {
			fx, fy := float64(x)+0.5, float64(y)+0.5
			outer := spatialCoverage(spatialRoundedDistance(fx, fy, float64(width), float64(height), float64(radius)))
			if outer <= 0 {
				return 0, 0, 0, 0
			}
			t := 0.0
			if height > 1 {
				t = float64(y) / float64(height-1)
			}
			// Ease the gradient so the midtone sits where the eye expects it.
			eased := t * t * (3 - 2*t)
			sheen := 5.0 * math.Pow(1-t, 3)
			rv := float64(tr)*(1-eased) + float64(br)*eased + sheen
			gv := float64(tg)*(1-eased) + float64(bg)*eased + sheen
			bv := float64(tb)*(1-eased) + float64(bb)*eased + sheen

			inner := 0.0
			if width > 3 && height > 3 {
				inner = spatialCoverage(spatialRoundedDistance(fx-1, fy-1, float64(width-2), float64(height-2), math.Max(float64(radius-1), 0)))
			}
			border := spatialClamp((outer-inner)*0.72, 0, 1)
			rv = rv*(1-border) + float64(sr)*border
			gv = gv*(1-border) + float64(sg)*border
			bv = bv*(1-border) + float64(sb)*border

			// Inner light: a bright hairline just inside the top edge.
			if height > 6 {
				lip := 0.0
				if width > 5 {
					lip = spatialCoverage(spatialRoundedDistance(fx-1.5, fy-1.5, float64(width-3), float64(height-3), math.Max(float64(radius)-1.5, 0)))
				}
				rim := spatialClamp((inner-lip)*math.Pow(1-t, 1.6), 0, 1)
				rv += highlight * rim
				gv += highlight * rim
				bv += highlight * rim
			}

			// A trace of grain keeps wide gradients from banding on 8-bit panels.
			grain := spatialGrain(x, y)
			rv += grain
			gv += grain
			bv += grain

			alpha := uint8(spatialClamp(outer*252, 0, 255))
			return uint8(spatialClamp(rv, 0, 255)), uint8(spatialClamp(gv, 0, 255)), uint8(spatialClamp(bv, 0, 255)), alpha
		})
		spatialMaterialCache[key] = bitmap
	}
	drawSpatialCachedBitmap(hdc, bitmap, r.Left, r.Top)
}

func drawSpatialSoftShadow(hdc uintptr, r rect, radius, blur, offset int32, color uintptr, opacity uint8) {
	if blur < 1 || width(r) <= 0 || height(r) <= 0 || opacity == 0 {
		return
	}
	pad := blur * 3
	key := spatialShadowKey{
		Width: width(r), Height: height(r), Radius: radius,
		Blur: blur, Color: uint32(color), Opacity: opacity,
	}
	bitmap, ok := spatialShadowCache[key]
	if !ok {
		red, green, blue := spatialColorChannels(color)
		canvasW := width(r) + pad*2
		canvasH := height(r) + pad*2
		sigma := float64(blur)
		bitmap = createSpatialBitmap(canvasW, canvasH, func(x, y int32) (uint8, uint8, uint8, uint8) {
			distance := spatialRoundedDistance(
				float64(x-pad)+0.5,
				float64(y-pad)+0.5,
				float64(width(r)),
				float64(height(r)),
				float64(radius),
			)
			outside := math.Max(distance, 0)
			alpha := float64(opacity) * math.Exp(-(outside*outside)/(2*sigma*sigma))
			return red, green, blue, uint8(spatialClamp(alpha, 0, 255))
		})
		spatialShadowCache[key] = bitmap
	}
	drawSpatialCachedBitmap(hdc, bitmap, r.Left-pad, r.Top-pad+offset)
}

func drawSpatialGlow(hdc uintptr, centerX, centerY, radiusX, radiusY int32, color uintptr, opacity uint8) {
	if radiusX <= 0 || radiusY <= 0 || opacity == 0 {
		return
	}
	key := spatialGlowKey{RadiusX: radiusX, RadiusY: radiusY, Color: uint32(color), Opacity: opacity}
	bitmap, ok := spatialGlowCache[key]
	if !ok {
		red, green, blue := spatialColorChannels(color)
		bitmap = createSpatialBitmap(radiusX*2, radiusY*2, func(x, y int32) (uint8, uint8, uint8, uint8) {
			dx := (float64(x) + 0.5 - float64(radiusX)) / float64(radiusX)
			dy := (float64(y) + 0.5 - float64(radiusY)) / float64(radiusY)
			distanceSquared := dx*dx + dy*dy
			if distanceSquared >= 1 {
				return 0, 0, 0, 0
			}
			falloff := math.Pow(1-distanceSquared, 2.3)
			return red, green, blue, uint8(spatialClamp(float64(opacity)*falloff, 0, 255))
		})
		spatialGlowCache[key] = bitmap
	}
	drawSpatialCachedBitmap(hdc, bitmap, centerX-radiusX, centerY-radiusY)
}

func cleanupSpatialMaterialSystem() {
	for _, cache := range []map[spatialMaterialKey]spatialCachedBitmap{spatialMaterialCache} {
		for _, bitmap := range cache {
			if bitmap.Handle != 0 {
				pDeleteObject.Call(bitmap.Handle)
			}
		}
	}
	for _, bitmap := range spatialShadowCache {
		if bitmap.Handle != 0 {
			pDeleteObject.Call(bitmap.Handle)
		}
	}
	for _, bitmap := range spatialGlowCache {
		if bitmap.Handle != 0 {
			pDeleteObject.Call(bitmap.Handle)
		}
	}
	spatialMaterialCache = map[spatialMaterialKey]spatialCachedBitmap{}
	spatialShadowCache = map[spatialShadowKey]spatialCachedBitmap{}
	spatialGlowCache = map[spatialGlowKey]spatialCachedBitmap{}
}
