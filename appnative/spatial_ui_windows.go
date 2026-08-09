//go:build windows

package main

import (
	"fmt"
	"math"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

const (
	WM_PAINT         = 0x000F
	WM_ERASEBKGND    = 0x0014
	WM_LBUTTONUP     = 0x0202
	WM_GETMINMAXINFO = 0x0024
	WM_SETFOCUS      = 0x0007
	WM_TIMER         = 0x0113

	TRANSPARENT = 1
	PS_SOLID    = 0
	PS_DASH     = 1
	NULL_BRUSH  = 5
	SRCCOPY     = 0x00CC0020

	DT_TOP          = 0x00000000
	DT_LEFT         = 0x00000000
	DT_CENTER       = 0x00000001
	DT_RIGHT        = 0x00000002
	DT_VCENTER      = 0x00000004
	DT_WORDBREAK    = 0x00000010
	DT_SINGLELINE   = 0x00000020
	DT_END_ELLIPSIS = 0x00008000

	FW_NORMAL   = 400
	FW_MEDIUM   = 500
	FW_SEMIBOLD = 600
	FW_BOLD     = 700

	SW_SHOWNORMAL   = 1
	EM_SETCUEBANNER = 0x1501

	idLanguageIT = 2001
	idLanguageEN = 2002
	idLanguageFR = 2003
	idLanguageES = 2004
	idLanguageDE = 2005

	idAdvancedScan   = 3001
	idAdvancedManual = 3002
	idAdvancedLog    = 3003

	idFilamentSearch   = 4001
	idFilamentResults  = 4002
	idFilamentApply    = 4003
	idFilamentClose    = 4004
	idSpatialAnimation = 5101
)

type paintStruct struct {
	Hdc         uintptr
	Erase       int32
	Paint       rect
	Restore     int32
	IncUpdate   int32
	RGBReserved [32]byte
}

type minMaxInfo struct {
	Reserved     point
	MaxSize      point
	MaxPosition  point
	MinTrackSize point
	MaxTrackSize point
}

type spatialRegions struct {
	language rect
	advanced rect
	device   rect
	preview  rect
	model    rect
	filament rect
	quality  rect
	open     rect
	fast     rect
	balanced rect
	perfect  rect
}

var (
	spatial                                                                              spatialRegions
	hFontLogo, hFontTitle, hFontHeading, hFontBody, hFontSmall, hFontButton, hFontNumber uintptr
	hAppIcon                                                                             uintptr
	hFilamentDialog, hFilamentSearchLabel, hFilamentApply, hFilamentClose                uintptr
	spatialAnimationTick                                                                 uint32
)

var (
	pBeginPaint      = user32.NewProc("BeginPaint")
	pEndPaint        = user32.NewProc("EndPaint")
	pInvalidateRect  = user32.NewProc("InvalidateRect")
	pDrawText        = user32.NewProc("DrawTextW")
	pFillRect        = user32.NewProc("FillRect")
	pGetCursorPos    = user32.NewProc("GetCursorPos")
	pCreatePopupMenu = user32.NewProc("CreatePopupMenu")
	pAppendMenu      = user32.NewProc("AppendMenuW")
	pTrackPopupMenu  = user32.NewProc("TrackPopupMenu")
	pDestroyMenu     = user32.NewProc("DestroyMenu")
	pSetFocus        = user32.NewProc("SetFocus")
	pDrawIconEx      = user32.NewProc("DrawIconEx")
	pSetTimer        = user32.NewProc("SetTimer")
	pKillTimer       = user32.NewProc("KillTimer")

	pCreateSolidBrush       = gdi32.NewProc("CreateSolidBrush")
	pCreatePen              = gdi32.NewProc("CreatePen")
	pSelectObject           = gdi32.NewProc("SelectObject")
	pDeleteObject           = gdi32.NewProc("DeleteObject")
	pRoundRect              = gdi32.NewProc("RoundRect")
	pEllipse                = gdi32.NewProc("Ellipse")
	pMoveToEx               = gdi32.NewProc("MoveToEx")
	pLineTo                 = gdi32.NewProc("LineTo")
	pPolygon                = gdi32.NewProc("Polygon")
	pSetBkMode              = gdi32.NewProc("SetBkMode")
	pSetTextColor           = gdi32.NewProc("SetTextColor")
	pCreateFont             = gdi32.NewProc("CreateFontW")
	pCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	pBitBlt                 = gdi32.NewProc("BitBlt")
	pSaveDC                 = gdi32.NewProc("SaveDC")
	pRestoreDC              = gdi32.NewProc("RestoreDC")
	pCreateRoundRectRgn     = gdi32.NewProc("CreateRoundRectRgn")
	pSelectClipRgn          = gdi32.NewProc("SelectClipRgn")

	dwmapi            = syscall.NewLazyDLL("dwmapi.dll")
	pDwmSetWindowAttr = dwmapi.NewProc("DwmSetWindowAttribute")
)

func rgb(r, g, b uint8) uintptr { return uintptr(r) | uintptr(g)<<8 | uintptr(b)<<16 }

func i32arg(v int32) uintptr { return uintptr(uint32(v)) }

func createUIFont(height int32, weight uintptr) uintptr {
	face := utf16Ptr("Segoe UI Variable Text")
	h, _, _ := pCreateFont.Call(i32arg(-height), 0, 0, 0, weight, 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(face)))
	if h == 0 {
		face = utf16Ptr("Segoe UI")
		h, _, _ = pCreateFont.Call(i32arg(-height), 0, 0, 0, weight, 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(face)))
	}
	return h
}

