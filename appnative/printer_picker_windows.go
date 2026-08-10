//go:build windows

package main

import (
	"fmt"
	"math"
	"runtime/debug"
	"strings"
	"syscall"
	"unsafe"

	"flashfitai/shared"
)

const (
	printerPickerClassName   = "FlashFitAI_Spatial_PrinterPicker_v1"
	idPrinterPickerAnimation = 5103

	WM_MOUSEWHEEL = 0x020A
	WM_KEYDOWN    = 0x0100
	VK_ESCAPE     = 0x1B
	VK_RETURN     = 0x0D
	VK_UP         = 0x26
	VK_DOWN       = 0x28
)

type printerPickerLayout struct {
	list    rect
	preview rect
	apply   rect
	close   rect
	rows    []rect
}

var (
	hPrinterPicker             uintptr
	printerWndProcCallback     uintptr
	printerClassRegistered     bool
	printerDraftIndex          int
	printerPickerScroll        int
	printerLayout              printerPickerLayout
	printerPickerAnimationTick uint32
	printerPickerBuffer        windowBackBuffer
)

func registerPrinterPickerClass() bool {
	if printerClassRegistered {
		return true
	}
	cursor, _, _ := pLoadCursor.Call(0, IDC_ARROW)
	cls := utf16Ptr(printerPickerClassName)
	printerWndProcCallback = syscall.NewCallback(printerWindowProc)
	wc := wndClassEx{
		CbSize: uint32(unsafe.Sizeof(wndClassEx{})), LpfnWndProc: printerWndProcCallback,
		HInstance: appInstance, HIcon: hAppIcon, HCursor: cursor,
		HbrBackground: COLOR_BTNFACE + 1, LpszClassName: cls, HIconSm: hAppIcon,
	}
	atom, _, err := pRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc)))
	if atom == 0 {
		writeLog(fmt.Sprintf("printer picker class failed: %v", err))
		return false
	}
	printerClassRegistered = true
	return true
}

func showPrinterPicker() {
	if app.importing {
		return
	}
	if len(app.printerChoices) == 0 {
		startDiscovery()
		return
	}
	if hPrinterPicker != 0 {
		pSetForegroundWindow.Call(hPrinterPicker)
		return
	}
	if !registerPrinterPickerClass() {
		return
	}
	printerDraftIndex = app.printerIndex
	if printerDraftIndex < 0 || printerDraftIndex >= len(app.printerChoices) {
		printerDraftIndex = 0
	}
	ensurePrinterDraftVisible()

	outerW, outerH := int32(980), int32(690)
	x, y := int32(120), int32(70)
	var parent rect
	if ok, _, _ := pGetWindowRect.Call(mainHwnd, uintptr(unsafe.Pointer(&parent))); ok != 0 {
		x = parent.Left + (width(parent)-outerW)/2
		y = parent.Top + (height(parent)-outerH)/2
	}
	cls := utf16Ptr(printerPickerClassName)
	title := utf16Ptr(tr("printerTitle"))
	style := uintptr(WS_CAPTION | WS_SYSMENU | WS_CLIPCHILDREN | WS_VISIBLE)
	pEnableWindow.Call(mainHwnd, 0)
	h, _, err := pCreateWindowEx.Call(0, uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(title)), style, i32arg(x), i32arg(y), uintptr(outerW), uintptr(outerH), mainHwnd, 0, appInstance, 0)
	if h == 0 {
		pEnableWindow.Call(mainHwnd, 1)
		writeLog(fmt.Sprintf("printer picker create failed: %v", err))
		return
	}
	hPrinterPicker = h
	setPremiumWindowMaterial(h)
	pShowWindow.Call(h, SW_SHOW)
	pUpdateWindow.Call(h)
	pSetForegroundWindow.Call(h)
}

func closePrinterPicker(apply bool) {
	if apply && printerDraftIndex >= 0 && printerDraftIndex < len(app.printerChoices) {
		selectDiscoveredMachine(printerDraftIndex)
	}
	if hPrinterPicker != 0 {
		pDestroyWindow.Call(hPrinterPicker)
	}
}

