//go:build windows

package main

import (
	"fmt"
	"math"
	"runtime/debug"
	"syscall"
	"unsafe"
)

const (
	textureClassName   = "FlashFitAI_UltraPremiumTextures_v1"
	idTextureAnimation = 5102
	WS_CAPTION         = 0x00C00000
	WS_SYSMENU         = 0x00080000
)

type texturePickerLayout struct {
	cards [4]rect
	apply rect
}

type texturePickerItem struct {
	id, titleKey, descriptionKey string
}

var texturePickerItems = [4]texturePickerItem{
	{id: "satin", titleKey: "textureSatin", descriptionKey: "textureSatinDesc"},
	{id: "prism", titleKey: "texturePrism", descriptionKey: "texturePrismDesc"},
	{id: "carbon", titleKey: "textureCarbon", descriptionKey: "textureCarbonDesc"},
	{id: "topographic", titleKey: "textureTopo", descriptionKey: "textureTopoDesc"},
}

var (
	hTexturePicker         uintptr
	textureWndProcCallback uintptr
	textureClassRegistered bool
	textureDraft           = "satin"
	textureLayout          texturePickerLayout
	textureAnimationTick   uint32
	texturePickerBuffer    windowBackBuffer
	pGetWindowRect         = user32.NewProc("GetWindowRect")
)

func registerTexturePickerClass() bool {
	if textureClassRegistered {
		return true
	}
	cursor, _, _ := pLoadCursor.Call(0, IDC_ARROW)
	cls := utf16Ptr(textureClassName)
	textureWndProcCallback = syscall.NewCallback(textureWindowProc)
	wc := wndClassEx{
		CbSize: uint32(unsafe.Sizeof(wndClassEx{})), LpfnWndProc: textureWndProcCallback,
		HInstance: appInstance, HIcon: hAppIcon, HCursor: cursor,
		HbrBackground: COLOR_BTNFACE + 1, LpszClassName: cls, HIconSm: hAppIcon,
	}
	atom, _, err := pRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc)))
	if atom == 0 {
		writeLog(fmt.Sprintf("texture picker class failed: %v", err))
		return false
	}
	textureClassRegistered = true
	return true
}

func showTexturePicker() {
	if app.importing || mainHwnd == 0 {
		return
	}
	if hTexturePicker != 0 {
		pSetForegroundWindow.Call(hTexturePicker)
		return
	}
	if !registerTexturePickerClass() {
		return
	}
	textureDraft = app.texture
	if textureDraft == "" || textureDraft == "none" {
		textureDraft = "satin"
	}
	outerW, outerH := int32(940), int32(650)
	x, y := int32(110), int32(70)
	var parent rect
	if ok, _, _ := pGetWindowRect.Call(mainHwnd, uintptr(unsafe.Pointer(&parent))); ok != 0 {
		x = parent.Left + (width(parent)-outerW)/2
		y = parent.Top + (height(parent)-outerH)/2
	}
	cls := utf16Ptr(textureClassName)
	title := utf16Ptr("FlashFit AI • Ultra Premium")
	style := uintptr(WS_CAPTION | WS_SYSMENU | WS_CLIPCHILDREN | WS_VISIBLE)
	pEnableWindow.Call(mainHwnd, 0)
	h, _, err := pCreateWindowEx.Call(0, uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(title)), style, i32arg(x), i32arg(y), uintptr(outerW), uintptr(outerH), mainHwnd, 0, appInstance, 0)
	if h == 0 {
		pEnableWindow.Call(mainHwnd, 1)
		writeLog(fmt.Sprintf("texture picker create failed: %v", err))
		return
	}
	hTexturePicker = h
	setPremiumWindowMaterial(h)
	pShowWindow.Call(h, SW_SHOW)
	pUpdateWindow.Call(h)
	pSetForegroundWindow.Call(h)
}

