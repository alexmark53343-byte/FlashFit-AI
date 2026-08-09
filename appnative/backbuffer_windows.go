//go:build windows

package main

// windowBackBuffer keeps one compatible bitmap alive between WM_PAINT calls.
// Recreating a full-window GDI bitmap for every animation frame caused heavy
// allocation pressure and could starve the Win32 message queue on slower PCs.
type windowBackBuffer struct {
	dc, bitmap, previous uintptr
	width, height        int32
}

func (b *windowBackBuffer) ensure(hdc uintptr, width, height int32) bool {
	if hdc == 0 || width <= 0 || height <= 0 {
		return false
	}
	if b.dc != 0 && b.bitmap != 0 && b.width == width && b.height == height {
		return true
	}
	b.reset()
	dc, _, _ := pCreateCompatibleDC.Call(hdc)
	if dc == 0 {
		return false
	}
	bitmap, _, _ := pCreateCompatibleBitmap.Call(hdc, uintptr(width), uintptr(height))
	if bitmap == 0 {
		pDeleteDC.Call(dc)
		return false
	}
	previous, _, _ := pSelectObject.Call(dc, bitmap)
	b.dc, b.bitmap, b.previous = dc, bitmap, previous
	b.width, b.height = width, height
	return true
}

func (b *windowBackBuffer) reset() {
	if b.dc != 0 && b.previous != 0 {
		pSelectObject.Call(b.dc, b.previous)
	}
	if b.bitmap != 0 {
		pDeleteObject.Call(b.bitmap)
	}
	if b.dc != 0 {
		pDeleteDC.Call(b.dc)
	}
	*b = windowBackBuffer{}
}
