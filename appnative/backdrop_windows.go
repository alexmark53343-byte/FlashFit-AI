//go:build windows

package main

import "math"

// The window backdrop is a field of coloured light rather than a flat fill: a
// handful of wide, soft blobs summed per pixel. It is what gives the light
// theme its iridescence and the dark theme its depth.
//
// It also makes frosted panels honest. Real frosted glass shows a blurred
// version of what is behind it, and blurring this field returns very nearly the
// field itself — it has no high frequencies to lose. So a panel can be filled
// by sampling the same field it covers and lifting it toward the surface
// colour, which is what the blur would have produced anyway.

type backdropBlob struct {
	// Position as a fraction of the window, radii likewise.
	X, Y, RX, RY float64
	R, G, B      float64
	Strength     float64
}

type backdropKey struct {
	Width, Height int32
	Dark          bool
	Accent        uint32
}

var (
	backdropCache  = map[backdropKey]spatialCachedBitmap{}
	backdropBlobsL = []backdropBlob{
		{X: 0.16, Y: 0.10, RX: 0.52, RY: 0.46, R: 255, G: 208, B: 224, Strength: 0.85}, // rose
		{X: 0.78, Y: 0.06, RX: 0.46, RY: 0.40, R: 255, G: 232, B: 205, Strength: 0.70}, // warm
		{X: 0.94, Y: 0.44, RX: 0.44, RY: 0.52, R: 205, G: 214, B: 255, Strength: 0.85}, // periwinkle
		{X: 0.52, Y: 0.92, RX: 0.60, RY: 0.44, R: 214, G: 203, B: 255, Strength: 0.80}, // lavender
		{X: 0.06, Y: 0.72, RX: 0.42, RY: 0.46, R: 200, G: 226, B: 255, Strength: 0.70}, // sky
	}
	backdropBlobsD = []backdropBlob{
		{X: 0.18, Y: 0.08, RX: 0.55, RY: 0.48, R: 74, G: 48, B: 122, Strength: 0.60},
		{X: 0.82, Y: 0.10, RX: 0.48, RY: 0.42, R: 26, G: 52, B: 116, Strength: 0.55},
		{X: 0.92, Y: 0.52, RX: 0.44, RY: 0.50, R: 96, G: 40, B: 96, Strength: 0.40},
		{X: 0.48, Y: 0.95, RX: 0.62, RY: 0.46, R: 44, G: 36, B: 104, Strength: 0.55},
		{X: 0.04, Y: 0.78, RX: 0.40, RY: 0.44, R: 20, G: 56, B: 104, Strength: 0.42},
	}
)

func backdropBlobs() []backdropBlob {
	if th.dark {
		return backdropBlobsD
	}
	return backdropBlobsL
}

func backdropBase() (float64, float64, float64) {
	if th.dark {
		return 9, 11, 18
	}
	return 236, 238, 247
}

// backdropSample returns the backdrop colour at a point, in window coordinates.
// Panels use it to work out what a blur of their background would look like.
func backdropSample(x, y, w, h int32) (float64, float64, float64) {
	if w <= 0 || h <= 0 {
		return backdropBase()
	}
	fx := float64(x) / float64(w)
	fy := float64(y) / float64(h)
	r, g, b := backdropBase()
	for _, blob := range backdropBlobs() {
		dx := (fx - blob.X) / blob.RX
		dy := (fy - blob.Y) / blob.RY
		d2 := dx*dx + dy*dy
		if d2 >= 1 {
			continue
		}
		// Smooth, wide falloff: no visible edge where a blob ends.
		falloff := (1 - d2) * (1 - d2)
		weight := falloff * blob.Strength
		if th.dark {
			// Dark theme adds light; light theme tints an already bright base.
			r += blob.R * weight
			g += blob.G * weight
			b += blob.B * weight
			continue
		}
		r = r*(1-weight) + blob.R*weight
		g = g*(1-weight) + blob.G*weight
		b = b*(1-weight) + blob.B*weight
	}
	return spatialClamp(r, 0, 255), spatialClamp(g, 0, 255), spatialClamp(b, 0, 255)
}

// backdropColorAt packs a sample into a COLORREF for the drawing helpers.
func backdropColorAt(x, y, w, h int32) uintptr {
	r, g, b := backdropSample(x, y, w, h)
	return rgb(uint8(r), uint8(g), uint8(b))
}

// frostedStops returns the two gradient stops a panel should use to read as
// frosted glass over the backdrop: the field beneath it, lifted toward the
// surface colour by the glass's own opacity.
func frostedStops(r rect, w, h int32, opacity float64) (uintptr, uintptr) {
	cx := (r.Left + r.Right) / 2
	topR, topG, topB := backdropSample(cx, r.Top, w, h)
	botR, botG, botB := backdropSample(cx, r.Bottom, w, h)
	sr, sg, sb := spatialColorChannels(th.surface)
	ar, ag, ab := spatialColorChannels(th.surfaceAlt)

	mix := func(fieldR, fieldG, fieldB float64, tr, tg, tb uint8) uintptr {
		return rgb(
			uint8(spatialClamp(fieldR*(1-opacity)+float64(tr)*opacity, 0, 255)),
			uint8(spatialClamp(fieldG*(1-opacity)+float64(tg)*opacity, 0, 255)),
			uint8(spatialClamp(fieldB*(1-opacity)+float64(tb)*opacity, 0, 255)),
		)
	}
	return mix(topR, topG, topB, sr, sg, sb), mix(botR, botG, botB, ar, ag, ab)
}

func backdropBitmap(w, h int32) spatialCachedBitmap {
	key := backdropKey{Width: w, Height: h, Dark: th.dark, Accent: uint32(th.accent)}
	if bitmap, ok := backdropCache[key]; ok {
		return bitmap
	}
	// Only one backdrop is ever useful at a time; drop the old size on resize.
	clearBackdropCache()
	bitmap := createSpatialBitmap(w, h, func(x, y int32) (uint8, uint8, uint8, uint8) {
		r, g, b := backdropSample(x, y, w, h)
		// A touch of ordered grain stops wide gradients from banding on 8-bit.
		grain := float64((int32(x)*7+int32(y)*13)%3) - 1
		return uint8(spatialClamp(r+grain, 0, 255)),
			uint8(spatialClamp(g+grain, 0, 255)),
			uint8(spatialClamp(b+grain, 0, 255)), 255
	})
	backdropCache[key] = bitmap
	return bitmap
}

// Every scene fills its canvas first, so this is the one place that reliably
// knows which window is being painted and how big it is. Panels sample the
// field against these dimensions, which keeps a picker's frost aligned with the
// picker's own backdrop rather than the main window's.
var backdropViewportW, backdropViewportH int32

func drawBackdrop(hdc uintptr, client rect) bool {
	w, h := width(client), height(client)
	if w <= 0 || h <= 0 {
		return false
	}
	backdropViewportW, backdropViewportH = w, h
	return drawSpatialCachedBitmap(hdc, backdropBitmap(w, h), 0, 0)
}

func clearBackdropCache() {
	for _, bitmap := range backdropCache {
		if bitmap.Handle != 0 {
			pDeleteObject.Call(bitmap.Handle)
		}
	}
	backdropCache = map[backdropKey]spatialCachedBitmap{}
}

// backdropIsSmooth reports whether math.Abs is worth trusting here; kept as a
// guard so a degenerate window size cannot produce NaN stops.
func backdropUsable(w, h int32) bool {
	return w > 0 && h > 0 && !math.IsNaN(float64(w)) && !math.IsNaN(float64(h))
}