func setPremiumWindowMaterial(hwnd uintptr) {
	corner := uint32(2)
	dark := uint32(0)
	caption := uint32(rgb(247, 249, 253))
	captionText := uint32(rgb(24, 29, 40))
	border := uint32(rgb(189, 199, 224))
	backdrop := uint32(2)
	pDwmSetWindowAttr.Call(hwnd, 33, uintptr(unsafe.Pointer(&corner)), unsafe.Sizeof(corner))
	pDwmSetWindowAttr.Call(hwnd, 20, uintptr(unsafe.Pointer(&dark)), unsafe.Sizeof(dark))
	pDwmSetWindowAttr.Call(hwnd, 35, uintptr(unsafe.Pointer(&caption)), unsafe.Sizeof(caption))
	pDwmSetWindowAttr.Call(hwnd, 36, uintptr(unsafe.Pointer(&captionText)), unsafe.Sizeof(captionText))
	pDwmSetWindowAttr.Call(hwnd, 34, uintptr(unsafe.Pointer(&border)), unsafe.Sizeof(border))
	pDwmSetWindowAttr.Call(hwnd, 38, uintptr(unsafe.Pointer(&backdrop)), unsafe.Sizeof(backdrop))
}

func closeTexturePicker(apply bool) {
	if apply {
		app.texture = textureDraft
		renderAnalysis()
		invalidateSpatial()
	}
	if hTexturePicker != 0 {
		pDestroyWindow.Call(hTexturePicker)
	}
}

func textureWindowProc(hwnd uintptr, message uint32, wParam, lParam uintptr) (ret uintptr) {
	defer func() {
		if recovered := recover(); recovered != nil {
			writeLog(fmt.Sprintf("PANIC texture wndproc msg=%x: %v\n%s", message, recovered, debug.Stack()))
			ret, _, _ = pDefWindowProc.Call(hwnd, uintptr(message), wParam, lParam)
		}
	}()
	switch message {
	case WM_CREATE:
		hTexturePicker = hwnd
		pSetTimer.Call(hwnd, idTextureAnimation, spatialAnimationInterval, 0)
		return 0
	case WM_PAINT:
		paintTexturePicker(hwnd)
		return 0
	case WM_ERASEBKGND:
		return 1
	case WM_TIMER:
		foreground, _, _ := pGetForegroundWindow.Call()
		if wParam == idTextureAnimation && foreground == hwnd {
			textureAnimationTick++
			pInvalidateRect.Call(hwnd, 0, 0)
		}
		return 0
	case WM_LBUTTONUP:
		x, y := pointFromLParam(lParam)
		for index, card := range textureLayout.cards {
			if contains(card, x, y) {
				textureDraft = texturePickerItems[index].id
				pInvalidateRect.Call(hwnd, 0, 0)
				return 0
			}
		}
		if contains(textureLayout.apply, x, y) {
			closeTexturePicker(true)
		}
		return 0
	case WM_CLOSE:
		closeTexturePicker(false)
		return 0
	case WM_DESTROY:
		pKillTimer.Call(hwnd, idTextureAnimation)
		texturePickerBuffer.reset()
		hTexturePicker = 0
		if mainHwnd != 0 {
			pEnableWindow.Call(mainHwnd, 1)
			pSetForegroundWindow.Call(mainHwnd)
		}
		return 0
	}
	r, _, _ := pDefWindowProc.Call(hwnd, uintptr(message), wParam, lParam)
	return r
}

func calculateTextureLayout(w, h int32) texturePickerLayout {
	margin, gap := int32(32), int32(13)
	top := int32(121)
	bottom := h - 102
	cardW := (w - 2*margin - 3*gap) / 4
	var layout texturePickerLayout
	for i := int32(0); i < 4; i++ {
		left := margin + i*(cardW+gap)
		layout.cards[i] = rect{left, top, left + cardW, bottom}
	}
	layout.apply = rect{w - margin - 224, h - 72, w - margin, h - 26}
	return layout
}

func paintTexturePicker(hwnd uintptr) {
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
	if !texturePickerBuffer.ensure(hdc, w, h) {
		drawTexturePickerScene(hdc, client)
		return
	}
	drawTexturePickerScene(texturePickerBuffer.dc, client)
	pBitBlt.Call(hdc, 0, 0, uintptr(w), uintptr(h), texturePickerBuffer.dc, 0, 0, SRCCOPY)
}

