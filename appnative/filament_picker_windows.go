//go:build windows

package main

import (
	"fmt"
	"math"
	"strings"
	"unsafe"

	"flashfitai/shared"
)

const idFilamentAnimation = 5104

type filamentPickerLayout struct {
	search  rect
	list    rect
	details rect
	apply   rect
	close   rect
	rows    []rect
}

var (
	filamentLayout              filamentPickerLayout
	filamentPickerScroll        int
	filamentPickerAnimationTick uint32
	filamentPickerBuffer        windowBackBuffer
)

func calculateFilamentLayout(w, h int32) filamentPickerLayout {
	margin, gap := int32(30), int32(18)
	search := rect{margin, 96, w - margin, 142}
	bottom := h - 96
	listW := min32(470, w*52/100)
	list := rect{margin, 166, margin + listW, bottom}
	details := rect{list.Right + gap, 166, w - margin, bottom}
	rowH := int32(76)
	rowGap := int32(10)
	count := int((height(list) - 24 + rowGap) / (rowH + rowGap))
	if count < 1 {
		count = 1
	}
	rows := make([]rect, 0, count)
	y := list.Top + 12
	for i := 0; i < count; i++ {
		rows = append(rows, rect{list.Left + 12, y, list.Right - 12, y + rowH})
		y += rowH + rowGap
	}
	return filamentPickerLayout{
		search: search, list: list, details: details,
		close: rect{w - margin - 360, h - 72, w - margin - 238, h - 26},
		apply: rect{w - margin - 224, h - 72, w - margin, h - 26},
		rows: rows,
	}
}

func filamentVisibleRows() int {
	if len(filamentLayout.rows) == 0 {
		return 1
	}
	return len(filamentLayout.rows)
}

func clampFilamentScroll() {
	maxScroll := len(app.filtered) - filamentVisibleRows()
	if maxScroll < 0 {
		maxScroll = 0
	}
	if filamentPickerScroll < 0 {
		filamentPickerScroll = 0
	}
	if filamentPickerScroll > maxScroll {
		filamentPickerScroll = maxScroll
	}
}

func selectedFilamentRow() int {
	if app.selected < 0 {
		return -1
	}
	for row, idx := range app.filtered {
		if idx == app.selected {
			return row
		}
	}
	return -1
}

func ensureSelectedFilamentVisible() {
	row := selectedFilamentRow()
	if row < 0 {
		clampFilamentScroll()
		return
	}
	if row < filamentPickerScroll {
		filamentPickerScroll = row
	}
	visible := filamentVisibleRows()
	if row >= filamentPickerScroll+visible {
		filamentPickerScroll = row - visible + 1
	}
	clampFilamentScroll()
}

func selectFilamentFromFilteredRow(row int) {
	if row < 0 || row >= len(app.filtered) {
		return
	}
	app.selected = app.filtered[row]
	app.manualBaseFilament = false
	updateFilamentDetails()
	autoSelectProfiles()
	renderAnalysis()
	renderProfiles()
	refreshReady()
	ensureSelectedFilamentVisible()
	if hFilamentDialog != 0 {
		pInvalidateRect.Call(hFilamentDialog, 0, 0)
	}
}

func filamentPickerHitRow(x, y int32) int {
	for i, row := range filamentLayout.rows {
		index := filamentPickerScroll + i
		if index >= len(app.filtered) {
			break
		}
		if contains(row, x, y) {
			return index
		}
	}
	return -1
}

func paintFilamentPicker(hwnd uintptr) {
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
	if !filamentPickerBuffer.ensure(hdc, w, h) {
		drawFilamentPickerScene(hdc, client)
		return
	}
	drawFilamentPickerScene(filamentPickerBuffer.dc, client)
	pBitBlt.Call(hdc, 0, 0, uintptr(w), uintptr(h), filamentPickerBuffer.dc, 0, 0, SRCCOPY)
}

