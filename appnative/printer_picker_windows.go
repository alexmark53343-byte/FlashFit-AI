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
	printerOptions             []shared.DiscoveredMachine
)

// The picker lists every supported machine, not only the ones whose profile is
// installed. Without a slicer present the discovered set is empty, and the user
// still needs to tell FlashFit which printer they own.
func buildPrinterOptions() []shared.DiscoveredMachine {
	options := append([]shared.DiscoveredMachine(nil), app.printerChoices...)
	installed := make(map[string]bool, len(options))
	for _, choice := range options {
		installed[choice.PrinterID] = true
	}
	for _, printer := range shared.SupportedPrinters() {
		if installed[printer.ID] {
			continue
		}
		options = append(options, shared.DiscoveredMachine{
			PrinterID: printer.ID,
			Brand:     printer.Brand,
			Model:     printer.Model,
			Label:     printer.Brand + " " + printer.Model,
			NozzleMM:  printer.NozzleDiameter,
		})
	}
	return options
}

func printerOptionInstalled(choice shared.DiscoveredMachine) bool {
	return strings.TrimSpace(choice.Path) != ""
}

func printerOptionActive(choice shared.DiscoveredMachine) bool {
	return choice.PrinterID != "" && choice.PrinterID == app.printer.ID
}

// selectPrinterOption applies either an installed profile or a catalog entry.
func selectPrinterOption(index int) {
	if app.importing || index < 0 || index >= len(printerOptions) {
		return
	}
	choice := printerOptions[index]
	if printerOptionInstalled(choice) {
		for i, discovered := range app.printerChoices {
			if discovered.Path == choice.Path && discovered.NozzleMM == choice.NozzleMM {
				selectDiscoveredMachine(i)
				return
			}
		}
	}
	printer, ok := shared.PrinterByID(choice.PrinterID)
	if !ok {
		return
	}
	app.printer = printer
	app.machine = ""
	app.printerIndex = -1
	// An explicit catalog pick must survive the next discovery pass.
	app.manualMachine = true
	app.processChoices = make(map[string]string, 3)
	for _, quality := range []string{"low", "balanced", "perfect"} {
		app.processChoices[quality] = chooseProcessForPrinter(app.profiles.Processes, quality, printer, app.profileMeta)
	}
	app.manualProcess = false
	autoSelectProfiles()
	renderAnalysis()
	renderProfiles()
	refreshReady()
	setStatusKey("statusPrinterCatalogOnly", printer.Brand+" "+printer.Model)
	invalidateSpatial()
}

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
	if hPrinterPicker != 0 {
		pSetForegroundWindow.Call(hPrinterPicker)
		return
	}
	if !registerPrinterPickerClass() {
		return
	}
	printerOptions = buildPrinterOptions()
	if len(printerOptions) == 0 {
		return
	}
	printerDraftIndex = 0
	for i, choice := range printerOptions {
		if printerOptionActive(choice) {
			printerDraftIndex = i
			break
		}
	}
	printerPickerScroll = 0
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
	if apply {
		selectPrinterOption(printerDraftIndex)
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
			if printerDraftIndex < len(printerOptions)-1 {
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
	maxScroll := len(printerOptions) - printerVisibleRows()
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
	if printerDraftIndex >= len(printerOptions) {
		printerDraftIndex = len(printerOptions) - 1
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
		if index >= len(printerOptions) {
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

	fillCanvas(hdc, client)
	drawAmbientLight(hdc, w, h)

	eyebrow(hdc, tr("printerEyebrow"), rect{30, 18, w - 30, 40}, DT_CENTER|DT_VCENTER)
	text(hdc, tr("printerTitle"), rect{30, 41, w - 30, 77}, hFontTitle, th.textPrimary, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	text(hdc, tr("printerSubtitle"), rect{80, 76, w - 80, 105}, hFontSmall, th.textSecondary, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)

	shade(hdc, printerLayout.list, 24, 13, 7, 24)
	card(hdc, printerLayout.list, 24)
	for i, row := range printerLayout.rows {
		index := printerPickerScroll + i
		if index >= len(printerOptions) {
			break
		}
		drawPrinterChoiceRow(hdc, row, printerOptions[index], index == printerDraftIndex, printerOptionActive(printerOptions[index]))
	}

	if printerPickerScroll > 0 {
		text(hdc, "▲", rect{printerLayout.list.Left, printerLayout.list.Top + 2, printerLayout.list.Right, printerLayout.list.Top + 22}, hFontSmall, th.textMuted, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	}
	if printerPickerScroll+len(printerLayout.rows) < len(printerOptions) {
		text(hdc, "▼", rect{printerLayout.list.Left, printerLayout.list.Bottom - 22, printerLayout.list.Right, printerLayout.list.Bottom - 2}, hFontSmall, th.textMuted, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	}

	if printerDraftIndex >= 0 && printerDraftIndex < len(printerOptions) {
		drawPrinterPreviewCard(hdc, printerLayout.preview, printerOptions[printerDraftIndex])
	}

	card(hdc, printerLayout.close, 22)
	text(hdc, tr("close"), inset(printerLayout.close, 7), hFontButton, th.textPrimary, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	shade(hdc, printerLayout.apply, 22, 11, 6, 42)
	accentFill(hdc, printerLayout.apply, 22)
	drawPrinterButtonShimmer(hdc, printerLayout.apply)
	text(hdc, tr("printerUse"), inset(printerLayout.apply, 8), hFontButton, th.textOnAccent, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
}

func drawPrinterChoiceRow(hdc uintptr, row rect, choice shared.DiscoveredMachine, selected, active bool) {
	titleColor := th.textPrimary
	subColor := th.textMuted
	if selected {
		shade(hdc, row, 20, 11, 5, 33)
		accentTint(hdc, row, 20)
		subColor = th.accentText
	} else {
		sunkenChip(hdc, row, 20)
	}
	icon := rect{row.Left + 15, row.Top + 13, row.Left + 61, row.Bottom - 13}
	drawSpatialRoundedMaterial(hdc, icon, 16, th.surface, th.surfaceAlt, th.stroke)
	iconColor := th.accent
	if !printerOptionInstalled(choice) {
		iconColor = th.textMuted
	}
	drawPrinterIcon(hdc, (icon.Left+icon.Right)/2, (icon.Top+icon.Bottom)/2, iconColor)
	title := choice.Label
	if strings.TrimSpace(title) == "" {
		title = choice.Brand + " " + choice.Model
	}
	text(hdc, title, rect{row.Left + 73, row.Top + 11, row.Right - 60, row.Top + 37}, hFontBody, titleColor, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	meta := selectedNozzleFromChoice(choice) + " · " + choice.Brand
	if !printerOptionInstalled(choice) {
		meta += " · " + tr("printerProfileMissing")
	}
	text(hdc, meta, rect{row.Left + 73, row.Top + 36, row.Right - 60, row.Bottom - 9}, hFontSmall, subColor, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	if active {
		badge := rect{row.Right - 46, (row.Top+row.Bottom)/2 - 13, row.Right - 14, (row.Top+row.Bottom)/2 + 13}
		accentFill(hdc, badge, 13)
		text(hdc, "✓", badge, hFontSmall, th.textOnAccent, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	}
}

func drawPrinterPreviewCard(hdc uintptr, cardRect rect, choice shared.DiscoveredMachine) {
	shade(hdc, cardRect, 26, 16, 8, 25)
	card(hdc, cardRect, 26)
	inner := inset(cardRect, 22)
	photo := rect{inner.Left, inner.Top, inner.Right, inner.Top + height(inner)*62/100}
	drawPrinterProductPhoto(hdc, photo, choice)

	title := choice.Label
	if strings.TrimSpace(title) == "" {
		title = choice.Brand + " " + choice.Model
	}
	text(hdc, title, rect{inner.Left, photo.Bottom + 18, inner.Right, photo.Bottom + 52}, hFontTitle, th.textPrimary, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
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
	footer := tr("printerPreviewOnly")
	if !printerOptionInstalled(choice) {
		footer = tr("printerProfileMissingHint")
	}
	text(hdc, footer, rect{inner.Left, inner.Bottom - 34, inner.Right, inner.Bottom - 4}, hFontSmall, th.textMuted, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
}

func drawPrinterInfoChip(hdc uintptr, r rect, label, value string) {
	sunkenChip(hdc, r, 17)
	eyebrow(hdc, label, rect{r.Left + 10, r.Top + 7, r.Right - 10, r.Top + 25}, DT_CENTER|DT_VCENTER)
	text(hdc, value, rect{r.Left + 10, r.Top + 25, r.Right - 10, r.Bottom - 6}, hFontSmall, th.textPrimary, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
}

func drawPrinterProductPhoto(hdc uintptr, photo rect, choice shared.DiscoveredMachine) {
	drawSpatialRoundedMaterial(hdc, photo, 22, th.stageTop, th.stageBottom, th.stageStroke)
	saved, _, _ := pSaveDC.Call(hdc)
	region, _, _ := pCreateRoundRectRgn.Call(i32arg(photo.Left), i32arg(photo.Top), i32arg(photo.Right+1), i32arg(photo.Bottom+1), 44, 44)
	if region != 0 {
		pSelectClipRgn.Call(hdc, region)
	}
	glow(hdc, photo.Left+width(photo)*70/100, photo.Top+height(photo)*28/100, width(photo)/3, height(photo)/3, th.glowCool, 40)
	glow(hdc, photo.Left+width(photo)*32/100, photo.Top+height(photo)*64/100, width(photo)/3, height(photo)/4, th.glowWarm, 28)

	printer, ok := printerProfileFromChoice(choice)
	if !ok {
		printer.BuildVolume = [3]float64{220, 220, 220}
	}
	drawBuildVolume(hdc, photo, printer)

	if region != 0 {
		pDeleteObject.Call(region)
	}
	if saved != 0 {
		pRestoreDC.Call(hdc, saved)
	}
}

// An isometric wireframe of the machine's real build envelope. It carries the
// actual millimetres, which is the thing that decides whether a model fits.
func drawBuildVolume(hdc uintptr, area rect, printer shared.PrinterProfile) {
	bx, by, bz := printer.BuildVolume[0], printer.BuildVolume[1], printer.BuildVolume[2]
	if bx <= 0 || by <= 0 || bz <= 0 {
		return
	}
	largest := math.Max(bx, math.Max(by, bz))
	nx, ny, nz := bx/largest, by/largest, bz/largest

	span := float64(min32(width(area), height(area)))
	unit := span * 0.36
	originX := float64(area.Left+area.Right) / 2
	originY := float64(area.Top) + float64(height(area))*0.70

	// Classic 30° isometric axes.
	const cos30, sin30 = 0.8660254, 0.5
	project := func(x, y, z float64) (int32, int32) {
		sx := originX + (x-y)*cos30*unit
		sy := originY - (x+y)*sin30*unit*0.62 - z*unit
		return int32(math.Round(sx)), int32(math.Round(sy))
	}

	// Bed grid.
	const divisions = 6
	for i := 0; i <= divisions; i++ {
		t := float64(i) / divisions
		ax, ay := project(t*nx, 0, 0)
		bxp, byp := project(t*nx, ny, 0)
		line(hdc, ax, ay, bxp, byp, th.stageGrid, 1)
		cx2, cy2 := project(0, t*ny, 0)
		dx2, dy2 := project(nx, t*ny, 0)
		line(hdc, cx2, cy2, dx2, dy2, th.stageGrid, 1)
	}

	corners := [8][3]float64{
		{0, 0, 0}, {nx, 0, 0}, {nx, ny, 0}, {0, ny, 0},
		{0, 0, nz}, {nx, 0, nz}, {nx, ny, nz}, {0, ny, nz},
	}
	var px, py [8]int32
	for i, c := range corners {
		px[i], py[i] = project(c[0], c[1], c[2])
	}
	base := [4][2]int{{0, 1}, {1, 2}, {2, 3}, {3, 0}}
	for _, e := range base {
		line(hdc, px[e[0]], py[e[0]], px[e[1]], py[e[1]], th.stageGridHi, 2)
	}
	for i := 0; i < 4; i++ {
		line(hdc, px[i], py[i], px[i+4], py[i+4], th.stageGridHi, 1)
	}
	top := [4][2]int{{4, 5}, {5, 6}, {6, 7}, {7, 4}}
	for _, e := range top {
		line(hdc, px[e[0]], py[e[0]], px[e[1]], py[e[1]], th.accentStroke, 1)
	}

	label := fmt.Sprintf("%.0f × %.0f × %.0f mm", bx, by, bz)
	text(hdc, label, rect{area.Left + 12, area.Bottom - 34, area.Right - 12, area.Bottom - 10}, hFontSmall, th.textSecondary, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
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