func drawTexturePickerScene(hdc uintptr, client rect) {
	w, h := width(client), height(client)
	textureLayout = calculateTextureLayout(w, h)
	background := brush(rgb(247, 249, 253))
	pFillRect.Call(hdc, uintptr(unsafe.Pointer(&client)), background)
	pDeleteObject.Call(background)
	drawSpatialGlow(hdc, w*18/100, 25, 260, 180, rgb(214, 224, 255), 33)
	drawSpatialGlow(hdc, w*84/100, h-40, 310, 210, rgb(224, 211, 255), 28)
	drawSpatialGlow(hdc, w*57/100, 90, 230, 140, rgb(192, 219, 255), 20)

	text(hdc, tr("textureEyebrow"), rect{32, 19, w - 32, 42}, hFontSmall, rgb(89, 103, 191), DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	text(hdc, tr("textureTitle"), rect{32, 41, w - 32, 76}, hFontTitle, rgb(23, 28, 40), DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	text(hdc, tr("textureSubtitle"), rect{80, 75, w - 80, 107}, hFontSmall, rgb(91, 102, 128), DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)

	for index, item := range texturePickerItems {
		drawTextureCard(hdc, textureLayout.cards[index], item, item.id == textureDraft)
	}

	text(hdc, "✓  "+tr("textureApplied"), rect{34, h - 73, textureLayout.apply.Left - 20, h - 25}, hFontSmall, rgb(75, 89, 126), DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	drawSpatialSoftShadow(hdc, textureLayout.apply, 23, 12, 6, rgb(71, 66, 204), 43)
	drawSpatialRoundedMaterial(hdc, textureLayout.apply, 23, rgb(77, 147, 255), rgb(111, 67, 241), rgb(135, 151, 255))
	drawTextureButtonShimmer(hdc, textureLayout.apply)
	text(hdc, tr("textureApply")+"   ›", inset(textureLayout.apply, 8), hFontButton, rgb(255, 255, 255), DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
}

func drawTextureCard(hdc uintptr, card rect, item texturePickerItem, selected bool) {
	stroke := rgb(224, 230, 241)
	if selected {
		stroke = rgb(105, 121, 244)
		drawSpatialGlow(hdc, (card.Left+card.Right)/2, card.Top+height(card)/2, width(card)*3/4, height(card)*3/5, rgb(104, 119, 255), 26)
		drawSpatialSoftShadow(hdc, card, 24, 18, 9, rgb(74, 68, 205), 42)
	} else {
		drawSpatialSoftShadow(hdc, card, 24, 13, 7, rgb(64, 78, 123), 24)
	}
	drawSpatialRoundedMaterial(hdc, card, 24, rgb(255, 255, 255), rgb(244, 247, 252), stroke)
	preview := rect{card.Left + 13, card.Top + 13, card.Right - 13, card.Top + min32(205, height(card)*55/100)}
	drawTexturePreview(hdc, preview, item.id, selected)
	if selected {
		badge := rect{preview.Left + 10, preview.Top + 10, preview.Right - 10, preview.Top + 34}
		text(hdc, "✓  "+tr("textureSelected"), badge, hFontSmall, rgb(230, 235, 255), DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	}
	text(hdc, tr(item.titleKey), rect{card.Left + 14, preview.Bottom + 11, card.Right - 14, preview.Bottom + 41}, hFontHeading, rgb(28, 33, 47), DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	text(hdc, tr(item.descriptionKey), rect{card.Left + 17, preview.Bottom + 43, card.Right - 17, card.Bottom - 13}, hFontSmall, rgb(91, 101, 124), DT_CENTER|DT_TOP|DT_WORDBREAK)
}

func drawTexturePreview(hdc uintptr, preview rect, kind string, selected bool) {
	drawSpatialRoundedMaterial(hdc, preview, 18, rgb(30, 33, 43), rgb(7, 9, 15), rgb(60, 68, 88))
	saved, _, _ := pSaveDC.Call(hdc)
	region, _, _ := pCreateRoundRectRgn.Call(i32arg(preview.Left), i32arg(preview.Top), i32arg(preview.Right+1), i32arg(preview.Bottom+1), 36, 36)
	if region != 0 {
		pSelectClipRgn.Call(hdc, region)
	}
	accent := rgb(123, 147, 255)
	if selected {
		drawSpatialGlow(hdc, preview.Right-25, preview.Top+25, 90, 90, rgb(116, 93, 255), 42)
	}
	switch kind {
	case "satin":
		for i := int32(-6); i < 24; i++ {
			x := preview.Left + i*13
			shade := rgb(84, 99, 145)
			if i%4 == 0 {
				shade = accent
			}
			line(hdc, x, preview.Bottom, x+105, preview.Top, shade, 1)
		}
		drawSpatialGlow(hdc, (preview.Left+preview.Right)/2, (preview.Top+preview.Bottom)/2, width(preview)/3, height(preview)/3, rgb(174, 190, 255), 24)
	case "prism":
		step := max32(26, width(preview)/6)
		for x := preview.Left - step; x < preview.Right+step; x += step {
			line(hdc, x, preview.Bottom, x+step*2, preview.Top, rgb(92, 111, 178), 1)
			line(hdc, x, preview.Top, x+step*2, preview.Bottom, rgb(63, 78, 137), 1)
		}
		for y := preview.Top + step/2; y < preview.Bottom; y += step {
			line(hdc, preview.Left, y, preview.Right, y, rgb(73, 89, 149), 1)
		}
		drawSpatialGlow(hdc, preview.Left+width(preview)*62/100, preview.Top+height(preview)*45/100, width(preview)/3, height(preview)/3, rgb(134, 104, 255), 38)
	case "carbon":
		for i := int32(-8); i < 24; i++ {
			x := preview.Left + i*11
			line(hdc, x, preview.Bottom, x+95, preview.Top, rgb(111, 126, 177), 4)
			line(hdc, x, preview.Bottom, x+95, preview.Top, rgb(39, 45, 65), 1)
			line(hdc, x, preview.Top, x+95, preview.Bottom, rgb(65, 75, 110), 3)
		}
		drawSpatialGlow(hdc, preview.Left+width(preview)*28/100, preview.Top+height(preview)*30/100, width(preview)/3, height(preview)/3, rgb(94, 127, 255), 24)
	case "topographic":
		cx, cy := preview.Left+width(preview)*57/100, preview.Top+height(preview)*53/100
		nullBrush, _, _ := pGetStockObject.Call(NULL_BRUSH)
		oldBrush, _, _ := pSelectObject.Call(hdc, nullBrush)
		for i := int32(1); i <= 9; i++ {
			p := pen(PS_SOLID, 1, rgb(uint8(66+i*4), uint8(81+i*5), uint8(130+i*7)))
			oldPen, _, _ := pSelectObject.Call(hdc, p)
			rx, ry := i*14, i*10
			offset := int32(math.Sin(float64(i)*1.7) * 8)
			pEllipse.Call(hdc, i32arg(cx-rx+offset), i32arg(cy-ry), i32arg(cx+rx+offset), i32arg(cy+ry))
			pSelectObject.Call(hdc, oldPen)
			pDeleteObject.Call(p)
		}
		pSelectObject.Call(hdc, oldBrush)
		drawSpatialGlow(hdc, cx, cy, width(preview)/3, height(preview)/3, rgb(86, 124, 255), 22)
	}
	if region != 0 {
		pDeleteObject.Call(region)
	}
	if saved != 0 {
		pRestoreDC.Call(hdc, saved)
	}
}

func drawTextureButtonShimmer(hdc uintptr, button rect) {
	saved, _, _ := pSaveDC.Call(hdc)
	region, _, _ := pCreateRoundRectRgn.Call(i32arg(button.Left), i32arg(button.Top), i32arg(button.Right+1), i32arg(button.Bottom+1), 46, 46)
	if region != 0 {
		pSelectClipRgn.Call(hdc, region)
		phase := float64(textureAnimationTick%210) / 209.0
		x := button.Left - 60 + int32(phase*float64(width(button)+120))
		drawSpatialGlow(hdc, x, (button.Top+button.Bottom)/2, 42, 34, rgb(255, 255, 255), 45)
		pDeleteObject.Call(region)
	}
	if saved != 0 {
		pRestoreDC.Call(hdc, saved)
	}
}

func max32(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}