func initSpatialUI(hwnd uintptr) {
	loadUILanguage()
	hFontLogo = createUIFont(19, FW_SEMIBOLD)
	hFontTitle = createUIFont(23, FW_SEMIBOLD)
	hFontHeading = createUIFont(16, FW_MEDIUM)
	hFontBody = createUIFont(15, FW_NORMAL)
	hFontSmall = createUIFont(12, FW_NORMAL)
	hFontButton = createUIFont(14, FW_MEDIUM)
	hFontNumber = createUIFont(17, FW_SEMIBOLD)
	initSpatialBenchyAsset()
	hFont = hFontBody
	hAppIcon, _, _ = pLoadIcon.Call(appInstance, 1)
	setSpatialTitle()
	// Rounded native Windows 11 corners. Unsupported attributes are harmless.
	corner := uint32(2) // DWMWCP_ROUND
	pDwmSetWindowAttr.Call(hwnd, 33, uintptr(unsafe.Pointer(&corner)), unsafe.Sizeof(corner))
	dark := uint32(0)
	pDwmSetWindowAttr.Call(hwnd, 20, uintptr(unsafe.Pointer(&dark)), unsafe.Sizeof(dark))
	caption := uint32(rgb(247, 249, 253))
	captionText := uint32(rgb(24, 29, 40))
	border := uint32(rgb(211, 218, 234))
	backdrop := uint32(2)
	pDwmSetWindowAttr.Call(hwnd, 35, uintptr(unsafe.Pointer(&caption)), unsafe.Sizeof(caption))
	pDwmSetWindowAttr.Call(hwnd, 36, uintptr(unsafe.Pointer(&captionText)), unsafe.Sizeof(captionText))
	pDwmSetWindowAttr.Call(hwnd, 34, uintptr(unsafe.Pointer(&border)), unsafe.Sizeof(border))
	pDwmSetWindowAttr.Call(hwnd, 38, uintptr(unsafe.Pointer(&backdrop)), unsafe.Sizeof(backdrop))
	// 30 fps is enough for restrained micro-motion and keeps CPU use minimal.
	pSetTimer.Call(hwnd, idSpatialAnimation, 33, 0)
}

func cleanupSpatialUI() {
	if mainHwnd != 0 {
		pKillTimer.Call(mainHwnd, idSpatialAnimation)
	}
	cleanupSpatialBenchyAsset()
	cleanupSpatialMaterialSystem()
	for _, h := range []uintptr{hFontLogo, hFontTitle, hFontHeading, hFontBody, hFontSmall, hFontButton, hFontNumber} {
		if h != 0 {
			pDeleteObject.Call(h)
		}
	}
}

func setSpatialTitle() {
	setText(mainHwnd, "FlashFit AI")
}

func invalidateSpatial() {
	if mainHwnd != 0 {
		pInvalidateRect.Call(mainHwnd, 0, 0)
	}
}

func inset(r rect, amount int32) rect {
	return rect{r.Left + amount, r.Top + amount, r.Right - amount, r.Bottom - amount}
}

func width(r rect) int32  { return r.Right - r.Left }
func height(r rect) int32 { return r.Bottom - r.Top }

func contains(r rect, x, y int32) bool {
	return x >= r.Left && x < r.Right && y >= r.Top && y < r.Bottom
}

func calculateSpatialLayout(w, h int32) spatialRegions {
	margin := int32(52)
	if w < 1120 {
		margin = 30
	}
	previewTop := int32(92)
	previewBottom := h - 250
	if previewBottom < 450 {
		previewBottom = 450
	}
	previewW := w * 76 / 100
	if previewW > 1050 {
		previewW = 1050
	}
	if previewW > w-2*margin {
		previewW = w - 2*margin
	}
	previewLeft := (w - previewW) / 2
	preview := rect{previewLeft, previewTop, previewLeft + previewW, previewBottom}

	cardsTop := preview.Bottom + 20
	cardsBottom := h - 58
	if cardsBottom-cardsTop < 150 {
		cardsTop = cardsBottom - 150
	}
	contentW := w - 2*margin
	gap := int32(14)
	usable := contentW - 3*gap
	w1 := usable * 23 / 100
	w2 := usable * 19 / 100
	w3 := usable * 30 / 100
	w4 := usable - w1 - w2 - w3
	x := margin
	model := rect{x, cardsTop, x + w1, cardsBottom}
	x = model.Right + gap
	filament := rect{x, cardsTop, x + w2, cardsBottom}
	x = filament.Right + gap
	quality := rect{x, cardsTop, x + w3, cardsBottom}
	x = quality.Right + gap
	open := rect{x, cardsTop, x + w4, cardsBottom}

	qInner := inset(quality, 16)
	qTop := quality.Bottom - 48
	qGap := int32(7)
	qW := (width(qInner) - 2*qGap) / 3
	fast := rect{qInner.Left, qTop, qInner.Left + qW, quality.Bottom - 13}
	balanced := rect{fast.Right + qGap, qTop, fast.Right + qGap + qW, quality.Bottom - 13}
	perfect := rect{balanced.Right + qGap, qTop, qInner.Right, quality.Bottom - 13}

	return spatialRegions{
		language: rect{w - margin - 64, 20, w - margin, 62},
		advanced: rect{w - margin - 168, 20, w - margin - 72, 62},
		device:   rect{w/2 - 180, 18, w/2 + 180, 68},
		preview:  preview, model: model, filament: filament, quality: quality, open: open,
		fast: fast, balanced: balanced, perfect: perfect,
	}
}

func brush(color uintptr) uintptr {
	b, _, _ := pCreateSolidBrush.Call(color)
	return b
}

func pen(style uintptr, lineWidth int, color uintptr) uintptr {
	p, _, _ := pCreatePen.Call(style, uintptr(lineWidth), color)
	return p
}