func printerWindowProc(hwnd uintptr, message uint32, wParam, lParam uintptr) (ret uintptr) {
	defer func() {
		if recovered := recover(); recovered != nil {
			writeLog(fmt.Sprintf("PANIC printer wndproc msg=%x: %v\n%s", message, recovered, debug.Stack()))
			ret, _, _ = pDefWindowProc.Call(hwnd, uintptr(message), wParam, lParam)
		}
	}()
	switch message {
	case WM_GETMINMAXINFO:
		mmi := (*minMaxInfo)(unsafe.Pointer(lParam))
		mmi.MinTrackSize = point{X: 780, Y: 560}
		return 0
	case WM_CREATE:
		hPrinterPicker = hwnd
		pSetTimer.Call(hwnd, idPrinterPickerAnimation, spatialAnimationInterval, 0)
		return 0
	case WM_PAINT:
		paintPrinterPicker(hwnd)
		return 0
	case WM_ERASEBKGND:
		return 1
	case WM_TIMER:
		foreground, _, _ := pGetForegroundWindow.Call()
		if wParam == idPrinterPickerAnimation && foreground == hwnd {
			printerPickerAnimationTick++
			pInvalidateRect.Call(hwnd, 0, 0)
		}
		return 0
	case WM_MOUSEWHEEL:
		delta := int16((wParam >> 16) & 0xffff)
		if delta > 0 {
			printerPickerScroll--
		} else {
			printerPickerScroll++
		}
		clampPrinterScroll()
		pInvalidateRect.Call(hwnd, 0, 0)
		return 0
	case WM_KEYDOWN:
		switch wParam {
		case VK_ESCAPE:
			closePrinterPicker(false)
		case VK_RETURN:
			closePrinterPicker(true)
		case VK_UP:
			if printerDraftIndex > 0 {
				printerDraftIndex--
				ensurePrinterDraftVisible()
				pInvalidateRect.Call(hwnd, 0, 0)
			}
		case VK_DOWN:
			if printerDraftIndex < len(app.printerChoices)-1 {
				printerDraftIndex++
				ensurePrinterDraftVisible()
				pInvalidateRect.Call(hwnd, 0, 0)
			}
		}
		return 0
	case WM_LBUTTONUP:
		x, y := pointFromLParam(lParam)
		if row := printerPickerHitRow(x, y); row >= 0 {
			printerDraftIndex = row
			ensurePrinterDraftVisible()
			pInvalidateRect.Call(hwnd, 0, 0)
			return 0
		}
		if contains(printerLayout.apply, x, y) {
			closePrinterPicker(true)
			return 0
		}
		if contains(printerLayout.close, x, y) {
			closePrinterPicker(false)
			return 0
		}
		return 0
	case WM_CLOSE:
		closePrinterPicker(false)
		return 0
	case WM_DESTROY:
		pKillTimer.Call(hwnd, idPrinterPickerAnimation)
		printerPickerBuffer.reset()
		hPrinterPicker = 0
		if mainHwnd != 0 {
			pEnableWindow.Call(mainHwnd, 1)
			pSetForegroundWindow.Call(mainHwnd)
		}
		return 0
	}
	r, _, _ := pDefWindowProc.Call(hwnd, uintptr(message), wParam, lParam)
	return r
}

func calculatePrinterLayout(w, h int32) printerPickerLayout {
	margin, gap := int32(30), int32(20)
	top := int32(112)
	bottom := h - 96
	listW := min32(410, w*44/100)
	list := rect{margin, top, margin + listW, bottom}
	preview := rect{list.Right + gap, top, w - margin, bottom}
	rowH := int32(72)
	rowGap := int32(10)
	count := int((height(list) + rowGap) / (rowH + rowGap))
	if count < 1 {
		count = 1
	}
	rows := make([]rect, 0, count)
	y := list.Top + 12
	for i := 0; i < count; i++ {
		rows = append(rows, rect{list.Left + 12, y, list.Right - 12, y + rowH})
		y += rowH + rowGap
	}
	return printerPickerLayout{
		list: list, preview: preview,
		close: rect{w - margin - 360, h - 72, w - margin - 238, h - 26},
		apply: rect{w - margin - 224, h - 72, w - margin, h - 26},
		rows: rows,
	}
}