func drawFilamentPickerScene(hdc uintptr, client rect) {
	w, h := width(client), height(client)
	filamentLayout = calculateFilamentLayout(w, h)
	clampFilamentScroll()

	background := brush(rgb(247, 249, 253))
	pFillRect.Call(hdc, uintptr(unsafe.Pointer(&client)), background)
	pDeleteObject.Call(background)
	drawSpatialGlow(hdc, w*18/100, 28, 260, 170, rgb(213, 224, 255), 31)
	drawSpatialGlow(hdc, w*84/100, h-44, 330, 230, rgb(222, 212, 255), 23)
	drawSpatialGlow(hdc, w*56/100, 88, 250, 155, rgb(190, 222, 255), 19)

	text(hdc, tr("filamentEyebrow"), rect{30, 18, w - 30, 40}, hFontSmall, rgb(88, 103, 190), DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	text(hdc, tr("filamentTitle"), rect{30, 41, w - 30, 77}, hFontTitle, rgb(23, 28, 40), DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	text(hdc, tr("filamentSubtitle"), rect{80, 76, w - 80, 100}, hFontSmall, rgb(91, 102, 128), DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)

	drawSpatialSoftShadow(hdc, filamentLayout.search, 22, 10, 5, rgb(62, 75, 122), 18)
	drawSpatialRoundedMaterial(hdc, filamentLayout.search, 22, rgb(255, 255, 255), rgb(247, 249, 253), rgb(224, 230, 242))
	drawSearchIcon(hdc, filamentLayout.search.Left+28, (filamentLayout.search.Top+filamentLayout.search.Bottom)/2, rgb(89, 104, 185))

	drawSpatialSoftShadow(hdc, filamentLayout.list, 24, 13, 7, rgb(62, 75, 122), 23)
	drawSpatialRoundedMaterial(hdc, filamentLayout.list, 24, rgb(255, 255, 255), rgb(244, 247, 252), rgb(225, 231, 242))
	if len(app.filtered) == 0 {
		text(hdc, tr("noFilament"), inset(filamentLayout.list, 24), hFontBody, rgb(91, 102, 128), DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	} else {
		for i, row := range filamentLayout.rows {
			index := filamentPickerScroll + i
			if index >= len(app.filtered) {
				break
			}
			drawFilamentRow(hdc, row, app.filaments[app.filtered[index]], app.filtered[index] == app.selected)
		}
	}
	if filamentPickerScroll > 0 {
		text(hdc, "^", rect{filamentLayout.list.Left, filamentLayout.list.Top + 2, filamentLayout.list.Right, filamentLayout.list.Top + 22}, hFontSmall, rgb(105, 117, 145), DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	}
	if filamentPickerScroll+len(filamentLayout.rows) < len(app.filtered) {
		text(hdc, "v", rect{filamentLayout.list.Left, filamentLayout.list.Bottom - 22, filamentLayout.list.Right, filamentLayout.list.Bottom - 2}, hFontSmall, rgb(105, 117, 145), DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	}

	drawFilamentDetailsCard(hdc, filamentLayout.details)

	drawSpatialRoundedMaterial(hdc, filamentLayout.close, 22, rgb(255, 255, 255), rgb(244, 247, 252), rgb(220, 226, 239))
	text(hdc, tr("close"), inset(filamentLayout.close, 7), hFontButton, rgb(56, 67, 91), DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	drawSpatialSoftShadow(hdc, filamentLayout.apply, 22, 11, 6, rgb(71, 66, 204), 42)
	drawSpatialRoundedMaterial(hdc, filamentLayout.apply, 22, rgb(77, 147, 255), rgb(111, 67, 241), rgb(135, 151, 255))
	drawFilamentButtonShimmer(hdc, filamentLayout.apply)
	text(hdc, tr("useFilament")+"   >", inset(filamentLayout.apply, 8), hFontButton, rgb(255, 255, 255), DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
}

func drawSearchIcon(hdc uintptr, cx, cy int32, color uintptr) {
	circle(hdc, cx-3, cy-3, 8, rgb(255, 255, 255), color)
	line(hdc, cx+4, cy+4, cx+12, cy+12, color, 2)
}

func drawFilamentRow(hdc uintptr, row rect, f shared.Filament, selected bool) {
	stroke := rgb(224, 230, 241)
	top := rgb(255, 255, 255)
	bottom := rgb(247, 249, 253)
	titleColor := rgb(31, 37, 52)
	subColor := rgb(93, 104, 130)
	if selected {
		drawSpatialGlow(hdc, row.Left+50, (row.Top+row.Bottom)/2, 75, 46, materialColor(f.Material), 30)
		drawSpatialSoftShadow(hdc, row, 20, 11, 5, rgb(74, 68, 205), 33)
		stroke = rgb(107, 124, 245)
		top = rgb(250, 252, 255)
		bottom = rgb(239, 244, 255)
		titleColor = rgb(24, 32, 76)
		subColor = rgb(73, 87, 168)
	}
	drawSpatialRoundedMaterial(hdc, row, 20, top, bottom, stroke)
	drawFilamentSpool(hdc, rect{row.Left + 15, row.Top + 13, row.Left + 63, row.Bottom - 13}, materialColor(f.Material))
	title := strings.TrimSpace(f.Brand + " " + f.Product)
	if title == "" {
		title = f.Material
	}
	text(hdc, title, rect{row.Left + 76, row.Top + 10, row.Right - 18, row.Top + 36}, hFontBody, titleColor, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	meta := strings.TrimSpace(f.Material)
	if strings.TrimSpace(f.Variant) != "" {
		meta += " | " + f.Variant
	}
	if f.OfficialProfile {
		meta += " | Flash Studio"
	} else {
		meta += " | " + tr("internal")
	}
	text(hdc, meta, rect{row.Left + 76, row.Top + 37, row.Right - 18, row.Bottom - 9}, hFontSmall, subColor, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
}

func drawFilamentDetailsCard(hdc uintptr, card rect) {
	drawSpatialSoftShadow(hdc, card, 24, 14, 7, rgb(62, 75, 122), 23)
	drawSpatialRoundedMaterial(hdc, card, 24, rgb(255, 255, 255), rgb(244, 247, 252), rgb(225, 231, 242))
	inner := inset(card, 22)
	f, ok := selectedFilament()
	if !ok {
		text(hdc, tr("noFilament"), inner, hFontBody, rgb(91, 102, 128), DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
		return
	}
	drawSpatialGlow(hdc, inner.Right-30, inner.Top+38, 110, 90, materialColor(f.Material), 24)
	drawFilamentSpool(hdc, rect{inner.Left, inner.Top, inner.Left + 86, inner.Top + 86}, materialColor(f.Material))
	title := strings.TrimSpace(f.Brand + " " + f.Product)
	if title == "" {
		title = f.Material
	}
	text(hdc, title, rect{inner.Left + 104, inner.Top + 2, inner.Right, inner.Top + 34}, hFontHeading, rgb(25, 31, 45), DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	text(hdc, strings.TrimSpace(f.Material+"  "+f.Variant), rect{inner.Left + 104, inner.Top + 35, inner.Right, inner.Top + 61}, hFontSmall, rgb(84, 96, 125), DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	source := f.Source
	if f.OfficialProfile {
		source = "Flash Studio"
	}
	text(hdc, source, rect{inner.Left + 104, inner.Top + 61, inner.Right, inner.Top + 86}, hFontSmall, rgb(104, 115, 140), DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)

	gridTop := inner.Top + 108
	colGap := int32(12)
	chipW := (width(inner) - colGap) / 2
	drawFilamentMetric(hdc, rect{inner.Left, gridTop, inner.Left + chipW, gridTop + 58}, tr("nozzleTemp"), fmt.Sprintf("%.0f C", f.NozzleDefault), fmt.Sprintf("%.0f-%.0f C", f.NozzleMin, f.NozzleMax))
	drawFilamentMetric(hdc, rect{inner.Left + chipW + colGap, gridTop, inner.Right, gridTop + 58}, tr("bedTemp"), fmt.Sprintf("%.0f C", f.BedDefault), fmt.Sprintf("%.0f-%.0f C", f.BedMin, f.BedMax))
	drawFilamentMetric(hdc, rect{inner.Left, gridTop + 70, inner.Left + chipW, gridTop + 128}, "MVS", fmt.Sprintf("%.1f mm3/s", f.MaxVolumetricSpeed), "Flow "+fmt.Sprintf("%.3f", f.FlowRatio))
	pa := tr("fromBase")
	if f.PressureAdvance != nil {
		pa = fmt.Sprintf("%.3f", *f.PressureAdvance)
	}
	drawFilamentMetric(hdc, rect{inner.Left + chipW + colGap, gridTop + 70, inner.Right, gridTop + 128}, "PA", pa, f.Confidence)

	calibration := tr("calibrationBaseline")
	if f.MeasuredCalibration {
		calibration = tr("calibrationMeasured")
	}
	drying := "-"
	if f.DryTemperature > 0 && f.DryHours > 0 {
		drying = fmt.Sprintf("%.0f C / %.0f h", f.DryTemperature, f.DryHours)
	}
	infoTop := gridTop + 150
	text(hdc, tr("calibration")+": "+calibration, rect{inner.Left, infoTop, inner.Right, infoTop + 28}, hFontSmall, rgb(80, 92, 122), DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	text(hdc, tr("drying")+": "+drying, rect{inner.Left, infoTop + 30, inner.Right, infoTop + 58}, hFontSmall, rgb(80, 92, 122), DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	if app.filamentMatchTotal > len(app.filtered) {
		text(hdc, trf("shownResults", len(app.filtered), app.filamentMatchTotal), rect{inner.Left, inner.Bottom - 34, inner.Right, inner.Bottom - 8}, hFontSmall, rgb(103, 113, 138), DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	}
}

func drawFilamentMetric(hdc uintptr, r rect, label, value, sub string) {
	drawSpatialRoundedMaterial(hdc, r, 17, rgb(250, 252, 255), rgb(241, 245, 253), rgb(224, 230, 242))
	text(hdc, label, rect{r.Left + 12, r.Top + 6, r.Right - 12, r.Top + 24}, hFontSmall, rgb(99, 109, 134), DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	text(hdc, value, rect{r.Left + 12, r.Top + 24, r.Right - 12, r.Top + 42}, hFontBody, rgb(35, 46, 75), DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	text(hdc, sub, rect{r.Left + 12, r.Top + 41, r.Right - 12, r.Bottom - 4}, hFontSmall, rgb(95, 106, 130), DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
}

func drawFilamentSpool(hdc uintptr, box rect, color uintptr) {
	drawSpatialRoundedMaterial(hdc, box, 16, rgb(244, 247, 255), rgb(228, 235, 252), rgb(207, 217, 242))
	cx, cy := (box.Left+box.Right)/2, (box.Top+box.Bottom)/2
	r := min32(width(box), height(box)) / 3
	circle(hdc, cx, cy, r+6, rgb(250, 252, 255), color)
	circle(hdc, cx, cy, r, color, color)
	circle(hdc, cx, cy, r/3, rgb(250, 252, 255), rgb(210, 219, 242))
	for i := 0; i < 4; i++ {
		a := float64(i)*math.Pi/2 + float64(filamentPickerAnimationTick)*0.015
		x := cx + int32(math.Cos(a)*float64(r*2/3))
		y := cy + int32(math.Sin(a)*float64(r*2/3))
		line(hdc, cx, cy, x, y, rgb(250, 252, 255), 1)
	}
}

func materialColor(material string) uintptr {
	m := strings.ToUpper(material)
	switch {
	case strings.Contains(m, "PETG"):
		return rgb(60, 179, 168)
	case strings.Contains(m, "ABS"):
		return rgb(238, 119, 86)
	case strings.Contains(m, "TPU"):
		return rgb(122, 101, 235)
	case strings.Contains(m, "SILK"):
		return rgb(213, 125, 230)
	case strings.Contains(m, "MATTE"):
		return rgb(95, 127, 215)
	default:
		return rgb(79, 138, 255)
	}
}

func drawFilamentButtonShimmer(hdc uintptr, button rect) {
	saved, _, _ := pSaveDC.Call(hdc)
	region, _, _ := pCreateRoundRectRgn.Call(i32arg(button.Left), i32arg(button.Top), i32arg(button.Right+1), i32arg(button.Bottom+1), 44, 44)
	if region != 0 {
		pSelectClipRgn.Call(hdc, region)
		phase := float64(filamentPickerAnimationTick%210) / 209.0
		x := button.Left - 60 + int32(phase*float64(width(button)+120))
		drawSpatialGlow(hdc, x, (button.Top+button.Bottom)/2, 42, 34, rgb(255, 255, 255), 44)
		pDeleteObject.Call(region)
	}
	if saved != 0 {
		pRestoreDC.Call(hdc, saved)
	}
}