func withObjects(hdc, b, p uintptr, draw func()) {
	oldB, _, _ := pSelectObject.Call(hdc, b)
	oldP, _, _ := pSelectObject.Call(hdc, p)
	draw()
	pSelectObject.Call(hdc, oldP)
	pSelectObject.Call(hdc, oldB)
	pDeleteObject.Call(p)
	pDeleteObject.Call(b)
}

func rounded(hdc uintptr, r rect, radius int32, fill, stroke uintptr) {
	drawSpatialRoundedMaterial(hdc, r, radius, fill, fill, stroke)
}

func elevated(hdc uintptr, r rect, radius int32, fill, stroke uintptr) {
	drawSpatialSoftShadow(hdc, r, radius, 14, 7, rgb(52, 72, 128), 34)
	drawSpatialSoftShadow(hdc, r, radius, 4, 2, rgb(65, 88, 148), 22)
	drawSpatialRoundedMaterial(hdc, r, radius, fill, fill, stroke)
}

func text(hdc uintptr, value string, r rect, font, color, flags uintptr) {
	if value == "" {
		return
	}
	old, _, _ := pSelectObject.Call(hdc, font)
	pSetBkMode.Call(hdc, TRANSPARENT)
	pSetTextColor.Call(hdc, color)
	u := utf16Ptr(value)
	copy := r
	pDrawText.Call(hdc, uintptr(unsafe.Pointer(u)), ^uintptr(0), uintptr(unsafe.Pointer(&copy)), flags)
	pSelectObject.Call(hdc, old)
}

func line(hdc uintptr, x1, y1, x2, y2 int32, color uintptr, lineWidth int) {
	p := pen(PS_SOLID, lineWidth, color)
	old, _, _ := pSelectObject.Call(hdc, p)
	pMoveToEx.Call(hdc, i32arg(x1), i32arg(y1), 0)
	pLineTo.Call(hdc, i32arg(x2), i32arg(y2))
	pSelectObject.Call(hdc, old)
	pDeleteObject.Call(p)
}

func circle(hdc uintptr, centerX, centerY, radius int32, fill, stroke uintptr) {
	b := brush(fill)
	p := pen(PS_SOLID, 1, stroke)
	withObjects(hdc, b, p, func() {
		pEllipse.Call(hdc, i32arg(centerX-radius), i32arg(centerY-radius), i32arg(centerX+radius), i32arg(centerY+radius))
	})
}

func drawNumber(hdc uintptr, n string, card rect) {
	cx, cy := card.Left+30, card.Top+30
	bubble := rect{cx - 15, cy - 15, cx + 15, cy + 15}
	drawSpatialSoftShadow(hdc, bubble, 15, 7, 3, rgb(66, 78, 150), 38)
	drawSpatialRoundedMaterial(hdc, bubble, 15, rgb(102, 133, 255), rgb(91, 77, 243), rgb(137, 157, 255))
	text(hdc, n, bubble, hFontNumber, rgb(255, 255, 255), DT_CENTER|DT_VCENTER|DT_SINGLELINE)
}