func printerVisibleRows() int {
	if len(printerLayout.rows) == 0 {
		return 1
	}
	return len(printerLayout.rows)
}

func clampPrinterScroll() {
	maxScroll := len(app.printerChoices) - printerVisibleRows()
	if maxScroll < 0 {
		maxScroll = 0
	}
	if printerPickerScroll < 0 {
		printerPickerScroll = 0
	}
	if printerPickerScroll > maxScroll {
		printerPickerScroll = maxScroll
	}
}

func ensurePrinterDraftVisible() {
	if printerDraftIndex < 0 {
		printerDraftIndex = 0
	}
	if printerDraftIndex >= len(app.printerChoices) {
		printerDraftIndex = len(app.printerChoices) - 1
	}
	if printerDraftIndex < printerPickerScroll {
		printerPickerScroll = printerDraftIndex
	}
	visible := printerVisibleRows()
	if printerDraftIndex >= printerPickerScroll+visible {
		printerPickerScroll = printerDraftIndex - visible + 1
	}
	clampPrinterScroll()
}

func printerPickerHitRow(x, y int32) int {
	for i, row := range printerLayout.rows {
		index := printerPickerScroll + i
		if index >= len(app.printerChoices) {
			break
		}
		if contains(row, x, y) {
			return index
		}
	}
	return -1
}

func paintPrinterPicker(hwnd uintptr) {
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
	if !printerPickerBuffer.ensure(hdc, w, h) {
		drawPrinterPickerScene(hdc, client)
		return
	}
	drawPrinterPickerScene(printerPickerBuffer.dc, client)
	pBitBlt.Call(hdc, 0, 0, uintptr(w), uintptr(h), printerPickerBuffer.dc, 0, 0, SRCCOPY)
}

