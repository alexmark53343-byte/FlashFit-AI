//go:build windows

package main

import (
	"bytes"
	_ "embed"
	"image/color"
	"image/png"
	"syscall"
	"unsafe"
)

// Rendered from the official 3DBenchy mesh and embedded inside the PE.
//
//go:embed benchy_spatial.png
var spatialBenchyPNG []byte

type spatialBitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type spatialBitmapInfo struct {
	Header spatialBitmapInfoHeader
	Colors [1]uint32
}

var (
	hSpatialBenchy      uintptr
	spatialBenchyWidth  int32
	spatialBenchyHeight int32
	pCreateDIBSection   = gdi32.NewProc("CreateDIBSection")
	pCreateCompatibleDC = gdi32.NewProc("CreateCompatibleDC")
	pDeleteDC           = gdi32.NewProc("DeleteDC")
	spatialMSImg32      = syscall.NewLazyDLL("msimg32.dll")
	pAlphaBlend         = spatialMSImg32.NewProc("AlphaBlend")
	pGradientFill       = spatialMSImg32.NewProc("GradientFill")
)

func initSpatialBenchyAsset() {
	decoded, err := png.Decode(bytes.NewReader(spatialBenchyPNG))
	if err != nil {
		return
	}
	bounds := decoded.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w <= 0 || h <= 0 || w > 4096 || h > 4096 {
		return
	}

	info := spatialBitmapInfo{Header: spatialBitmapInfoHeader{
		Size: uint32(unsafe.Sizeof(spatialBitmapInfoHeader{})), Width: int32(w), Height: -int32(h),
		Planes: 1, BitCount: 32, Compression: 0, SizeImage: uint32(w * h * 4),
	}}
	var pixelMemory unsafe.Pointer
	bitmap, _, _ := pCreateDIBSection.Call(0, uintptr(unsafe.Pointer(&info)), 0, uintptr(unsafe.Pointer(&pixelMemory)), 0, 0)
	if bitmap == 0 || pixelMemory == nil {
		return
	}

	pixels := unsafe.Slice((*byte)(pixelMemory), w*h*4)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.NRGBAModel.Convert(decoded.At(bounds.Min.X+x, bounds.Min.Y+y)).(color.NRGBA)
			i := (y*w + x) * 4
			a := uint16(c.A)
			pixels[i+0] = uint8(uint16(c.B) * a / 255)
			pixels[i+1] = uint8(uint16(c.G) * a / 255)
			pixels[i+2] = uint8(uint16(c.R) * a / 255)
			pixels[i+3] = c.A
		}
	}
	hSpatialBenchy = bitmap
	spatialBenchyWidth, spatialBenchyHeight = int32(w), int32(h)
}

func cleanupSpatialBenchyAsset() {
	if hSpatialBenchy != 0 {
		pDeleteObject.Call(hSpatialBenchy)
		hSpatialBenchy = 0
	}
}

func drawSpatialBenchyAsset(hdc uintptr, cx, cy, size int32) bool {
	if hSpatialBenchy == 0 || spatialBenchyWidth <= 0 || spatialBenchyHeight <= 0 {
		return false
	}
	memoryDC, _, _ := pCreateCompatibleDC.Call(hdc)
	if memoryDC == 0 {
		return false
	}
	oldBitmap, _, _ := pSelectObject.Call(memoryDC, hSpatialBenchy)
	destinationWidth := size * 3
	destinationHeight := destinationWidth * spatialBenchyHeight / spatialBenchyWidth
	destinationX := cx - destinationWidth/2
	destinationY := cy - destinationHeight/2
	blend := uintptr(0x01ff0000)
	ok, _, _ := pAlphaBlend.Call(
		hdc,
		i32arg(destinationX), i32arg(destinationY), uintptr(destinationWidth), uintptr(destinationHeight),
		memoryDC, 0, 0, uintptr(spatialBenchyWidth), uintptr(spatialBenchyHeight), blend,
	)
	pSelectObject.Call(memoryDC, oldBitmap)
	pDeleteDC.Call(memoryDC)
	return ok != 0
}