func drawLogo(hdc uintptr, w int32) {
	if hAppIcon != 0 {
		pDrawIconEx.Call(hdc, 24, 25, hAppIcon, 30, 30, 0, 0, 3)
	} else {
		drawSpatialRoundedMaterial(hdc, rect{23, 24, 55, 56}, 10, rgb(105, 139, 255), rgb(91, 76, 244), rgb(143, 162, 255))
		text(hdc, "F", rect{23, 24, 55, 56}, hFontLogo, rgb(255, 255, 255), DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	}
	text(hdc, "FlashFit AI", rect{63, 20, 260, 62}, hFontLogo, rgb(24, 28, 38), DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	_ = w
}

func drawPrinterIcon(hdc uintptr, cx, cy int32, color uintptr) {
	line(hdc, cx-8, cy-7, cx+8, cy-7, color, 2)
	line(hdc, cx-8, cy-7, cx-8, cy-1, color, 2)
	line(hdc, cx+8, cy-7, cx+8, cy-1, color, 2)
	rounded(hdc, rect{cx - 12, cy - 2, cx + 12, cy + 9}, 4, rgb(238, 242, 250), color)
	rounded(hdc, rect{cx - 8, cy + 4, cx + 8, cy + 12}, 3, rgb(251, 252, 255), color)
	circle(hdc, cx+7, cy+2, 1, color, color)
}

func drawDevicePill(hdc uintptr) {
	r := spatial.device
	drawSpatialSoftShadow(hdc, r, 25, 15, 8, rgb(54, 69, 119), 30)
	drawSpatialSoftShadow(hdc, r, 25, 4, 2, rgb(64, 84, 140), 18)
	drawSpatialRoundedMaterial(hdc, r, 25, rgb(255, 255, 255), rgb(246, 248, 253), rgb(228, 233, 243))
	drawPrinterIcon(hdc, r.Left+36, (r.Top+r.Bottom)/2, rgb(70, 82, 110))
	text(hdc, tr("device")+"   •   "+tr("nozzle"), rect{r.Left + 62, r.Top, r.Right - 45, r.Bottom}, hFontBody, rgb(35, 40, 53), DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	text(hdc, "⌄", rect{r.Right - 42, r.Top - 2, r.Right - 17, r.Bottom}, hFontHeading, rgb(49, 56, 73), DT_CENTER|DT_VCENTER|DT_SINGLELINE)
}

func drawPerspectiveGrid(hdc uintptr, r rect) {
	horizon := r.Top + height(r)*56/100
	centerX := (r.Left + r.Right) / 2
	for i := int32(0); i <= 8; i++ {
		t := float64(i) / 8
		y := horizon + int32(float64(r.Bottom-horizon)*math.Pow(t, 1.7))
		shade := uint8(230 + int(t*7))
		line(hdc, r.Left, y, r.Right, y, rgb(shade, shade+3, 247), 1)
	}
	for i := int32(-7); i <= 7; i++ {
		topX := centerX + i*width(r)/35
		bottomX := centerX + i*width(r)/14
		line(hdc, topX, horizon, bottomX, r.Bottom, rgb(231, 235, 247), 1)
	}
}

func drawUploadIcon(hdc uintptr, cx, cy int32, color uintptr) {
	line(hdc, cx, cy+7, cx, cy-8, color, 2)
	line(hdc, cx, cy-8, cx-5, cy-3, color, 2)
	line(hdc, cx, cy-8, cx+5, cy-3, color, 2)
	line(hdc, cx-8, cy+3, cx-8, cy+9, color, 2)
	line(hdc, cx-8, cy+9, cx+8, cy+9, color, 2)
	line(hdc, cx+8, cy+9, cx+8, cy+3, color, 2)
}

func drawDashedFrame(hdc uintptr, r rect) {
	dash := pen(PS_DASH, 1, rgb(132, 151, 234))
	nullBrush, _, _ := pGetStockObject.Call(NULL_BRUSH)
	oldPen, _, _ := pSelectObject.Call(hdc, dash)
	oldBrush, _, _ := pSelectObject.Call(hdc, nullBrush)
	pRoundRect.Call(hdc, i32arg(r.Left), i32arg(r.Top), i32arg(r.Right), i32arg(r.Bottom), 34, 34)
	pSelectObject.Call(hdc, oldBrush)
	pSelectObject.Call(hdc, oldPen)
	pDeleteObject.Call(dash)
}

func spatialFloatOffset() int32 {
	return int32(math.Round(math.Sin(float64(spatialAnimationTick)*0.065) * 2.0))
}

func drawPreview(hdc uintptr) {
	r := spatial.preview
	drawSpatialSoftShadow(hdc, r, 30, 24, 12, rgb(55, 72, 125), 31)
	drawSpatialSoftShadow(hdc, r, 30, 6, 2, rgb(78, 99, 159), 17)
	drawSpatialRoundedMaterial(hdc, r, 30, rgb(255, 255, 255), rgb(244, 247, 253), rgb(218, 225, 239))

	inner := inset(r, 28)
	drawSpatialGlow(hdc, (r.Left+r.Right)/2, r.Top+height(r)*42/100, width(r)*34/100, height(r)*45/100, rgb(181, 203, 255), 34)
	drawSpatialGlow(hdc, r.Left+width(r)*69/100, r.Top+height(r)*35/100, width(r)*18/100, height(r)*30/100, rgb(219, 200, 255), 24)
	drawPerspectiveGrid(hdc, rect{inner.Left, inner.Top + 20, inner.Right, inner.Bottom - 4})

	frameW := width(inner) * 62 / 100
	frame := rect{(r.Left + r.Right - frameW) / 2, inner.Top + 10, (r.Left + r.Right + frameW) / 2, inner.Bottom - 18}
	drawDashedFrame(hdc, frame)

	modelCX := (r.Left + r.Right) / 2
	modelCY := r.Top + height(r)*45/100 + spatialFloatOffset()
	drawSpatialGlow(hdc, modelCX, modelCY+height(r)*22/100, width(r)*21/100, height(r)*12/100, rgb(74, 85, 123), 20)
	modelSize := min32(122, height(r)*31/100)
	if !drawSpatialBenchyAsset(hdc, modelCX, modelCY, modelSize) {
		drawBenchy(hdc, modelCX, modelCY, modelSize)
	}

	labelW := min32(430, width(r)*54/100)
	labelR := rect{(r.Left + r.Right - labelW) / 2, r.Bottom - 70, (r.Left + r.Right + labelW) / 2, r.Bottom - 22}
	drawSpatialSoftShadow(hdc, labelR, 24, 13, 7, rgb(62, 76, 125), 29)
	drawSpatialRoundedMaterial(hdc, labelR, 24, rgb(255, 255, 255), rgb(245, 248, 253), rgb(226, 231, 241))
	if app.modelPath == "" {
		drawUploadIcon(hdc, labelR.Left+37, (labelR.Top+labelR.Bottom)/2, rgb(74, 112, 241))
		text(hdc, tr("dropTitle"), rect{labelR.Left + 64, labelR.Top + 3, labelR.Right - 18, labelR.Top + 27}, hFontBody, rgb(48, 57, 77), DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
		text(hdc, tr("dropSubtitle"), rect{labelR.Left + 64, labelR.Top + 23, labelR.Right - 18, labelR.Bottom - 2}, hFontSmall, rgb(105, 115, 137), DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	} else {
		name := filepath.Base(app.modelPath)
		text(hdc, name, rect{labelR.Left + 20, labelR.Top + 3, labelR.Right - 20, labelR.Top + 27}, hFontBody, rgb(38, 48, 70), DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
		subtitle := tr("analyzing")
		if app.analysis != nil {
			a := *app.analysis
			subtitle = trf("analysisSummary", a.InputFormat, a.TriangleCount, a.Extents[0], a.Extents[1], a.Extents[2])
		}
		text(hdc, subtitle, rect{labelR.Left + 16, labelR.Top + 23, labelR.Right - 16, labelR.Bottom - 2}, hFontSmall, rgb(96, 108, 133), DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	}
}

func min32(a, b int32) int32 {
	if a < b {
		return a
	}
	return b
}

func drawBenchy(hdc uintptr, cx, cy, size int32) {
	blue := rgb(99, 118, 236)
	soft := rgb(225, 233, 254)
	// Soft translucent-looking silhouette made from layered native GDI shapes.
	pts := []point{{cx - size, cy + size/3}, {cx + size, cy + size/3}, {cx + size*3/4, cy + size*2/3}, {cx - size*2/3, cy + size*2/3}}
	b := brush(soft)
	p := pen(PS_SOLID, 3, blue)
	withObjects(hdc, b, p, func() { pPolygon.Call(hdc, uintptr(unsafe.Pointer(&pts[0])), uintptr(len(pts))) })
	rounded(hdc, rect{cx - size/3, cy - size/2, cx + size/3, cy + size/3}, 10, rgb(239, 244, 255), blue)
	line(hdc, cx-size/3, cy-size/6, cx+size/3, cy-size/6, blue, 2)
	circle(hdc, cx-size/8, cy-size/3, size/12, rgb(248, 250, 255), blue)
	circle(hdc, cx+size/3, cy+size/2, size/12, rgb(248, 250, 255), blue)
	line(hdc, cx-size/3, cy-size/2, cx+size/2, cy-size/2, blue, 3)
	line(hdc, cx, cy-size/2, cx, cy-size*3/4, blue, 3)
	line(hdc, cx-size/8, cy-size*3/4, cx+size/8, cy-size*3/4, blue, 3)
}

func drawCardIcon(hdc uintptr, card rect, kind int) {
	cx, cy := (card.Left+card.Right)/2, card.Top+54
	orb := rect{cx - 27, cy - 27, cx + 27, cy + 27}
	drawSpatialSoftShadow(hdc, orb, 27, 10, 5, rgb(57, 75, 130), 27)
	drawSpatialRoundedMaterial(hdc, orb, 27, rgb(255, 255, 255), rgb(241, 245, 253), rgb(230, 234, 244))
	blue := rgb(76, 111, 242)
	switch kind {
	case 1:
		line(hdc, cx-12, cy-7, cx-3, cy-7, blue, 2)
		line(hdc, cx-3, cy-7, cx+1, cy-3, blue, 2)
		line(hdc, cx+1, cy-3, cx+12, cy-3, blue, 2)
		line(hdc, cx-12, cy-7, cx-12, cy+10, blue, 2)
		line(hdc, cx-12, cy+10, cx+12, cy+10, blue, 2)
		line(hdc, cx+12, cy+10, cx+12, cy-3, blue, 2)
	case 2:
		circle(hdc, cx, cy-7, 11, rgb(244, 247, 255), blue)
		circle(hdc, cx, cy-7, 3, rgb(255, 255, 255), blue)
		line(hdc, cx-11, cy-2, cx-11, cy+9, blue, 2)
		line(hdc, cx+11, cy-2, cx+11, cy+9, blue, 2)
		line(hdc, cx-11, cy+1, cx+11, cy+1, blue, 2)
		line(hdc, cx-11, cy+5, cx+11, cy+5, blue, 2)
		line(hdc, cx-11, cy+9, cx+11, cy+9, blue, 2)
	case 3:
		for i, off := range []int32{-9, 0, 9} {
			line(hdc, cx-13, cy+off, cx+13, cy+off, blue, 2)
			knob := []int32{4, -5, 7}[i]
			circle(hdc, cx+knob, cy+off, 3, rgb(255, 255, 255), blue)
		}
	case 4:
		ptsA := []point{{cx - 11, cy}, {cx - 3, cy - 9}, {cx + 5, cy}, {cx - 3, cy + 9}}
		ptsB := []point{{cx + 3, cy + 7}, {cx + 9, cy + 1}, {cx + 15, cy + 7}, {cx + 9, cy + 13}}
		b, p := brush(blue), pen(PS_SOLID, 1, blue)
		withObjects(hdc, b, p, func() {
			pPolygon.Call(hdc, uintptr(unsafe.Pointer(&ptsA[0])), uintptr(len(ptsA)))
			pPolygon.Call(hdc, uintptr(unsafe.Pointer(&ptsB[0])), uintptr(len(ptsB)))
		})
	}
}

func drawActionCard(hdc uintptr, card rect, number, titleValue string, kind int) {
	drawSpatialSoftShadow(hdc, card, 24, 17, 9, rgb(57, 72, 125), 27)
	drawSpatialSoftShadow(hdc, card, 24, 4, 2, rgb(77, 96, 151), 15)
	drawSpatialRoundedMaterial(hdc, card, 24, rgb(255, 255, 255), rgb(246, 248, 252), rgb(225, 230, 241))
	drawNumber(hdc, number, card)
	drawCardIcon(hdc, card, kind)
	text(hdc, titleValue, rect{card.Left + 16, card.Top + 83, card.Right - 16, card.Top + 116}, hFontHeading, rgb(30, 35, 48), DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
}

func drawActions(hdc uintptr) {
	drawActionCard(hdc, spatial.model, "1", tr("stepModel"), 1)
	modelText := tr("chooseModel")
	if app.modelPath != "" {
		modelText = filepath.Base(app.modelPath)
	}
	text(hdc, modelText, rect{spatial.model.Left + 18, spatial.model.Top + 116, spatial.model.Right - 18, spatial.model.Bottom - 12}, hFontSmall, rgb(93, 102, 124), DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)

	drawActionCard(hdc, spatial.filament, "2", tr("stepFilament"), 2)
	filText := tr("noFilament")
	if f, ok := selectedFilament(); ok {
		filText = "✓  " + f.Material
	}
	chip := rect{spatial.filament.Left + 22, spatial.filament.Bottom - 47, spatial.filament.Right - 22, spatial.filament.Bottom - 14}
	drawSpatialRoundedMaterial(hdc, chip, 17, rgb(251, 252, 255), rgb(241, 245, 253), rgb(105, 126, 238))
	text(hdc, filText, inset(chip, 7), hFontSmall, rgb(65, 83, 208), DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)

	drawActionCard(hdc, spatial.quality, "3", tr("stepQuality"), 3)
	drawQualityChoice(hdc, spatial.fast, "low", tr("qualityFast"))
	drawQualityChoice(hdc, spatial.balanced, "balanced", tr("qualityBalanced"))
	drawQualityChoice(hdc, spatial.perfect, "perfect", tr("qualityPerfect"))
	if app.quality == "perfect" {
		text(hdc, "✦  "+currentTextureTitle(), rect{spatial.quality.Left + 18, spatial.quality.Top + 112, spatial.quality.Right - 18, spatial.quality.Bottom - 53}, hFontSmall, rgb(83, 92, 172), DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	}

	drawActionCard(hdc, spatial.open, "4", tr("stepOpen"), 4)
	button := rect{spatial.open.Left + 17, spatial.open.Bottom - 52, spatial.open.Right - 17, spatial.open.Bottom - 14}
	if app.ready && !app.importing {
		drawSpatialSoftShadow(hdc, button, 19, 10, 5, rgb(79, 71, 219), 45)
		drawSpatialRoundedMaterial(hdc, button, 19, rgb(74, 144, 255), rgb(109, 70, 242), rgb(124, 146, 255))
		drawButtonShimmer(hdc, button)
		text(hdc, tr("openFlash")+"  ›", inset(button, 6), hFontButton, rgb(255, 255, 255), DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	} else {
		drawSpatialRoundedMaterial(hdc, button, 19, rgb(238, 241, 248), rgb(226, 231, 241), rgb(218, 224, 235))
		text(hdc, tr("openFlash"), inset(button, 6), hFontButton, rgb(137, 148, 171), DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	}
}

func currentTextureTitle() string {
	switch app.texture {
	case "prism":
		return tr("texturePrism")
	case "carbon":
		return tr("textureCarbon")
	case "topographic":
		return tr("textureTopo")
	default:
		return tr("textureSatin")
	}
}

func drawButtonShimmer(hdc uintptr, button rect) {
	saved, _, _ := pSaveDC.Call(hdc)
	region, _, _ := pCreateRoundRectRgn.Call(i32arg(button.Left), i32arg(button.Top), i32arg(button.Right+1), i32arg(button.Bottom+1), 38, 38)
	if region != 0 {
		pSelectClipRgn.Call(hdc, region)
		phase := float64(spatialAnimationTick%240) / 239.0
		x := button.Left - 70 + int32(phase*float64(width(button)+140))
		drawSpatialGlow(hdc, x, (button.Top+button.Bottom)/2, 44, 31, rgb(255, 255, 255), 44)
		pDeleteObject.Call(region)
	}
	if saved != 0 {
		pRestoreDC.Call(hdc, saved)
	}
}

func drawQualityChoice(hdc uintptr, r rect, value, label string) {
	selected := app.quality == value
	if selected {
		drawSpatialSoftShadow(hdc, r, 14, 7, 3, rgb(74, 69, 205), 35)
		drawSpatialRoundedMaterial(hdc, r, 14, rgb(84, 130, 255), rgb(94, 76, 238), rgb(124, 143, 255))
		text(hdc, label, inset(r, 5), hFontSmall, rgb(255, 255, 255), DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	} else {
		drawSpatialRoundedMaterial(hdc, r, 14, rgb(255, 255, 255), rgb(243, 246, 251), rgb(224, 229, 240))
		text(hdc, label, inset(r, 5), hFontSmall, rgb(47, 57, 79), DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	}
}

func currentStatusText() string {
	if app.statusKey != "" {
		args := append([]any(nil), app.statusArgs...)
		if len(args) > 0 && (app.statusKey == "statusAnalysisFailed" || app.statusKey == "statusAnalysisBlocked" || app.statusKey == "statusImportCanceled") {
			if raw, ok := args[0].(string); ok {
				args[0] = localizeEngineText(raw)
			}
		}
		return trf(app.statusKey, args...)
	}
	if app.ready {
		return tr("readyOpen")
	}
	return tr("ready")
}

func drawStatus(hdc uintptr, w, h int32) {
	status := currentStatusText()
	cx := w / 2
	pulse := int32(1)
	if spatialAnimationTick%60 < 30 {
		pulse = 0
	}
	drawSpatialGlow(hdc, cx-136, h-31, 19+pulse, 19+pulse, rgb(85, 115, 246), 39)
	dot := rect{cx - 144, h - 39, cx - 128, h - 23}
	drawSpatialRoundedMaterial(hdc, dot, 8, rgb(94, 135, 255), rgb(88, 82, 240), rgb(132, 153, 255))
	text(hdc, "✓", dot, hFontSmall, rgb(255, 255, 255), DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	text(hdc, status, rect{cx - 117, h - 49, cx + 330, h - 13}, hFontBody, rgb(94, 103, 123), DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
}

func drawSpatialScene(hdc uintptr, client rect) {
	w, h := width(client), height(client)
	spatial = calculateSpatialLayout(w, h)
	background := brush(rgb(247, 249, 253))
	pFillRect.Call(hdc, uintptr(unsafe.Pointer(&client)), background)
	pDeleteObject.Call(background)

	// Atmospheric light instead of visible decorative shapes.
	drawSpatialGlow(hdc, w*25/100, 70, min32(430, w*34/100), 250, rgb(219, 226, 255), 31)
	drawSpatialGlow(hdc, w*78/100, h-40, min32(520, w*38/100), 300, rgb(205, 222, 255), 27)
	drawSpatialGlow(hdc, w*62/100, 120, min32(300, w*23/100), 210, rgb(231, 218, 255), 20)
	drawLogo(hdc, w)
	drawSpatialSoftShadow(hdc, spatial.advanced, 21, 9, 4, rgb(61, 77, 124), 20)
	drawSpatialRoundedMaterial(hdc, spatial.advanced, 21, rgb(255, 255, 255), rgb(245, 248, 253), rgb(228, 233, 243))
	text(hdc, "⋯  "+tr("advanced"), inset(spatial.advanced, 8), hFontSmall, rgb(67, 80, 108), DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	drawSpatialSoftShadow(hdc, spatial.language, 21, 9, 4, rgb(61, 77, 124), 20)
	drawSpatialRoundedMaterial(hdc, spatial.language, 21, rgb(255, 255, 255), rgb(245, 248, 253), rgb(228, 233, 243))
	text(hdc, strings.ToUpper(uiLanguage)+"  ▾", inset(spatial.language, 8), hFontSmall, rgb(48, 60, 87), DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	drawDevicePill(hdc)
	drawPreview(hdc)
	drawActions(hdc)
	drawStatus(hdc, w, h)
}

func paintSpatialUI(hwnd uintptr) {
	var ps paintStruct
	hdc, _, _ := pBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	if hdc == 0 {
		return
	}
	defer pEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	var client rect
	pGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&client)))
	w, h := width(client), height(client)
	if w <= 0 || h <= 0 {
		return
	}

	bufferDC, _, _ := pCreateCompatibleDC.Call(hdc)
	bufferBitmap, _, _ := pCreateCompatibleBitmap.Call(hdc, uintptr(w), uintptr(h))
	if bufferDC == 0 || bufferBitmap == 0 {
		if bufferDC != 0 {
			pDeleteDC.Call(bufferDC)
		}
		if bufferBitmap != 0 {
			pDeleteObject.Call(bufferBitmap)
		}
		drawSpatialScene(hdc, client)
		return
	}
	oldBitmap, _, _ := pSelectObject.Call(bufferDC, bufferBitmap)
	drawSpatialScene(bufferDC, client)
	pBitBlt.Call(hdc, 0, 0, uintptr(w), uintptr(h), bufferDC, 0, 0, SRCCOPY)
	pSelectObject.Call(bufferDC, oldBitmap)
	pDeleteObject.Call(bufferBitmap)
	pDeleteDC.Call(bufferDC)
}

func spatialClick(x, y int32) {
	switch {
	case contains(spatial.language, x, y):
		showLanguageMenu()
	case contains(spatial.advanced, x, y):
		showAdvancedMenu()
	case contains(spatial.device, x, y):
		startDiscovery()
	case contains(spatial.preview, x, y), contains(spatial.model, x, y):
		chooseAndSetModel()
	case contains(spatial.filament, x, y):
		showFilamentPicker()
	case contains(spatial.fast, x, y):
		setQuality("low")
	case contains(spatial.balanced, x, y):
		setQuality("balanced")
	case contains(spatial.perfect, x, y):
		setQuality("perfect")
	case contains(spatial.open, x, y):
		if app.ready {
			startImport()
		}
	}
}

func popupCommand(items []struct {
	id    int
	label string
}) int {
	menu, _, _ := pCreatePopupMenu.Call()
	if menu == 0 {
		return 0
	}
	defer pDestroyMenu.Call(menu)
	for _, item := range items {
		label := utf16Ptr(item.label)
		pAppendMenu.Call(menu, 0, uintptr(item.id), uintptr(unsafe.Pointer(label)))
	}
	var pt point
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	cmd, _, _ := pTrackPopupMenu.Call(menu, 0x0100|0x0002, i32arg(pt.X), i32arg(pt.Y), 0, mainHwnd, 0)
	return int(cmd)
}

func showLanguageMenu() {
	cmd := popupCommand([]struct {
		id    int
		label string
	}{
		{idLanguageIT, "Italiano"}, {idLanguageEN, "English"}, {idLanguageFR, "Français"}, {idLanguageES, "Español"}, {idLanguageDE, "Deutsch"},
	})
	code := map[int]string{idLanguageIT: "it", idLanguageEN: "en", idLanguageFR: "fr", idLanguageES: "es", idLanguageDE: "de"}[cmd]
	if code == "" || code == uiLanguage {
		return
	}
	uiLanguage = code
	saveUILanguage()
	setSpatialTitle()
	updateFilamentDialogLanguage()
	invalidateSpatial()
}

func showAdvancedMenu() {
	cmd := popupCommand([]struct {
		id    int
		label string
	}{
		{idAdvancedScan, tr("scanAgain")},
		{idAdvancedManual, tr("manualProfiles")},
		{idAdvancedLog, tr("openLog")},
	})
	switch cmd {
	case idAdvancedScan:
		startDiscovery()
	case idAdvancedManual:
		configureProfilesManually()
	case idAdvancedLog:
		_ = exec.Command("explorer.exe", filepath.Dir(logPath())).Start()
	}
}

func chooseAndSetModel() {
	filter := tr("supportedModels") + " (*.stl;*.obj;*.3mf)\x00*.stl;*.obj;*.3mf\x00STL (*.stl)\x00*.stl\x00OBJ (*.obj)\x00*.obj\x003MF (*.3mf)\x00*.3mf\x00" + tr("allFiles") + " (*.*)\x00*.*\x00\x00"
	if p := chooseFile(tr("selectModelTitle"), filter, "3mf"); p != "" {
		setModelPath(p)
	}
}

func showFilamentPicker() {
	if hFilamentDialog != 0 {
		pShowWindow.Call(hFilamentDialog, SW_RESTORE)
		pSetForegroundWindow.Call(hFilamentDialog)
		return
	}
	cls := utf16Ptr(filamentClassName)
	inst := appInstance
	if registerFilamentClass(inst) == 0 {
		return
	}
	title := utf16Ptr(tr("filamentTitle"))
	h, _, _ := pCreateWindowEx.Call(0x00000001, uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(title)), WS_OVERLAPPEDWINDOW|WS_VISIBLE|WS_CLIPCHILDREN, 210, 130, 760, 640, mainHwnd, 0, inst, 0)
	if h != 0 {
		hFilamentDialog = h
		pShowWindow.Call(h, SW_SHOWNORMAL)
		pSetForegroundWindow.Call(h)
	}
}

const filamentClassName = "FlashFitAI_Spatial_FilamentPicker_v40"

var filamentClassAtom uintptr

func registerFilamentClass(inst uintptr) uintptr {
	if filamentClassAtom != 0 {
		return filamentClassAtom
	}
	cursor, _, _ := pLoadCursor.Call(0, IDC_ARROW)
	cls := utf16Ptr(filamentClassName)
	icon, _, _ := pLoadIcon.Call(inst, 1)
	wc := wndClassEx{CbSize: uint32(unsafe.Sizeof(wndClassEx{})), LpfnWndProc: syscall.NewCallback(filamentWindowProc), HInstance: inst, HIcon: icon, HCursor: cursor, HbrBackground: COLOR_BTNFACE + 1, LpszClassName: cls, HIconSm: icon}
	atom, _, _ := pRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc)))
	filamentClassAtom = atom
	return atom
}

func filamentWindowProc(hwnd uintptr, message uint32, wParam, lParam uintptr) (ret uintptr) {
	switch message {
	case WM_CREATE:
		hFilamentDialog = hwnd
		hFilamentSearchLabel = control(hwnd, "STATIC", tr("searchFilament"), WS_CHILD|WS_VISIBLE, 0, 0)
		hSearch = control(hwnd, "EDIT", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|WS_BORDER|ES_AUTOHSCROLL, WS_EX_CLIENTEDGE, idFilamentSearch)
		hFilamentList = control(hwnd, "LISTBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|WS_BORDER|WS_VSCROLL|LBS_NOTIFY|LBS_NOINTEGRALHEIGHT, WS_EX_CLIENTEDGE, idFilamentResults)
		hFilamentDetails = control(hwnd, "EDIT", "", WS_CHILD|WS_VISIBLE|WS_BORDER|ES_MULTILINE|ES_AUTOVSCROLL|ES_READONLY|WS_VSCROLL, WS_EX_CLIENTEDGE, 0)
		hFilamentApply = control(hwnd, "BUTTON", tr("useFilament"), WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON, 0, idFilamentApply)
		hFilamentClose = control(hwnd, "BUTTON", tr("close"), WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON, 0, idFilamentClose)
		cue := utf16Ptr(tr("searchFilament"))
		pSendMessage.Call(hSearch, EM_SETCUEBANNER, 1, uintptr(unsafe.Pointer(cue)))
		refreshFilamentList()
		layoutFilamentDialog(hwnd)
		pSetFocus.Call(hSearch)
		return 0
	case WM_SIZE:
		layoutFilamentDialog(hwnd)
		return 0
	case WM_COMMAND:
		id := int(wParam & 0xffff)
		notify := int((wParam >> 16) & 0xffff)
		switch id {
		case idFilamentSearch:
			if notify == EN_CHANGE {
				refreshFilamentList()
			}
		case idFilamentResults:
			if notify == LBN_SELCHANGE {
				selectFilamentFromList()
				invalidateSpatial()
			}
		case idFilamentApply, idFilamentClose:
			pDestroyWindow.Call(hwnd)
		}
		return 0
	case WM_CLOSE:
		pDestroyWindow.Call(hwnd)
		return 0
	case WM_DESTROY:
		hFilamentDialog, hSearch, hFilamentList, hFilamentDetails = 0, 0, 0, 0
		hFilamentSearchLabel, hFilamentApply, hFilamentClose = 0, 0, 0
		invalidateSpatial()
		return 0
	}
	r, _, _ := pDefWindowProc.Call(hwnd, uintptr(message), wParam, lParam)
	return r
}

func layoutFilamentDialog(hwnd uintptr) {
	if hSearch == 0 {
		return
	}
	var r rect
	pGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	w, h := int(width(r)), int(height(r))
	if w < 620 {
		w = 620
	}
	if h < 500 {
		h = 500
	}
	m := 22
	move(hFilamentSearchLabel, m, 18, w-2*m, 24)
	move(hSearch, m, 48, w-2*m, 36)
	listW := (w - 3*m) * 55 / 100
	move(hFilamentList, m, 100, listW, h-178)
	move(hFilamentDetails, 2*m+listW, 100, w-3*m-listW, h-178)
	move(hFilamentClose, w-m-250, h-58, 110, 36)
	move(hFilamentApply, w-m-130, h-58, 130, 36)
}

func updateFilamentDialogLanguage() {
	if hFilamentDialog == 0 {
		return
	}
	setText(hFilamentDialog, tr("filamentTitle"))
	setText(hFilamentSearchLabel, tr("searchFilament"))
	setText(hFilamentApply, tr("useFilament"))
	setText(hFilamentClose, tr("close"))
	cue := utf16Ptr(tr("searchFilament"))
	pSendMessage.Call(hSearch, EM_SETCUEBANNER, 1, uintptr(unsafe.Pointer(cue)))
	updateFilamentDetails()
}

func pointFromLParam(lParam uintptr) (int32, int32) {
	x := int32(int16(lParam & 0xffff))
	y := int32(int16((lParam >> 16) & 0xffff))
	return x, y
}

func formatProfileState() string {
	parts := []string{}
	if app.slicer != "" {
		parts = append(parts, "Flash Studio")
	}
	if app.machine != "" {
		parts = append(parts, "AD5M")
	}
	if app.process != "" {
		parts = append(parts, app.quality)
	}
	return fmt.Sprintf("%d/3 %s", len(parts), strings.Join(parts, " • "))
}