func drawPrinterPickerScene(hdc uintptr, client rect) {
	w, h := width(client), height(client)
	printerLayout = calculatePrinterLayout(w, h)
	clampPrinterScroll()

	background := brush(rgb(247, 249, 253))
	pFillRect.Call(hdc, uintptr(unsafe.Pointer(&client)), background)
	pDeleteObject.Call(background)
	drawSpatialGlow(hdc, w*18/100, 24, 250, 170, rgb(211, 224, 255), 31)
	drawSpatialGlow(hdc, w*83/100, h-54, 330, 230, rgb(222, 212, 255), 24)
	drawSpatialGlow(hdc, w*58/100, 72, 240, 150, rgb(190, 222, 255), 18)

	text(hdc, tr("printerEyebrow"), rect{30, 18, w - 30, 40}, hFontSmall, rgb(88, 103, 190), DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	text(hdc, tr("printerTitle"), rect{30, 41, w - 30, 77}, hFontTitle, rgb(23, 28, 40), DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	text(hdc, tr("printerSubtitle"), rect{80, 76, w - 80, 105}, hFontSmall, rgb(91, 102, 128), DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)

	drawSpatialSoftShadow(hdc, printerLayout.list, 24, 13, 7, rgb(62, 75, 122), 24)
	drawSpatialRoundedMaterial(hdc, printerLayout.list, 24, rgb(255, 255, 255), rgb(244, 247, 252), rgb(225, 231, 242))
	for i, row := range printerLayout.rows {
		index := printerPickerScroll + i
		if index >= len(app.printerChoices) {
			break
		}
		drawPrinterChoiceRow(hdc, row, app.printerChoices[index], index == printerDraftIndex, index == app.printerIndex)
	}

	if printerPickerScroll > 0 {
		text(hdc, "^", rect{printerLayout.list.Left, printerLayout.list.Top + 2, printerLayout.list.Right, printerLayout.list.Top + 22}, hFontSmall, rgb(105, 117, 145), DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	}
	if printerPickerScroll+len(printerLayout.rows) < len(app.printerChoices) {
		text(hdc, "v", rect{printerLayout.list.Left, printerLayout.list.Bottom - 22, printerLayout.list.Right, printerLayout.list.Bottom - 2}, hFontSmall, rgb(105, 117, 145), DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	}

	if printerDraftIndex >= 0 && printerDraftIndex < len(app.printerChoices) {
		drawPrinterPreviewCard(hdc, printerLayout.preview, app.printerChoices[printerDraftIndex])
	}

	drawSpatialRoundedMaterial(hdc, printerLayout.close, 22, rgb(255, 255, 255), rgb(244, 247, 252), rgb(220, 226, 239))
	text(hdc, tr("close"), inset(printerLayout.close, 7), hFontButton, rgb(56, 67, 91), DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	drawSpatialSoftShadow(hdc, printerLayout.apply, 22, 11, 6, rgb(71, 66, 204), 42)
	drawSpatialRoundedMaterial(hdc, printerLayout.apply, 22, rgb(77, 147, 255), rgb(111, 67, 241), rgb(135, 151, 255))
	drawPrinterButtonShimmer(hdc, printerLayout.apply)
	text(hdc, tr("printerUse")+"   >", inset(printerLayout.apply, 8), hFontButton, rgb(255, 255, 255), DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
}

func drawPrinterChoiceRow(hdc uintptr, row rect, choice shared.DiscoveredMachine, selected, active bool) {
	stroke := rgb(224, 230, 241)
	top := rgb(255, 255, 255)
	bottom := rgb(247, 249, 253)
	titleColor := rgb(31, 37, 52)
	subColor := rgb(93, 104, 130)
	if selected {
		drawSpatialGlow(hdc, row.Left+48, (row.Top+row.Bottom)/2, 72, 45, rgb(111, 139, 255), 28)
		drawSpatialSoftShadow(hdc, row, 20, 11, 5, rgb(74, 68, 205), 33)
		stroke = rgb(107, 124, 245)
		top = rgb(250, 252, 255)
		bottom = rgb(239, 244, 255)
		titleColor = rgb(24, 32, 76)
		subColor = rgb(73, 87, 168)
	}
	drawSpatialRoundedMaterial(hdc, row, 20, top, bottom, stroke)
	icon := rect{row.Left + 15, row.Top + 13, row.Left + 61, row.Bottom - 13}
	drawSpatialRoundedMaterial(hdc, icon, 16, rgb(239, 244, 255), rgb(229, 236, 252), rgb(210, 219, 242))
	drawPrinterIcon(hdc, (icon.Left+icon.Right)/2, (icon.Top+icon.Bottom)/2, rgb(72, 92, 212))
	title := choice.Label
	if strings.TrimSpace(title) == "" {
		title = choice.Brand + " " + choice.Model
	}
	text(hdc, title, rect{row.Left + 73, row.Top + 11, row.Right - 56, row.Top + 37}, hFontBody, titleColor, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	text(hdc, selectedNozzleFromChoice(choice)+"  |  "+choice.Brand, rect{row.Left + 73, row.Top + 36, row.Right - 56, row.Bottom - 9}, hFontSmall, subColor, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	if active {
		badge := rect{row.Right - 45, row.Top + 20, row.Right - 15, row.Top + 50}
		drawSpatialRoundedMaterial(hdc, badge, 15, rgb(92, 135, 255), rgb(92, 80, 241), rgb(130, 151, 255))
		text(hdc, "OK", badge, hFontSmall, rgb(255, 255, 255), DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	}
}

func drawPrinterPreviewCard(hdc uintptr, card rect, choice shared.DiscoveredMachine) {
	drawSpatialSoftShadow(hdc, card, 26, 16, 8, rgb(61, 75, 123), 25)
	drawSpatialRoundedMaterial(hdc, card, 26, rgb(255, 255, 255), rgb(244, 247, 252), rgb(224, 230, 242))
	inner := inset(card, 22)
	photo := rect{inner.Left, inner.Top, inner.Right, inner.Top + height(inner)*62/100}
	drawPrinterProductPhoto(hdc, photo, choice)

	title := choice.Label
	if strings.TrimSpace(title) == "" {
		title = choice.Brand + " " + choice.Model
	}
	text(hdc, title, rect{inner.Left, photo.Bottom + 18, inner.Right, photo.Bottom + 52}, hFontTitle, rgb(24, 29, 41), DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	printer, ok := printerProfileFromChoice(choice)
	build := tr("printerBuildUnknown")
	motion := choice.Brand
	if ok {
		build = fmt.Sprintf("%.0f x %.0f x %.0f mm", printer.BuildVolume[0], printer.BuildVolume[1], printer.BuildVolume[2])
		motion = printer.Motion
		if printer.Enclosed {
			motion += " | " + tr("printerEnclosed")
		} else {
			motion += " | " + tr("printerOpenFrame")
		}
	}
	nozzle := selectedNozzleFromChoice(choice)
	statsTop := photo.Bottom + 64
	drawPrinterInfoChip(hdc, rect{inner.Left, statsTop, inner.Left + width(inner)/3 - 8, statsTop + 54}, tr("printerNozzle"), nozzle)
	drawPrinterInfoChip(hdc, rect{inner.Left + width(inner)/3 + 4, statsTop, inner.Left + width(inner)*2/3 - 4, statsTop + 54}, tr("printerBuild"), build)
	drawPrinterInfoChip(hdc, rect{inner.Left + width(inner)*2/3 + 8, statsTop, inner.Right, statsTop + 54}, tr("printerMotion"), motion)
	text(hdc, tr("printerPreviewOnly"), rect{inner.Left, inner.Bottom - 31, inner.Right, inner.Bottom - 5}, hFontSmall, rgb(102, 113, 138), DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
}

func drawPrinterInfoChip(hdc uintptr, r rect, label, value string) {
	drawSpatialRoundedMaterial(hdc, r, 17, rgb(250, 252, 255), rgb(241, 245, 253), rgb(224, 230, 242))
	text(hdc, label, rect{r.Left + 12, r.Top + 6, r.Right - 12, r.Top + 24}, hFontSmall, rgb(99, 109, 134), DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	text(hdc, value, rect{r.Left + 12, r.Top + 24, r.Right - 12, r.Bottom - 5}, hFontSmall, rgb(41, 51, 79), DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
}

func drawPrinterProductPhoto(hdc uintptr, photo rect, choice shared.DiscoveredMachine) {
	drawSpatialRoundedMaterial(hdc, photo, 22, rgb(28, 32, 43), rgb(9, 12, 20), rgb(61, 70, 93))
	saved, _, _ := pSaveDC.Call(hdc)
	region, _, _ := pCreateRoundRectRgn.Call(i32arg(photo.Left), i32arg(photo.Top), i32arg(photo.Right+1), i32arg(photo.Bottom+1), 44, 44)
	if region != 0 {
		pSelectClipRgn.Call(hdc, region)
	}
	drawSpatialGlow(hdc, photo.Left+width(photo)*72/100, photo.Top+height(photo)*27/100, width(photo)/3, height(photo)/3, rgb(118, 141, 255), 42)
	drawSpatialGlow(hdc, photo.Left+width(photo)*35/100, photo.Top+height(photo)*62/100, width(photo)/3, height(photo)/4, rgb(73, 111, 221), 30)
	for i := int32(0); i <= 5; i++ {
		y := photo.Top + height(photo)*72/100 + i*height(photo)/18
		line(hdc, photo.Left+30, y, photo.Right-30, y, rgb(42, 50, 72), 1)
	}

	cx := (photo.Left + photo.Right) / 2
	baseY := photo.Top + height(photo)*72/100
	scale := min32(width(photo), height(photo))
	printer, _ := printerProfileFromChoice(choice)
	enclosed := printer.Enclosed || strings.Contains(strings.ToLower(choice.Model), "pro") || strings.Contains(strings.ToLower(choice.Model), "x1") || strings.Contains(strings.ToLower(choice.Model), "p1s")
	bedslinger := strings.Contains(strings.ToLower(choice.Model), "a1")
	accent := rgb(111, 137, 255)
	if strings.EqualFold(choice.Brand, "Flashforge") {
		accent = rgb(76, 152, 255)
	}
	if bedslinger {
		drawBedslingerPrinterRender(hdc, cx, baseY, scale, accent)
	} else if enclosed {
		drawEnclosedPrinterRender(hdc, cx, baseY, scale, accent)
	} else {
		drawCoreXYPrinterRender(hdc, cx, baseY, scale, accent)
	}
	if region != 0 {
		pDeleteObject.Call(region)
	}
	if saved != 0 {
		pRestoreDC.Call(hdc, saved)
	}
}

func drawEnclosedPrinterRender(hdc uintptr, cx, baseY, scale int32, accent uintptr) {
	w := scale * 47 / 100
	h := scale * 42 / 100
	body := rect{cx - w/2, baseY - h, cx + w/2, baseY}
	drawSpatialSoftShadow(hdc, rect{body.Left + 8, body.Bottom - 4, body.Right - 8, body.Bottom + 12}, 18, 16, 5, rgb(0, 0, 0), 55)
	drawSpatialRoundedMaterial(hdc, body, 20, rgb(235, 240, 250), rgb(198, 207, 224), rgb(123, 137, 164))
	window := rect{body.Left + 18, body.Top + 18, body.Right - 18, body.Bottom - 28}
	drawSpatialRoundedMaterial(hdc, window, 13, rgb(34, 42, 61), rgb(15, 18, 28), rgb(91, 105, 134))
	drawSpatialGlow(hdc, window.Left+width(window)*65/100, window.Top+height(window)*34/100, width(window)/4, height(window)/3, accent, 36)
	line(hdc, window.Left+18, window.Top+20, window.Right-18, window.Top+20, accent, 2)
	line(hdc, cx, window.Top+22, cx, window.Bottom-18, rgb(150, 166, 205), 2)
	tool := rect{cx - 22, window.Top + 42, cx + 22, window.Top + 71}
	drawSpatialRoundedMaterial(hdc, tool, 8, rgb(235, 240, 252), rgb(197, 207, 230), accent)
	line(hdc, cx, tool.Bottom, cx-8, tool.Bottom+20, rgb(219, 226, 243), 2)
	line(hdc, cx, tool.Bottom, cx+8, tool.Bottom+20, rgb(219, 226, 243), 2)
	drawSpatialRoundedMaterial(hdc, rect{body.Left + 30, body.Bottom - 22, body.Right - 30, body.Bottom - 10}, 6, rgb(36, 43, 61), rgb(23, 28, 41), rgb(89, 99, 125))
}

func drawCoreXYPrinterRender(hdc uintptr, cx, baseY, scale int32, accent uintptr) {
	w := scale * 52 / 100
	h := scale * 39 / 100
	left, right := cx-w/2, cx+w/2
	top := baseY - h
	drawSpatialSoftShadow(hdc, rect{left + 8, baseY - 3, right - 8, baseY + 12}, 18, 16, 5, rgb(0, 0, 0), 52)
	for _, x := range []int32{left, right} {
		line(hdc, x, top, x, baseY, rgb(215, 224, 242), 5)
	}
	line(hdc, left, top, right, top, rgb(215, 224, 242), 5)
	line(hdc, left, baseY, right, baseY, rgb(176, 189, 216), 5)
	line(hdc, left+22, top+30, right-22, top+30, accent, 2)
	toolX := cx + int32(math.Sin(float64(printerPickerAnimationTick)*0.08)*18)
	drawSpatialRoundedMaterial(hdc, rect{toolX - 21, top + 37, toolX + 21, top + 68}, 8, rgb(236, 241, 252), rgb(198, 208, 231), accent)
	line(hdc, toolX, top+69, toolX, top+91, rgb(218, 226, 244), 2)
	drawSpatialRoundedMaterial(hdc, rect{left + 28, baseY - 28, right - 28, baseY - 12}, 6, rgb(44, 52, 72), rgb(25, 30, 45), rgb(93, 105, 132))
}

func drawBedslingerPrinterRender(hdc uintptr, cx, baseY, scale int32, accent uintptr) {
	w := scale * 56 / 100
	h := scale * 40 / 100
	left, right := cx-w/2, cx+w/2
	top := baseY - h
	drawSpatialSoftShadow(hdc, rect{left + 6, baseY - 1, right - 6, baseY + 14}, 20, 15, 5, rgb(0, 0, 0), 54)
	line(hdc, left+18, top+16, left+18, baseY-18, rgb(217, 225, 242), 5)
	line(hdc, right-18, top+16, right-18, baseY-18, rgb(217, 225, 242), 5)
	line(hdc, left+18, top+16, right-18, top+16, rgb(217, 225, 242), 5)
	toolX := cx + int32(math.Sin(float64(printerPickerAnimationTick)*0.08)*16)
	line(hdc, left+30, top+45, right-30, top+45, accent, 2)
	drawSpatialRoundedMaterial(hdc, rect{toolX - 19, top + 52, toolX + 19, top + 82}, 8, rgb(237, 242, 252), rgb(199, 208, 231), accent)
	bedShift := int32(math.Cos(float64(printerPickerAnimationTick)*0.06) * 8)
	bed := rect{cx - w*36/100 + bedShift, baseY - 42, cx + w*36/100 + bedShift, baseY - 22}
	drawSpatialRoundedMaterial(hdc, bed, 7, rgb(52, 61, 82), rgb(30, 35, 49), rgb(101, 115, 144))
	line(hdc, cx, top+83, cx, baseY-44, rgb(206, 216, 237), 2)
}

func drawPrinterButtonShimmer(hdc uintptr, button rect) {
	saved, _, _ := pSaveDC.Call(hdc)
	region, _, _ := pCreateRoundRectRgn.Call(i32arg(button.Left), i32arg(button.Top), i32arg(button.Right+1), i32arg(button.Bottom+1), 44, 44)
	if region != 0 {
		pSelectClipRgn.Call(hdc, region)
		phase := float64(printerPickerAnimationTick%210) / 209.0
		x := button.Left - 60 + int32(phase*float64(width(button)+120))
		drawSpatialGlow(hdc, x, (button.Top+button.Bottom)/2, 42, 34, rgb(255, 255, 255), 44)
		pDeleteObject.Call(region)
	}
	if saved != 0 {
		pRestoreDC.Call(hdc, saved)
	}
}

func printerProfileFromChoice(choice shared.DiscoveredMachine) (shared.PrinterProfile, bool) {
	if printer, err := shared.ResolvePrinterProfile(choice.Path); err == nil {
		return printer, true
	}
	if printer, ok := shared.PrinterByID(choice.PrinterID); ok {
		if choice.NozzleMM > 0 {
			printer.NozzleDiameter = choice.NozzleMM
		}
		return printer, true
	}
	return shared.PrinterProfile{}, false
}

func selectedNozzleFromChoice(choice shared.DiscoveredMachine) string {
	if choice.NozzleMM > 0 {
		return formatNozzleMM(choice.NozzleMM)
	}
	if printer, ok := printerProfileFromChoice(choice); ok {
		return formatNozzleMM(printer.NozzleDiameter)
	}
	return formatNozzleMM(0.4)
}

func selectedNozzleLabel() string {
	if printer, ok := selectedPrinter(); ok {
		return formatNozzleMM(printer.NozzleDiameter)
	}
	return tr("printerNozzle")
}

func formatNozzleMM(mm float64) string {
	if mm <= 0 {
		mm = 0.4
	}
	value := strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", mm), "0"), ".")
	return value + " mm"
}
