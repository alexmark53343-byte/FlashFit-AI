//go:build windows

package main

import (
	"fmt"
	"runtime/debug"
	"syscall"
	"unsafe"
)

const (
	textureClassName   = "FlashFitAI_UltraPremiumTextures_v1"
	idTextureAnimation = 5102
	WS_CAPTION         = 0x00C00000
	WS_SYSMENU         = 0x00080000
	VK_LEFT            = 0x25
	VK_RIGHT           = 0x27
)

type texturePickerLayout struct {
	cards [4]rect
	apply rect
	close rect
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
	textureHoverCard       = -1
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
	applyWindowChrome(hwnd)
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
			return 0
		}
		if contains(textureLayout.close, x, y) {
			closeTexturePicker(false)
		}
		return 0
	case WM_MOUSEMOVE:
		x, y := pointFromLParam(lParam)
		if card := textureHoveredCard(x, y); card != textureHoverCard {
			textureHoverCard = card
			pInvalidateRect.Call(hwnd, 0, 0)
		}
		return 0
	case WM_KEYDOWN:
		switch wParam {
		case VK_ESCAPE:
			closeTexturePicker(false)
		case VK_RETURN:
			closeTexturePicker(true)
		case VK_LEFT, VK_UP:
			moveTextureDraft(-1)
		case VK_RIGHT, VK_DOWN:
			moveTextureDraft(1)
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
	layout.close = rect{layout.apply.Left - 12 - 122, h - 72, layout.apply.Left - 12, h - 26}
	return layout
}

func textureHoveredCard(x, y int32) int {
	for index, card := range textureLayout.cards {
		if contains(card, x, y) {
			return index
		}
	}
	return -1
}

func textureDraftIndex() int {
	for index, item := range texturePickerItems {
		if item.id == textureDraft {
			return index
		}
	}
	return 0
}

func moveTextureDraft(delta int) {
	next := textureDraftIndex() + delta
	if next < 0 || next >= len(texturePickerItems) {
		return
	}
	textureDraft = texturePickerItems[next].id
	pInvalidateRect.Call(hTexturePicker, 0, 0)
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
	fillCanvas(hdc, client)
	drawAmbientLight(hdc, w, h)

	text(hdc, tr("textureEyebrow"), rect{32, 19, w - 32, 42}, hFontSmall, th.textMuted, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	text(hdc, tr("textureTitle"), rect{32, 41, w - 32, 76}, hFontTitle, th.textPrimary, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	text(hdc, tr("textureSubtitle"), rect{80, 75, w - 80, 107}, hFontSmall, th.textSecondary, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)

	for index, item := range texturePickerItems {
		drawTextureCard(hdc, textureLayout.cards[index], item, item.id == textureDraft, index == textureHoverCard)
	}

	text(hdc, "✓  "+tr("textureApplied"), rect{34, h - 73, textureLayout.close.Left - 20, h - 25}, hFontSmall, th.textSecondary, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	cardOutlined(hdc, textureLayout.close, 23, th.stroke)
	text(hdc, tr("close"), inset(textureLayout.close, 8), hFontButton, th.textPrimary, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	shade(hdc, textureLayout.apply, 23, 12, 6, 43)
	accentFill(hdc, textureLayout.apply, 23)
	drawTextureButtonShimmer(hdc, textureLayout.apply)
	text(hdc, tr("textureApply")+"   ›", inset(textureLayout.apply, 8), hFontButton, th.textOnAccent, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
}

func drawTextureCard(hdc uintptr, card rect, item texturePickerItem, selected, hovered bool) {
	stroke := th.stroke
	switch {
	case selected:
		stroke = th.accentStroke
		glow(hdc, (card.Left+card.Right)/2, card.Top+height(card)/2, width(card)*3/4, height(card)*3/5, th.glowCool, 26)
		shade(hdc, card, 24, 18, 9, 42)
	case hovered:
		// The pointer lifts an unselected card so it reads as reachable.
		card = rect{card.Left, card.Top - 3, card.Right, card.Bottom - 3}
		stroke = th.accentStroke
		shade(hdc, card, 24, 16, 8, 34)
	default:
		shade(hdc, card, 24, 13, 7, 24)
	}
	cardOutlined(hdc, card, 24, stroke)
	preview := rect{card.Left + 13, card.Top + 13, card.Right - 13, card.Top + min32(205, height(card)*55/100)}
	drawTexturePreview(hdc, preview, item.id, selected)
	if selected {
		// The swatch underneath can be any brightness, so the badge carries its
		// own dark pill instead of relying on contrast with the material.
		badge := rect{preview.Left + 10, preview.Top + 10, preview.Left + 128, preview.Top + 36}
		drawSpatialRoundedMaterial(hdc, badge, 13, rgb(18, 22, 34), rgb(10, 13, 22), th.accentStroke)
		text(hdc, "✓  "+tr("textureSelected"), inset(badge, 6), hFontSmall, rgb(232, 237, 255), DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	}
	text(hdc, tr(item.titleKey), rect{card.Left + 14, preview.Bottom + 11, card.Right - 14, preview.Bottom + 41}, hFontHeading, th.textPrimary, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	text(hdc, tr(item.descriptionKey), rect{card.Left + 17, preview.Bottom + 43, card.Right - 17, card.Bottom - 13}, hFontSmall, th.textSecondary, DT_CENTER|DT_TOP|DT_WORDBREAK)
}

func drawTexturePreview(hdc uintptr, preview rect, kind string, selected bool) {
	bitmap := texturePreviewBitmap(kind, width(preview), height(preview))
	if !drawSpatialCachedBitmap(hdc, bitmap, preview.Left, preview.Top) {
		drawSpatialRoundedMaterial(hdc, preview, 18, th.sunken, th.sunkenAlt, th.stroke)
	}
	if selected {
		// A rim of accent light around the chosen swatch, inside its own edge.
		outlineRounded(hdc, inset(preview, 1), 34, th.accent, 2)
	}
}

// outlineRounded strokes a rounded rectangle without filling it.
func outlineRounded(hdc uintptr, r rect, diameter int32, color uintptr, lineWidth int) {
	p := pen(PS_SOLID, lineWidth, color)
	nullBrush, _, _ := pGetStockObject.Call(NULL_BRUSH)
	oldPen, _, _ := pSelectObject.Call(hdc, p)
	oldBrush, _, _ := pSelectObject.Call(hdc, nullBrush)
	pRoundRect.Call(hdc, i32arg(r.Left), i32arg(r.Top), i32arg(r.Right), i32arg(r.Bottom), uintptr(uint32(diameter)), uintptr(uint32(diameter)))
	pSelectObject.Call(hdc, oldBrush)
	pSelectObject.Call(hdc, oldPen)
	pDeleteObject.Call(p)
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
