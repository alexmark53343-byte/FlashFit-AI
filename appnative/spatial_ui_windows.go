//go:build windows

package main

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"flashfitai/shared"
)

const (
	WM_PAINT         = 0x000F
	WM_ERASEBKGND    = 0x0014
	WM_LBUTTONUP     = 0x0202
	WM_GETMINMAXINFO = 0x0024
	WM_SETFOCUS      = 0x0007
	WM_TIMER         = 0x0113
	WM_ENTERSIZEMOVE = 0x0231
	WM_EXITSIZEMOVE  = 0x0232
	SIZE_MINIMIZED   = 1

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
	WM_CTLCOLOREDIT = 0x0133

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
	idPrinterBase      = 6000
	idAdvisorFolder    = 6900
	idAdvisorModelBase = 6901

	// The motion is intentionally restrained. A 30 fps full-window GDI repaint
	// offered no visible benefit for a two-pixel float but could monopolize the
	// UI thread and trigger Windows' "not responding" watchdog.
	// 60 Hz. The timer firing is not the same as repainting: each tick only
	// advances float tracks and asks sceneNeedsAnimationFrame whether anything
	// on screen actually moved. At 10 Hz the hover easing visibly stepped.
	spatialAnimationInterval = 16
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
	toolbar  rect
	aiStatus rect
	aiLight  rect
	aiHeavy  rect
	theme    rect
	language rect
	advanced rect

	sidebar rect
	nav     [5]rect
	promo   rect

	workspace rect
	stage     rect
	tools     [4]rect
	dimension rect
	drop      rect

	inspector rect
	model     rect
	printer   rect
	filament  rect
	quality   rect
	plan      rect
	fast      rect
	balanced  rect
	perfect   rect
	open      rect

	status rect
	stats  rect
}

var (
	spatial                                                                              spatialRegions
	hFontLogo, hFontTitle, hFontHeading, hFontBody, hFontSmall, hFontButton, hFontNumber uintptr
	hFontEyebrow, hFontValue                                                             uintptr
	hAppIcon                                                                             uintptr
	hFilamentDialog, hFilamentSearchLabel, hFilamentApply, hFilamentClose                uintptr
	spatialAnimationTick                                                                 uint32
	stageOnlyFrame                                                                       bool
	sceneSkipStage                                                                       bool
	spatialLayoutValid                                                                   bool
	spatialResizing                                                                      bool
	spatialViewportWidth, spatialViewportHeight                                          int32
	mainSpatialBuffer                                                                    windowBackBuffer
)

var (
	pBeginPaint          = user32.NewProc("BeginPaint")
	pEndPaint            = user32.NewProc("EndPaint")
	pInvalidateRect      = user32.NewProc("InvalidateRect")
	pDrawText            = user32.NewProc("DrawTextW")
	pFillRect            = user32.NewProc("FillRect")
	pGetCursorPos        = user32.NewProc("GetCursorPos")
	pCreatePopupMenu     = user32.NewProc("CreatePopupMenu")
	pAppendMenu          = user32.NewProc("AppendMenuW")
	pTrackPopupMenu      = user32.NewProc("TrackPopupMenu")
	pDestroyMenu         = user32.NewProc("DestroyMenu")
	pSetFocus            = user32.NewProc("SetFocus")
	pDrawIconEx          = user32.NewProc("DrawIconEx")
	pSetTimer            = user32.NewProc("SetTimer")
	pKillTimer           = user32.NewProc("KillTimer")
	pGetForegroundWindow = user32.NewProc("GetForegroundWindow")

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
	pSetBkColor             = gdi32.NewProc("SetBkColor")
	pSetTextColor           = gdi32.NewProc("SetTextColor")
	pCreateFont             = gdi32.NewProc("CreateFontW")
	pCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	pBitBlt                 = gdi32.NewProc("BitBlt")
	pSaveDC                 = gdi32.NewProc("SaveDC")
	pRestoreDC              = gdi32.NewProc("RestoreDC")
	pCreateRoundRectRgn     = gdi32.NewProc("CreateRoundRectRgn")
	pExcludeClipRect        = gdi32.NewProc("ExcludeClipRect")
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
	animClock.start()
	loadUISettings()
	applyThemePalette()
	hFontLogo = createUIFont(19, FW_SEMIBOLD)
	hFontTitle = createUIFont(23, FW_SEMIBOLD)
	hFontHeading = createUIFont(16, FW_MEDIUM)
	hFontBody = createUIFont(15, FW_NORMAL)
	hFontSmall = createUIFont(12, FW_NORMAL)
	hFontButton = createUIFont(14, FW_MEDIUM)
	hFontNumber = createUIFont(15, FW_SEMIBOLD)
	hFontEyebrow = createUIFont(11, FW_SEMIBOLD)
	hFontValue = createUIFont(18, FW_SEMIBOLD)
	initSpatialBenchyAsset()
	hFont = hFontBody
	hAppIcon, _, _ = pLoadIcon.Call(appInstance, 1)
	setSpatialTitle()
	applyWindowChrome(hwnd)
	startSpatialAnimation(hwnd)
}

func cleanupSpatialUI() {
	stopSpatialAnimation(mainHwnd)
	mainSpatialBuffer.reset()
	cleanupSpatialBenchyAsset()
	cleanupSpatialMaterialSystem()
	releaseCompositeDC()
	releaseSolidPens()
	clearBackdropCache()
	clearTexturePreviewCache()
	cleanupStage3D()
	for _, h := range []uintptr{hFontLogo, hFontTitle, hFontHeading, hFontBody, hFontSmall, hFontButton, hFontNumber, hFontEyebrow, hFontValue} {
		if h != 0 {
			pDeleteObject.Call(h)
		}
	}
}

func startSpatialAnimation(hwnd uintptr) {
	if hwnd != 0 {
		pSetTimer.Call(hwnd, idSpatialAnimation, spatialAnimationInterval, 0)
	}
}

func stopSpatialAnimation(hwnd uintptr) {
	if hwnd != 0 {
		pKillTimer.Call(hwnd, idSpatialAnimation)
	}
}

func spatialAnimationActive(hwnd uintptr) bool {
	if hwnd == 0 || spatialResizing || hTexturePicker != 0 {
		return false
	}
	iconic, _, _ := pIsIconic.Call(hwnd)
	if iconic != 0 {
		return false
	}
	foreground, _, _ := pGetForegroundWindow.Call()
	return foreground == hwnd
}

// The only things that animate on their own are the shimmer on an armed call to
// action and the engine dot while work is in flight. Everything else is static
// until the pointer moves, so an idle window costs nothing.
func sceneNeedsAnimationFrame() bool {
	if app.discovering || app.importing || app.analyzing {
		return true
	}
	// The empty envelope drifts and its gradient flows, so it animates too —
	// it is only line work, unlike a full mesh re-render.
	if !stageIsShowingUserModel() {
		return true
	}
	// The shimmer sits in the inspector, not the stage, so it needs a full
	// frame; it is left to the hover path rather than driving one per tick.
	// Nothing continuous is on screen: keep painting only until the hover
	// transitions have finished, then let the window go quiet.
	return !hoverSettled()
}

func setSpatialTitle() {
	setText(mainHwnd, "FlashFit AI")
}

// Continuous effects live inside the stage, so a frame of the cube turning must
// not cost a redraw of the sidebar, the inspector and every frosted panel.
// invalidateStageOnly repaints just that rectangle; the rest of the window is
// left exactly as it was in the back buffer.
//
// Frame-time budget for a stage-only frame: < 8 ms on the reference machine.
func invalidateStageOnly() {
	if mainHwnd == 0 || width(spatial.stage) <= 0 {
		return
	}
	stageOnlyFrame = true
	area := spatial.stage
	pInvalidateRect.Call(mainHwnd, uintptr(unsafe.Pointer(&area)), 0)
}

// invalidateChrome repaints everything except the canvas, for changes that
// cannot touch it: hover, focus, a toolbar state. The canvas keeps whatever it
// last rendered, which is still correct.
func invalidateChrome() {
	if mainHwnd == 0 || !spatialLayoutValid {
		invalidateSpatial()
		return
	}
	stageOnlyFrame = false
	sceneSkipStage = true
	pInvalidateRect.Call(mainHwnd, 0, 0)
}

func invalidateSpatial() {
	stageOnlyFrame = false
	sceneSkipStage = false
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

func min32(a, b int32) int32 {
	if a < b {
		return a
	}
	return b
}

func clamp32(value, low, high int32) int32 {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

// Four floating planes over a lit background: navigation rail, workspace,
// configuration inspector and a status bar. Every plane keeps a margin, so the
// background light reads between them instead of behind a single flat sheet.
func calculateSpatialLayout(w, h int32) spatialRegions {
	margin := clamp32(w/72, 12, 22)
	gap := clamp32(w/96, 10, 18)
	toolbarH := int32(78)
	statusH := clamp32(h/12, 62, 82)

	sidebarW := clamp32(w*17/100, 168, 240)
	inspectorW := clamp32(w*29/100, 280, 430)
	if sidebarW+inspectorW > w-2*margin-2*gap-300 {
		sidebarW = clamp32(w*15/100, 130, 240)
		inspectorW = clamp32(w*26/100, 220, 430)
	}

	contentTop := toolbarH
	contentBottom := h - margin - statusH - gap
	if contentBottom-contentTop < 240 {
		contentBottom = contentTop + 240
	}

	regions := spatialRegions{
		toolbar: rect{0, 0, w, toolbarH},
		sidebar: rect{margin, contentTop, margin + sidebarW, contentBottom},
		status:  rect{margin, contentBottom + gap, w - margin, h - margin},
	}
	regions.inspector = rect{w - margin - inspectorW, contentTop, w - margin, contentBottom}
	regions.workspace = rect{regions.sidebar.Right + gap, contentTop, regions.inspector.Left - gap, contentBottom}

	// Toolbar controls run from the right edge inward.
	right := w - margin - 6
	regions.advanced = rect{right - 100, 22, right, 60}
	regions.language = rect{regions.advanced.Left - 8 - 56, 22, regions.advanced.Left - 8, 60}
	regions.theme = rect{regions.language.Left - 8 - 40, 22, regions.language.Left - 8, 60}
	// Two dedicated buttons rather than a system popup: which model is loaded is
	// a standing choice, so it is shown as a standing control with the active
	// one lit, instead of hiding the state behind a menu that has to be opened
	// to be read.
	aiRight := regions.theme.Left - 10
	regions.aiHeavy = rect{aiRight - 92, 22, aiRight, 60}
	regions.aiLight = rect{regions.aiHeavy.Left - 6 - 92, 22, regions.aiHeavy.Left - 6, 60}
	regions.aiStatus = rect{regions.aiLight.Left - 10 - 132, 22, regions.aiLight.Left - 10, 60}

	// Navigation rail: five destinations, promo card pinned to the bottom.
	navTop := regions.sidebar.Top + 14
	navH := int32(52)
	for i := 0; i < 5; i++ {
		y := navTop + int32(i)*(navH+4)
		regions.nav[i] = rect{regions.sidebar.Left + 10, y, regions.sidebar.Right - 10, y + navH}
	}
	promoH := clamp32(height(regions.sidebar)/4, 96, 150)
	regions.promo = rect{regions.sidebar.Left + 10, regions.sidebar.Bottom - 12 - promoH, regions.sidebar.Right - 10, regions.sidebar.Bottom - 12}
	if regions.promo.Top < regions.nav[4].Bottom+12 {
		regions.promo = rect{0, 0, 0, 0}
	}

	// Workspace: canvas with a floating tool strip, dimension pill and drop zone.
	dropH := int32(78)
	regions.drop = rect{regions.workspace.Left + 26, regions.workspace.Bottom - 18 - dropH, regions.workspace.Right - 26, regions.workspace.Bottom - 18}
	regions.dimension = rect{
		(regions.workspace.Left+regions.workspace.Right)/2 - 105, regions.drop.Top - 46,
		(regions.workspace.Left+regions.workspace.Right)/2 + 105, regions.drop.Top - 14,
	}
	regions.stage = rect{regions.workspace.Left + 8, regions.workspace.Top + 62, regions.workspace.Right - 8, regions.dimension.Top - 6}

	toolTop := regions.stage.Top + height(regions.stage)/2 - 68
	for i := 0; i < 4; i++ {
		y := toolTop + int32(i)*34
		regions.tools[i] = rect{regions.workspace.Left + 18, y, regions.workspace.Left + 52, y + 32}
	}

	// Inspector: three option cards, segmented quality, plan, gradient CTA.
	padding := int32(18)
	left := regions.inspector.Left + padding
	rightEdge := regions.inspector.Right - padding
	ctaH := int32(52)
	regions.open = rect{left, regions.inspector.Bottom - padding - ctaH, rightEdge, regions.inspector.Bottom - padding}

	rowH := clamp32(height(regions.inspector)/8, 66, 84)
	y := regions.inspector.Top + 46
	regions.model = rect{left, y, rightEdge, y + rowH}
	y = regions.model.Bottom + 10
	regions.printer = rect{left, y, rightEdge, y + rowH}
	y = regions.printer.Bottom + 10
	regions.filament = rect{left, y, rightEdge, y + rowH}
	y = regions.filament.Bottom + 20

	track := rect{left, y + 24, rightEdge, y + 24 + 40}
	regions.quality = rect{left, y, rightEdge, track.Bottom}
	segW := width(track) / 3
	regions.fast = rect{track.Left, track.Top, track.Left + segW, track.Bottom}
	regions.balanced = rect{regions.fast.Right, track.Top, regions.fast.Right + segW, track.Bottom}
	regions.perfect = rect{regions.balanced.Right, track.Top, track.Right, track.Bottom}

	regions.plan = rect{left, regions.quality.Bottom + 18, rightEdge, regions.open.Top - 16}
	regions.stats = rect{regions.status.Right - 400, regions.status.Top, regions.status.Right - 20, regions.status.Bottom}
	return regions
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

// Pens are cached rather than created per stroke. The neon envelope alone draws
// several hundred short segments a frame, and creating plus destroying a GDI
// pen for each one cost more than the drawing did.
type penKey struct {
	color uintptr
	width int
}

var solidPenCache = map[penKey]uintptr{}

const solidPenCacheLimit = 512

func cachedPen(color uintptr, lineWidth int) uintptr {
	key := penKey{color: color, width: lineWidth}
	if handle, ok := solidPenCache[key]; ok {
		return handle
	}
	if len(solidPenCache) >= solidPenCacheLimit {
		releaseSolidPens()
	}
	handle := pen(PS_SOLID, lineWidth, color)
	solidPenCache[key] = handle
	return handle
}

// Safe to call only when no cached pen is selected into a DC, which is true
// between paints.
func releaseSolidPens() {
	for _, handle := range solidPenCache {
		if handle != 0 {
			pDeleteObject.Call(handle)
		}
	}
	solidPenCache = map[penKey]uintptr{}
}

func line(hdc uintptr, x1, y1, x2, y2 int32, color uintptr, lineWidth int) {
	p := cachedPen(color, lineWidth)
	if p == 0 {
		return
	}
	old, _, _ := pSelectObject.Call(hdc, p)
	pMoveToEx.Call(hdc, i32arg(x1), i32arg(y1), 0)
	pLineTo.Call(hdc, i32arg(x2), i32arg(y2))
	pSelectObject.Call(hdc, old)
}

func circle(hdc uintptr, centerX, centerY, radius int32, fill, stroke uintptr) {
	b := brush(fill)
	p := pen(PS_SOLID, 1, stroke)
	withObjects(hdc, b, p, func() {
		pEllipse.Call(hdc, i32arg(centerX-radius), i32arg(centerY-radius), i32arg(centerX+radius), i32arg(centerY+radius))
	})
}

// Small caps-style section label used at the top of every plane.
func eyebrow(hdc uintptr, value string, r rect, flags uintptr) {
	text(hdc, strings.ToUpper(value), r, hFontEyebrow, th.textMuted, flags|DT_SINGLELINE|DT_END_ELLIPSIS)
}

func drawPrinterIcon(hdc uintptr, cx, cy int32, color uintptr) {
	line(hdc, cx-8, cy-7, cx+8, cy-7, color, 2)
	line(hdc, cx-8, cy-7, cx-8, cy-1, color, 2)
	line(hdc, cx+8, cy-7, cx+8, cy-1, color, 2)
	rounded(hdc, rect{cx - 12, cy - 2, cx + 12, cy + 9}, 4, th.sunken, color)
	rounded(hdc, rect{cx - 8, cy + 4, cx + 8, cy + 12}, 3, th.surface, color)
	circle(hdc, cx+7, cy+2, 1, color, color)
}

func drawSpoolIcon(hdc uintptr, cx, cy int32, color uintptr) {
	circle(hdc, cx, cy, 10, th.sunken, color)
	circle(hdc, cx, cy, 4, th.surface, color)
	line(hdc, cx-10, cy+11, cx+10, cy+11, color, 2)
}

func drawSlidersIcon(hdc uintptr, cx, cy int32, color uintptr) {
	for i, off := range []int32{-7, 0, 7} {
		line(hdc, cx-10, cy+off, cx+10, cy+off, color, 2)
		knob := []int32{3, -4, 6}[i]
		circle(hdc, cx+knob, cy+off, 2, th.surface, color)
	}
}

func drawRocketIcon(hdc uintptr, cx, cy int32, color uintptr) {
	pts := []point{{cx, cy - 11}, {cx + 7, cy + 2}, {cx, cy + 8}, {cx - 7, cy + 2}}
	b, p := brush(color), pen(PS_SOLID, 1, color)
	withObjects(hdc, b, p, func() { pPolygon.Call(hdc, uintptr(unsafe.Pointer(&pts[0])), uintptr(len(pts))) })
	circle(hdc, cx, cy-3, 3, th.surface, color)
	line(hdc, cx-4, cy+9, cx+4, cy+9, color, 2)
}

func drawStepIcon(hdc uintptr, cx, cy int32, index int, color uintptr) {
	switch index {
	case 0:
		line(hdc, cx-10, cy-6, cx-2, cy-6, color, 2)
		line(hdc, cx-2, cy-6, cx+1, cy-3, color, 2)
		line(hdc, cx+1, cy-3, cx+10, cy-3, color, 2)
		line(hdc, cx-10, cy-6, cx-10, cy+8, color, 2)
		line(hdc, cx-10, cy+8, cx+10, cy+8, color, 2)
		line(hdc, cx+10, cy+8, cx+10, cy-3, color, 2)
	case 1:
		drawSpoolIcon(hdc, cx, cy-1, color)
	case 2:
		drawSlidersIcon(hdc, cx, cy, color)
	default:
		drawRocketIcon(hdc, cx, cy, color)
	}
}

func drawMoonIcon(hdc uintptr, cx, cy int32, color uintptr) {
	circle(hdc, cx, cy, 8, color, color)
	circle(hdc, cx+4, cy-4, 7, th.surface, th.surface)
}

func drawSunIcon(hdc uintptr, cx, cy int32, color uintptr) {
	circle(hdc, cx, cy, 5, color, color)
	for i := 0; i < 8; i++ {
		a := float64(i) * math.Pi / 4
		x1 := cx + int32(math.Cos(a)*8)
		y1 := cy + int32(math.Sin(a)*8)
		x2 := cx + int32(math.Cos(a)*11)
		y2 := cy + int32(math.Sin(a)*11)
		line(hdc, x1, y1, x2, y2, color, 2)
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

func drawChevron(hdc uintptr, cx, cy int32, color uintptr) {
	line(hdc, cx-3, cy-4, cx+2, cy, color, 2)
	line(hdc, cx+2, cy, cx-3, cy+4, color, 2)
}

func drawHeader(hdc uintptr, w int32) {
	left := spatial.sidebar.Left
	tile := rect{left, 20, left + 42, 62}
	shade(hdc, tile, 13, 10, 5, 40)
	accentFill(hdc, tile, 13)
	if hAppIcon != 0 {
		pDrawIconEx.Call(hdc, uintptr(uint32(tile.Left+7)), uintptr(uint32(tile.Top+7)), hAppIcon, 28, 28, 0, 0, 3)
	} else {
		text(hdc, "F", tile, hFontLogo, th.textOnAccent, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	}
	text(hdc, "FlashFit AI", rect{tile.Right + 14, 18, tile.Right + 260, 44}, hFontLogo, th.textPrimary, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	text(hdc, "v"+shortVersion(), rect{tile.Right + 15, 42, tile.Right + 260, 62}, hFontSmall, th.textMuted, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	drawAIStatus(hdc)

	themeButton := drawHeaderButton(hdc, spatial.theme, hoverTheme)
	iconColor := mixColor(th.textSecondary, th.accent, hoverOf(hoverTheme))
	if th.dark {
		drawSunIcon(hdc, (themeButton.Left+themeButton.Right)/2, (themeButton.Top+themeButton.Bottom)/2, iconColor)
	} else {
		drawMoonIcon(hdc, (themeButton.Left+themeButton.Right)/2, (themeButton.Top+themeButton.Bottom)/2, iconColor)
	}

	languageButton := drawHeaderButton(hdc, spatial.language, hoverLanguage)
	text(hdc, strings.ToUpper(uiLanguage), rect{languageButton.Left, languageButton.Top, languageButton.Right - 14, languageButton.Bottom}, hFontButton, th.textPrimary, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	drawChevronDown(hdc, languageButton.Right-16, (languageButton.Top+languageButton.Bottom)/2, th.textMuted)

	advancedButton := drawHeaderButton(hdc, spatial.advanced, hoverAdvanced)
	text(hdc, tr("advanced"), inset(advancedButton, 8), hFontButton, th.textPrimary, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	_ = w
}

// A standing indicator for the model: what state it is in, at a glance, without
// having to open anything. Four states, each with its own colour and a dot that
// only animates while the model is actually working.
func drawAIStatus(hdc uintptr) {
	r := spatial.aiStatus
	if width(r) <= 0 {
		return
	}
	ready, starting, failure := advisorServer.status()
	thinking := ready && shared.AdvisorIsThinking()

	var dot uintptr
	var label string
	switch {
	case thinking:
		dot, label = th.accent, tr("aiThinking")
	case ready:
		dot, label = th.okColor, tr("aiOnline")
	case starting:
		dot, label = th.warnColor, tr("aiLoading")
		// The server reports how far it has mapped the weights; show it when
		// it does, rather than a spinner that says nothing.
		if percent := advisorLoadingProgress(); percent >= 0 {
			label = fmt.Sprintf("%s %.0f%%", tr("aiLoading"), percent)
		}
	case failure != "":
		dot, label = th.textMuted, tr("aiOffline")
	default:
		dot, label = th.textMuted, tr("aiOffline")
	}

	shade(hdc, r, height(r)/2, 8, 3, 18)
	drawSpatialRoundedMaterial(hdc, r, height(r)/2, th.surface, th.surfaceAlt, th.stroke)

	cx := r.Left + 20
	cy := (r.Top + r.Bottom) / 2
	if thinking || starting {
		// Breathing halo: the only part that moves, so a still dot always means
		// "not working".
		glow(hdc, cx, cy, 16, 16, dot, uint8(30+animPulse(1.2)*70))
	}
	circle(hdc, cx, cy, 5, dot, dot)
	text(hdc, label, rect{cx + 12, r.Top, r.Right - 12, r.Bottom}, hFontSmall, th.textSecondary, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)

	drawAISelector(hdc)
}

// The two model buttons, and the download that fills in behind them.
//
// A model that is not on disk yet is still offered: choosing it starts the
// fetch. Hiding the option until the file happens to exist would leave the user
// with no way to ever get it.
func drawAISelector(hdc uintptr) {
	if download := advisorDownloadStatus(); download.Active {
		drawAIDownloadBar(hdc, download)
		return
	}
	// A model that still has to be fetched says so, so a click on it is never a
	// surprise gigabyte.
	light, strong := tr("aiLightShort"), tr("aiHeavyShort")
	if !advisorModelPresent("light") {
		light += " ↓"
	}
	if !advisorModelPresent("strong") {
		strong += " ↓"
	}
	drawAIChoice(hdc, spatial.aiLight, light, advisorUsingModel("light"), true, hoverAILight)
	drawAIChoice(hdc, spatial.aiHeavy, strong, advisorUsingModel("strong"), true, hoverAIHeavy)
}

// advisorUsingModel reports whether a catalogue entry is the one loaded. The
// file is matched wherever it actually is, since weights fetched by hand carry
// the name their publisher gave them.
func advisorUsingModel(id string) bool {
	entry, ok := advisorCatalogEntryByID(id)
	if !ok {
		return false
	}
	if strings.EqualFold(advisorSelectedModel, advisorModelFile(entry)) {
		return true
	}
	existing, present := findExistingModel(entry)
	return present && strings.EqualFold(advisorSelectedModel, existing)
}

// The progress bar replaces both buttons while a model is coming down. A
// transfer of this size is worth the whole width: a thin sliver moving for ten
// minutes reads as a stall.
func drawAIDownloadBar(hdc uintptr, state advisorDownloadState) {
	r := rect{spatial.aiLight.Left, spatial.aiLight.Top, spatial.aiHeavy.Right, spatial.aiHeavy.Bottom}
	radius := height(r) / 2
	drawSpatialRoundedMaterial(hdc, r, radius, th.sunken, th.sunkenAlt, th.stroke)

	// The filled portion is clipped to the same rounded shape, so it does not
	// square off the ends of the track as it grows.
	filled := int32(float64(width(r)) * spatialClamp(state.Fraction, 0, 1))
	if filled > 2 {
		saved, _, _ := pSaveDC.Call(hdc)
		region, _, _ := pCreateRoundRectRgn.Call(i32arg(r.Left), i32arg(r.Top), i32arg(r.Right+1), i32arg(r.Bottom+1), uintptr(uint32(radius*2)), uintptr(uint32(radius*2)))
		if region != 0 {
			pSelectClipRgn.Call(hdc, region)
		}
		accentFill(hdc, rect{r.Left, r.Top, r.Left + filled, r.Bottom}, radius)
		if region != 0 {
			pDeleteObject.Call(region)
		}
		if saved != 0 {
			pRestoreDC.Call(hdc, saved)
		}
	}

	label := fmt.Sprintf("%s  %.0f%%", state.Label, state.Fraction*100)
	if state.Total > 0 {
		label = fmt.Sprintf("%s  %.0f%%  ·  %.1f/%.1f GB",
			state.Label, state.Fraction*100,
			float64(state.Received)/(1<<30), float64(state.Total)/(1<<30))
	}
	text(hdc, label, inset(r, 8), hFontSmall, th.textPrimary, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
}

func drawAIChoice(hdc uintptr, r rect, label string, active, available bool, id int) {
	if width(r) <= 0 {
		return
	}
	radius := height(r) / 2
	switch {
	case active:
		shade(hdc, r, radius, 9, 4, 34)
		accentFill(hdc, r, radius)
		text(hdc, label, inset(r, 6), hFontSmall, th.textOnAccent, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
		return
	case !available:
		// Shown but plainly inert, so the option is discoverable without
		// pretending it can be used.
		drawSpatialRoundedMaterial(hdc, r, radius, th.sunken, th.sunkenAlt, th.strokeSoft)
		text(hdc, label, inset(r, 6), hFontSmall, th.textMuted, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
		return
	}
	amount := hoverOf(id)
	drawSpatialRoundedMaterial(hdc, r, radius,
		mixColor(th.surface, th.accentTintA, amount),
		mixColor(th.surfaceAlt, th.accentTintB, amount),
		mixColor(th.stroke, th.accentStroke, amount))
	text(hdc, label, inset(r, 6), hFontSmall, th.textSecondary, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
}

// The build string carries its channel for logs and crash reports; the chrome
// only needs the number.
func shortVersion() string {
	if index := strings.IndexAny(buildVersion, "-+"); index > 0 {
		return buildVersion[:index]
	}
	return buildVersion
}

func drawChevronDown(hdc uintptr, cx, cy int32, color uintptr) {
	line(hdc, cx-4, cy-2, cx, cy+2, color, 2)
	line(hdc, cx, cy+2, cx+4, cy-2, color, 2)
}

// Toolbar buttons are standing chips: rounded to their own height so a square
// one reads as a circle, lifting slightly under the pointer.
func drawHeaderButton(hdc uintptr, r rect, id int) rect {
	r = lift(r, id)
	radius := height(r) / 2
	shade(hdc, r, radius, 9, 4, hoverShadow(id, 22))
	amount := hoverOf(id)
	drawSpatialRoundedMaterial(hdc, r, radius,
		mixColor(th.surface, th.accentTintA, amount),
		mixColor(th.surfaceAlt, th.accentTintB, amount),
		mixColor(th.stroke, th.accentStroke, amount))
	return r
}

// The model canvas, recessed inside the workspace card.
func drawStage(hdc uintptr) {
	r := spatial.stage
	drawSpatialRoundedMaterial(hdc, r, 16, th.stageTop, th.stageBottom, th.stageStroke)

	saved, _, _ := pSaveDC.Call(hdc)
	region, _, _ := pCreateRoundRectRgn.Call(i32arg(r.Left), i32arg(r.Top), i32arg(r.Right+1), i32arg(r.Bottom+1), 32, 32)
	if region != 0 {
		pSelectClipRgn.Call(hdc, region)
	}

	// Colour pooled into the canvas. Without these the stage is a flat void, and
	// they are what gives the depth its atmosphere.
	w, h := width(r), height(r)
	glow(hdc, r.Left+w*30/100, r.Top+h*30/100, w*58/100, h*66/100, th.glowCool, 92)
	glow(hdc, r.Left+w*78/100, r.Top+h*22/100, w*46/100, h*52/100, th.glowWarm, 78)
	glow(hdc, r.Left+w*58/100, r.Bottom-h*8/100, w*52/100, h*40/100, th.glowMint, 54)
	glow(hdc, (r.Left+r.Right)/2, r.Top+h*46/100, w*30/100, h*34/100, th.glowCool, 60)

	inner := inset(r, 8)
	if stageIsShowingUserModel() {
		// The empty state draws its own plate grid, so the horizon floor is only
		// context for a loaded part.
		drawPerspectiveFloor(hdc, rect{inner.Left, inner.Top + height(inner)*42/100, inner.Right, inner.Bottom})
	}

	modelCX := (r.Left + r.Right) / 2
	viewport := rect{inner.Left, inner.Top + 14, inner.Right, spatial.drop.Top - 14}
	if stageIsShowingUserModel() {
		// Contact shadow: tight and dark under the part, widening outward.
		groundY := viewport.Bottom - height(viewport)*16/100
		glow(hdc, modelCX, groundY, width(r)*15/100, height(r)*5/100, th.shadow, 62)
		glow(hdc, modelCX, groundY, width(r)*26/100, height(r)*9/100, th.shadow, 26)
		drawStageModel(hdc, viewport)
	} else {
		drawStagePlate(hdc, viewport)
	}

	drawStageVignette(hdc, r)

	if region != 0 {
		pDeleteObject.Call(region)
	}
	if saved != 0 {
		pRestoreDC.Call(hdc, saved)
	}

	text(hdc, tr("viewerHint"), rect{r.Left + 18, r.Bottom - 26, r.Right - 18, r.Bottom - 8}, hFontSmall, th.textMuted, DT_RIGHT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
}

// Darkened corners pull the eye to the centre of the plate.
func drawStageVignette(hdc uintptr, r rect) {
	corner := min32(width(r), height(r)) * 55 / 100
	strength := uint8(26)
	if th.dark {
		strength = 46
	}
	for _, c := range [4][2]int32{
		{r.Left, r.Top}, {r.Right, r.Top},
		{r.Left, r.Bottom}, {r.Right, r.Bottom},
	} {
		glow(hdc, c[0], c[1], corner, corner, th.shadow, strength)
	}
}

func drawPerspectiveFloor(hdc uintptr, r rect) {
	horizon := r.Top
	centerX := (r.Left + r.Right) / 2
	for i := int32(0); i <= 9; i++ {
		t := float64(i) / 9
		y := horizon + int32(float64(r.Bottom-horizon)*math.Pow(t, 1.75))
		color := th.stageGrid
		if i%3 == 0 {
			color = th.stageGridHi
		}
		line(hdc, r.Left, y, r.Right, y, color, 1)
	}
	for i := int32(-8); i <= 8; i++ {
		topX := centerX + i*width(r)/40
		bottomX := centerX + i*width(r)/11
		color := th.stageGrid
		if i == 0 {
			color = th.stageGridHi
		}
		line(hdc, topX, horizon, bottomX, r.Bottom, color, 1)
	}
}

func drawDropPill(hdc uintptr, r rect) {
	shade(hdc, r, 26, 14, 7, 28)
	card(hdc, r, 26)
	if app.modelPath == "" {
		drawDashedFrame(hdc, inset(r, 6))
		drawUploadIcon(hdc, r.Left+34, (r.Top+r.Bottom)/2, th.accent)
		text(hdc, tr("dropTitle"), rect{r.Left + 58, r.Top + 9, r.Right - 18, r.Top + 33}, hFontBody, th.textPrimary, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
		text(hdc, tr("dropSubtitle"), rect{r.Left + 58, r.Top + 30, r.Right - 18, r.Bottom - 8}, hFontSmall, th.textMuted, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
		return
	}
	name := filepath.Base(app.modelPath)
	subtitle := tr("analyzing")
	if app.analysis != nil {
		a := *app.analysis
		subtitle = trf("analysisSummary", a.InputFormat, a.TriangleCount, a.Extents[0], a.Extents[1], a.Extents[2])
	}
	text(hdc, name, rect{r.Left + 20, r.Top + 9, r.Right - 20, r.Top + 33}, hFontBody, th.textPrimary, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	text(hdc, subtitle, rect{r.Left + 16, r.Top + 30, r.Right - 16, r.Bottom - 8}, hFontSmall, th.textMuted, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
}

func drawDashedFrame(hdc uintptr, r rect) {
	dash := pen(PS_DASH, 1, th.accentStroke)
	nullBrush, _, _ := pGetStockObject.Call(NULL_BRUSH)
	oldPen, _, _ := pSelectObject.Call(hdc, dash)
	oldBrush, _, _ := pSelectObject.Call(hdc, nullBrush)
	pRoundRect.Call(hdc, i32arg(r.Left), i32arg(r.Top), i32arg(r.Right), i32arg(r.Bottom), 34, 34)
	pSelectObject.Call(hdc, oldBrush)
	pSelectObject.Call(hdc, oldPen)
	pDeleteObject.Call(dash)
}


// The inspector: one flush panel, hairline-separated rows, no nested cards.
func drawInspector(hdc uintptr) {
	panel := spatial.inspector
	background := brush(th.surface)
	pFillRect.Call(hdc, uintptr(unsafe.Pointer(&panel)), background)
	pDeleteObject.Call(background)
	line(hdc, panel.Left, panel.Top, panel.Left, panel.Bottom, th.strokeSoft, 1)
	eyebrow(hdc, tr("inspectorTitle"), rect{panel.Left + 16, panel.Top + 14, panel.Right - 16, panel.Top + 36}, DT_LEFT|DT_VCENTER)

	drawModelRow(hdc, spatial.model)
	drawPrinterSlot(hdc, spatial.printer)
	drawFilamentSlot(hdc, spatial.filament)
	drawQualitySlot(hdc, spatial.quality)
	drawPlanCard(hdc, spatial.plan)
	drawPrimaryAction(hdc, spatial.open)
}

func drawModelRow(hdc uintptr, r rect) {
	value := tr("chooseModel")
	if app.modelPath != "" {
		value = filepath.Base(app.modelPath)
	}
	meta := ""
	if app.analysis != nil {
		meta = fmt.Sprintf("%s · %d ▲", app.analysis.InputFormat, app.analysis.TriangleCount)
	}
	drawSelectSlot(hdc, r, tr("stepModel"), value, meta, drawFileIcon, app.modelPath != "", hoverModel)
}

func drawFileIcon(hdc uintptr, cx, cy int32, color uintptr) {
	line(hdc, cx-7, cy-9, cx+3, cy-9, color, 2)
	line(hdc, cx-7, cy-9, cx-7, cy+9, color, 2)
	line(hdc, cx-7, cy+9, cx+7, cy+9, color, 2)
	line(hdc, cx+7, cy+9, cx+7, cy-5, color, 2)
	line(hdc, cx+3, cy-9, cx+7, cy-5, color, 2)
}

type modelCheck struct {
	label  string
	detail string
	level  int // 0 pass, 1 advisory, 2 blocking
}

// Concrete, actionable findings about the loaded mesh. "Something is wrong" is
// not useful on its own; each row names the property and what it implies.
func modelChecks() []modelCheck {
	// The three layers come first, and they are reported whether or not a model
	// is loaded.
	//
	// They used to be appended last, after the mesh checks, and the whole list
	// was skipped entirely until a model had been analysed. Both were wrong for
	// the same reason: the row that answers "is this thing running" was the
	// first to be dropped when the panel ran short of height, and did not exist
	// at all in the state where the user is most likely to be asking. The mesh
	// checks describe a file and can wait; these describe the application.
	checks := make([]modelCheck, 0, 8)
	if check, ok := advisorCheck(); ok {
		checks = append(checks, check)
	}
	checks = append(checks, protectionCheck())
	checks = append(checks, guardrailCheck())
	checks = append(checks, sogCheck())

	if app.analysis == nil {
		return checks
	}
	a := *app.analysis

	// Mesh defects are advisory: the slicer repairs them on load, and blocking
	// on them made ordinary downloaded models unusable. Only a mesh broken in
	// bulk is fatal, and that is refused before reaching here.
	mesh := modelCheck{label: tr("checkMesh"), detail: tr("checkPass")}
	switch {
	case a.DegenerateFaces > 0:
		mesh.level, mesh.detail = 1, trf("checkMeshDegenerate", a.DegenerateFaces)
	case !a.Watertight:
		mesh.level, mesh.detail = 1, tr("checkMeshOpen")
	}
	checks = append(checks, mesh)

	volume := modelCheck{label: tr("checkVolume"), detail: tr("checkPass")}
	if printer, ok := selectedPrinter(); ok && printer.BuildVolume[0] > 0 {
		for axis := 0; axis < 3; axis++ {
			if a.Extents[axis] > printer.BuildVolume[axis] {
				volume.level = 2
				volume.detail = trf("checkVolumeOver", a.Extents[axis]-printer.BuildVolume[axis])
				break
			}
		}
	} else {
		volume.level, volume.detail = 1, tr("checkVolumeUnknown")
	}
	checks = append(checks, volume)

	overhang := modelCheck{label: tr("checkOverhang"), detail: tr("supportsOff")}
	if a.SupportSuggested {
		overhang.level, overhang.detail = 1, tr("supportsOn")
	}
	checks = append(checks, overhang)

	adhesion := modelCheck{label: tr("checkAdhesion"), detail: tr("checkPass")}
	if a.BrimSuggested || a.ThinOrTall {
		adhesion.level, adhesion.detail = 1, tr("checkAdhesionBrim")
	}
	checks = append(checks, adhesion)

	// The defects predicted for this particular profile, after the rows that
	// describe the machinery itself.
	checks = append(checks, printReadinessChecks()...)
	return checks
}

// printReadinessChecks turns the predicted defects into rows. When nothing is
// predicted it says so, because "no warnings" and "not checked yet" look the
// same otherwise.
// sogCheck reports what S.O.G did, and is always present.
//
// Drawing the row only when something had been corrected made "it ran and found
// nothing" indistinguishable from "it never ran" — and the second is the one
// state it is never in, because it is part of the application rather than
// something that switches on.
func sogCheck() modelCheck {
	if app.recommendation == nil {
		return modelCheck{label: tr("checkSOG"), detail: tr("checkSOGWaiting"), level: 0}
	}
	verdict := shared.LastSOGVerdict
	switch {
	case !verdict.Cleared:
		return modelCheck{label: tr("checkSOG"), detail: tr("checkSOGHeld"), level: 2}
	case len(verdict.Repairs) > 0:
		return modelCheck{label: tr("checkSOG"), detail: trf("checkSOGRepaired", len(verdict.Repairs)), level: 0}
	}
	return modelCheck{label: tr("checkSOG"), detail: tr("checkSOGClean"), level: 0}
}

func printReadinessChecks() []modelCheck {
	readiness := shared.LastPrintReadiness
	if app.recommendation == nil {
		return nil
	}
	rows := make([]modelCheck, 0, len(readiness.Issues)+1)
	if len(readiness.Issues) == 0 {
		return append(rows, modelCheck{label: tr("checkSimulation"), detail: tr("checkSimulationClear"), level: 0})
	}
	for _, issue := range readiness.Issues {
		level := 1
		if issue.Severity >= 2 {
			level = 2
		}
		rows = append(rows, modelCheck{label: tr(issue.Key), detail: issue.Detail, level: level})
	}
	return rows
}

// The three layers each get their own row.
//
//	checkAI     what the model recognised
//	checkGuard  whether the guardrail believed it
//	checkSOG    what S.O.G did to the profile
//
// They were folded into one row before, and that hid the middle layer
// completely. The guardrail's whole purpose is to overrule the model when the
// mesh disagrees with it — which is precisely the event worth watching happen,
// and it was being reported as a footnote on the model's own row, as though the
// model had reported its own error.
func advisorCheck() (modelCheck, bool) {
	ready, starting, failure := advisorServer.status()
	switch {
	case starting:
		return modelCheck{label: tr("checkAI"), detail: tr("checkAILoading"), level: 1}, true
	case !ready:
		if failure != "" {
			return modelCheck{label: tr("checkAI"), detail: tr("checkAIOff"), level: 0}, true
		}
		return modelCheck{}, false
	}
	outcome := shared.LastAdvisorOutcome
	if !outcome.Used {
		return modelCheck{label: tr("checkAI"), detail: tr("checkAIReady"), level: 0}, true
	}
	// This row is the recognition and nothing else: what the model thought the
	// part was. Whether that was believed is the next row's business.
	object := strings.TrimSpace(outcome.Object)
	if object == "" || strings.EqualFold(object, "unknown") {
		return modelCheck{label: tr("checkAI"), detail: tr("checkAIUnnamed"), level: 0}, true
	}
	return modelCheck{label: tr("checkAI"), detail: object, level: 0}, true
}

// protectionCheck is the one-glance answer: are the two safety layers doing
// their job right now, yes or no.
//
// The rows below it report what each layer decided, which is detail — and
// detail does not answer "is this protecting me". That question wants a green
// dot or a red one, so this row carries no nuance on purpose: green only when
// both layers are operating on a real profile, red the moment they are not.
//
// They stop being able to work when no profile could be produced at all — an
// unresolved printer, a filament the machine cannot run. Nothing was checked
// then, and showing green over nothing would be the worst thing this row could
// do.
func protectionCheck() modelCheck {
	label := tr("checkProtection")
	if app.analysis != nil && app.recommendation == nil {
		// A model is loaded but no profile came out of it, so there was nothing
		// for either layer to inspect.
		return modelCheck{label: label, detail: tr("checkProtectionOff"), level: 2}
	}
	return modelCheck{label: label, detail: tr("checkProtectionOn"), level: 0}
}

// guardrailCheck reports the verdict passed on what the model said. Like the
// S.O.G row it is always present, because a layer that only appears when it
// objects cannot be told apart from one that is not running.
func guardrailCheck() modelCheck {
	outcome := shared.LastAdvisorOutcome
	row := func(key string, level int) modelCheck {
		return modelCheck{label: tr("checkGuard"), detail: tr(key), level: level}
	}
	switch {
	case !outcome.Used:
		return row("checkGuardIdle", 0)
	case outcome.Mismatch != "":
		// The event this layer exists for: the mesh contradicted the class, so
		// the class was withdrawn and only the name kept.
		return row("checkGuardWithdrawn", 1)
	case outcome.Detail != "":
		return modelCheck{label: tr("checkGuard"), detail: trf("checkGuardRejected", outcome.Detail), level: 1}
	case outcome.Abstained:
		return row("checkGuardNoProposal", 0)
	case outcome.Scaled:
		return row("checkGuardScaled", 0)
	case outcome.Accepted:
		return row("checkGuardConfirmed", 0)
	}
	return row("checkGuardIdle", 0)
}

func checkLevelColor(level int) uintptr {
	switch level {
	case 2:
		return th.warnColor
	case 1:
		return th.accent
	default:
		return th.okColor
	}
}

func drawPlanCard(hdc uintptr, r rect) {
	if height(r) < 60 {
		return
	}
	if checks := modelChecks(); len(checks) > 0 {
		eyebrow(hdc, tr("checksTitle"), rect{r.Left, r.Top, r.Right, r.Top + 18}, DT_LEFT|DT_VCENTER)
		y := r.Top + 24
		rowH := int32(24)
		for index, check := range checks {
			if y+rowH > r.Bottom {
				// Rows that do not fit used to vanish with no sign of it, which
				// is how a check could be reported and never seen. Say how many
				// are hidden instead of quietly losing them.
				if hidden := len(checks) - index; hidden > 0 && y <= r.Bottom {
					text(hdc, trf("checksHidden", hidden), rect{r.Left + 16, y, r.Right, y + rowH},
						hFontSmall, th.textMuted, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
					y += rowH
				}
				break
			}
			color := checkLevelColor(check.level)
			circle(hdc, r.Left+4, y+rowH/2, 4, color, color)
			text(hdc, check.label, rect{r.Left + 16, y, r.Left + width(r)*52/100, y + rowH}, hFontSmall, th.textMuted, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
			detailColor := th.textPrimary
			if check.level == 2 {
				detailColor = th.warnColor
			}
			text(hdc, check.detail, rect{r.Left + width(r)*52/100, y, r.Right, y + rowH}, hFontSmall, detailColor, DT_RIGHT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
			y += rowH
		}
		r = rect{r.Left, y + 12, r.Right, r.Bottom}
		if height(r) < 60 {
			return
		}
	}
	eyebrow(hdc, tr("planTitle"), rect{r.Left, r.Top, r.Right, r.Top + 18}, DT_LEFT|DT_VCENTER)
	if app.recommendation == nil {
		text(hdc, tr("planEmpty"), rect{r.Left, r.Top + 22, r.Right, r.Bottom - 4}, hFontSmall, th.textMuted, DT_LEFT|DT_TOP|DT_WORDBREAK)
		return
	}
	c := app.recommendation.CriticalValues
	rows := [][2]string{
		{tr("planLayer"), fmt.Sprintf("%.2f mm", c["layer_height"])},
		{tr("planWallSpeed"), fmt.Sprintf("%.0f mm/s", c["outer_wall_speed"])},
		{tr("planFlow"), fmt.Sprintf("%.1f mm³/s", c["max_volumetric_speed"])},
		{tr("planTemps"), fmt.Sprintf("%.0f°C / %.0f°C", c["nozzle_temperature"], c["bed_temperature"])},
		{tr("planSupports"), planSupportLabel()},
	}
	y := r.Top + 24
	rowH := int32(23)
	for _, row := range rows {
		if y+rowH > r.Bottom {
			break
		}
		text(hdc, row[0], rect{r.Left, y, r.Left + width(r)/2, y + rowH}, hFontSmall, th.textMuted, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
		text(hdc, row[1], rect{r.Left + width(r)/2, y, r.Right, y + rowH}, hFontSmall, th.textPrimary, DT_RIGHT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
		y += rowH
	}
}

func planSupportLabel() string {
	if app.analysis != nil && app.analysis.SupportSuggested {
		return tr("supportsOn")
	}
	return tr("supportsOff")
}

// One inspector row: label above, value below, chevron right, hairline under.
// The hover state is a full-bleed wash rather than a lifting card, which is how
// list rows behave in a native inspector.
// An inspector option card: icon tile, label, value, accent detail, chevron.
func drawSelectSlot(hdc uintptr, r rect, label, value, meta string, icon func(uintptr, int32, int32, uintptr), configured bool, id int) {
	r = lift(r, id)
	if hoverOf(id) > 0 {
		shade(hdc, r, 15, 10, 4, hoverShadow(id, 20))
	}
	hoverSunken(hdc, r, 15, id)

	tile := rect{r.Left + 12, (r.Top+r.Bottom)/2 - 19, r.Left + 50, (r.Top+r.Bottom)/2 + 19}
	iconColor := th.accent
	if !configured {
		iconColor = th.textMuted
	}
	drawSpatialRoundedMaterial(hdc, tile, 11, th.surface, th.surfaceAlt, th.stroke)
	icon(hdc, (tile.Left+tile.Right)/2, (tile.Top+tile.Bottom)/2, iconColor)

	textLeft := tile.Right + 14
	textRight := r.Right - 26
	hasMeta := meta != "" && height(r) >= 70
	top := r.Top + 12
	if !hasMeta {
		top = r.Top + 18
	}
	text(hdc, label, rect{textLeft, top, textRight, top + 22}, hFontBody, th.textPrimary, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	valueColor := th.textMuted
	if configured {
		valueColor = th.textSecondary
	}
	text(hdc, value, rect{textLeft, top + 20, textRight, top + 40}, hFontSmall, valueColor, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	if hasMeta {
		text(hdc, meta, rect{textLeft, top + 38, textRight, r.Bottom - 8}, hFontSmall, th.accentText, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	}
	drawChevron(hdc, r.Right-16+int32(hoverOf(id)*3), (r.Top+r.Bottom)/2, mixColor(th.textMuted, th.accent, hoverOf(id)))
}

func drawPrinterSlot(hdc uintptr, r rect) {
	printer, ok := selectedPrinter()
	value := tr("device")
	meta := tr("printerNotDetected")
	if ok {
		value = printer.Model
		meta = printer.Brand + " · " + tr("printerNozzle") + " " + selectedNozzleLabel()
	}
	drawSelectSlot(hdc, r, tr("navPrinter"), value, meta, drawPrinterIcon, ok, hoverPrinter)
}

func drawFilamentSlot(hdc uintptr, r rect) {
	f, ok := selectedFilament()
	value := tr("noFilament")
	meta := ""
	if ok {
		value = strings.TrimSpace(f.Brand + " " + f.Product)
		if value == "" {
			value = f.Material
		}
		meta = fmt.Sprintf("%s · %.0f°C / %.0f°C", f.Material, f.NozzleDefault, f.BedDefault)
	}
	drawSelectSlot(hdc, r, tr("navMaterial"), value, meta, drawSpoolIcon, ok, hoverFilament)
}

// A true segmented control: one recessed track, one moving pill.
func drawQualitySlot(hdc uintptr, r rect) {
	label := tr("stepQuality")
	if app.quality == "perfect" {
		label += " · " + currentTextureTitle()
	}
	eyebrow(hdc, label, rect{r.Left, r.Top, r.Right, r.Top + 18}, DT_LEFT|DT_VCENTER)

	track := rect{spatial.fast.Left, spatial.fast.Top, spatial.perfect.Right, spatial.perfect.Bottom}
	drawSpatialRoundedMaterial(hdc, track, 9, th.sunkenAlt, th.sunken, th.strokeSoft)
	drawSegment(hdc, spatial.fast, tr("qualityFast"), app.quality == "low", hoverFast)
	drawSegment(hdc, spatial.balanced, tr("qualityBalanced"), app.quality == "balanced", hoverBalanced)
	drawSegment(hdc, spatial.perfect, tr("qualityPerfect"), app.quality == "perfect", hoverPerfect)
}

func drawSegment(hdc uintptr, r rect, label string, selected bool, id int) {
	if selected {
		pill := inset(r, 3)
		shade(hdc, pill, 7, 6, 2, 30)
		accentFill(hdc, pill, 7)
		text(hdc, label, pill, hFontSmall, th.textOnAccent, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
		return
	}
	text(hdc, label, r, hFontSmall, mixColor(th.textSecondary, th.textPrimary, hoverOf(id)), DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
}

func drawPrimaryAction(hdc uintptr, r rect) {
	if app.ready && !app.importing {
		r = lift(r, hoverOpen)
		shade(hdc, r, 18, 12, 6, hoverShadow(hoverOpen, 46))
		accentFill(hdc, r, 18)
		drawButtonShimmer(hdc, r)
		text(hdc, tr("openFlash"), inset(r, 8), hFontButton, th.textOnAccent, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
		return
	}
	drawSpatialRoundedMaterial(hdc, r, 18, th.sunken, th.sunkenAlt, th.stroke)
	label := tr("openFlash")
	if app.importing {
		label = tr("working")
	}
	text(hdc, label, inset(r, 8), hFontButton, th.textMuted, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
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
		// One sweep every 2.4 s, timed off the clock rather than frame count.
		phase := animPhase(2.4)
		x := button.Left - 70 + int32(phase*float64(width(button)+140))
		drawSpatialGlow(hdc, x, (button.Top+button.Bottom)/2, 44, 31, rgb(255, 255, 255), 40)
		pDeleteObject.Call(region)
	}
	if saved != 0 {
		pRestoreDC.Call(hdc, saved)
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

func drawFooter(hdc uintptr, w, h int32) {
	r := spatial.status
	line(hdc, 0, r.Top, w, r.Top, th.strokeSoft, 1)
	cy := (r.Top + r.Bottom) / 2
	dotColor := th.accent
	if app.discovering || app.importing || app.analyzing {
		// Breathes on a 2 s cycle instead of blinking on a frame counter.
		glow(hdc, 22, cy, 14, 14, dotColor, uint8(40+animPulse(2.0)*60))
	}
	circle(hdc, 22, cy, 3, dotColor, dotColor)
	text(hdc, currentStatusText(), rect{34, r.Top, w - 200, r.Bottom}, hFontSmall, th.textSecondary, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	text(hdc, "v"+buildVersion, rect{w - 190, r.Top, w - 16, r.Bottom}, hFontSmall, th.textMuted, DT_RIGHT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
}

func drawSpatialScene(hdc uintptr, client rect) {
	w, h := width(client), height(client)
	spatial = calculateSpatialLayout(w, h)

	// On a chrome-only frame the canvas must be left exactly as it is. Skipping
	// the model render is not enough on its own: the background fill covers the
	// whole window and would wipe it first. Clipping the canvas out is what
	// actually protects those pixels, and it protects them from every drawing
	// call in the frame rather than just the one we remembered about.
	saved := uintptr(0)
	if sceneSkipStage && width(spatial.stage) > 0 && height(spatial.stage) > 0 {
		saved, _, _ = pSaveDC.Call(hdc)
		pExcludeClipRect.Call(hdc,
			i32arg(spatial.stage.Left), i32arg(spatial.stage.Top),
			i32arg(spatial.stage.Right), i32arg(spatial.stage.Bottom))
	}

	fillCanvas(hdc, client)
	drawAmbientLight(hdc, w, h)
	drawSidebar(hdc)
	drawWorkspace(hdc)
	drawInspector(hdc)
	drawHeader(hdc, w)
	drawStatusBar(hdc)

	if saved != 0 {
		pRestoreDC.Call(hdc, saved)
	}
}

var navIconDrawers = [5]func(uintptr, int32, int32, uintptr){
	drawCubeIcon, drawPrinterIcon, drawSpoolIcon, drawSlidersIcon, drawLayersIcon,
}

func navLabels() [5]string {
	return [5]string{tr("navModel"), tr("navPrinter"), tr("navMaterial"), tr("navSettings"), tr("navPreview")}
}

// Which rail entry is lit: the first thing still missing, so the rail doubles as
// a progress indicator without numbering the steps.
func activeNavIndex() int {
	switch {
	case app.modelPath == "":
		return 0
	case app.printer.ID == "":
		return 1
	default:
		if _, ok := selectedFilament(); !ok {
			return 2
		}
	}
	return 4
}

func drawSidebar(hdc uintptr) {
	panel := spatial.sidebar
	shade(hdc, panel, 20, 20, 10, 30)
	card(hdc, panel, 20)

	labels := navLabels()
	active := activeNavIndex()
	for i := 0; i < 5; i++ {
		drawNavItem(hdc, spatial.nav[i], labels[i], navIconDrawers[i], i == active, hoverNav1+i)
	}
	if width(spatial.promo) > 0 {
		drawPromoCard(hdc, spatial.promo)
	}
}

func drawNavItem(hdc uintptr, r rect, label string, icon func(uintptr, int32, int32, uintptr), active bool, id int) {
	iconColor := th.textMuted
	labelColor := th.textSecondary
	switch {
	case active:
		shade(hdc, r, 13, 9, 4, hoverShadow(id, 24))
		accentTint(hdc, r, 13)
		iconColor = th.accent
		labelColor = th.textPrimary
	case hoverOf(id) > 0:
		hoverSunken(hdc, r, 13, id)
		labelColor = th.textPrimary
	}
	icon(hdc, r.Left+26, (r.Top+r.Bottom)/2, iconColor)
	text(hdc, label, rect{r.Left + 48, r.Top, r.Right - 10, r.Bottom}, hFontBody, labelColor, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
}

// The sidebar card is where the question actually gets asked, so it is where it
// gets answered.
//
// It used to be a slogan: a star, the words "Guardrail attivi", and a sentence,
// painted identically whether or not anything was being checked. A claim that
// cannot come out false is not a status — it is decoration that looks like one,
// which is worse than no indicator at all, and this is the most prominent place
// in the window.
//
// Now both layers report here with a dot each, and the card can say no.
func drawPromoCard(hdc uintptr, r rect) {
	sunkenChip(hdc, r, 15)

	rowH := int32(22)
	y := r.Top + 10
	statusRow := func(label string, ok bool) {
		color := th.okColor
		if !ok {
			color = th.warnColor
		}
		circle(hdc, r.Left+18, y+rowH/2, 4, color, color)
		text(hdc, label, rect{r.Left + 30, y, r.Right - 12, y + rowH}, hFontBody, th.textPrimary,
			DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
		y += rowH
	}
	guardLabel, guardOK := guardrailState()
	sogLabel, sogOK := sogState()
	statusRow(guardLabel, guardOK)
	statusRow(sogLabel, sogOK)

	if body := r.Bottom - 8 - (y + 4); body > 14 {
		text(hdc, tr("promoBody"), rect{r.Left + 14, y + 4, r.Right - 14, r.Bottom - 8}, hFontSmall, th.textMuted,
			DT_LEFT|DT_TOP|DT_WORDBREAK)
	}
}

// guardrailState and sogState are the two dots. Green means the layer inspected
// this profile; red means it did not, and the two ways that happens are worth
// telling apart.
//
// Nothing to inspect is the quiet one: a model is loaded but no profile came
// out of it — an unresolved printer, a filament the machine cannot run — so
// neither layer saw anything. Held is the loud one: S.O.G looked and refused to
// clear the print. Both are red, because both mean the profile in front of the
// user has not been approved.
func guardrailState() (string, bool) {
	if app.analysis != nil && app.recommendation == nil {
		return tr("promoGuardrailIdle"), false
	}
	return tr("promoGuardrailOn"), true
}

func sogState() (string, bool) {
	if app.analysis != nil && app.recommendation == nil {
		return tr("promoSOGIdle"), false
	}
	if app.recommendation != nil && !shared.LastSOGVerdict.Cleared {
		return tr("promoSOGHeld"), false
	}
	return tr("promoSOGOn"), true
}

func drawCubeIcon(hdc uintptr, cx, cy int32, color uintptr) {
	line(hdc, cx-9, cy-5, cx, cy-10, color, 2)
	line(hdc, cx, cy-10, cx+9, cy-5, color, 2)
	line(hdc, cx+9, cy-5, cx+9, cy+5, color, 2)
	line(hdc, cx+9, cy+5, cx, cy+10, color, 2)
	line(hdc, cx, cy+10, cx-9, cy+5, color, 2)
	line(hdc, cx-9, cy+5, cx-9, cy-5, color, 2)
	line(hdc, cx-9, cy-5, cx, cy, color, 1)
	line(hdc, cx+9, cy-5, cx, cy, color, 1)
	line(hdc, cx, cy, cx, cy+10, color, 1)
}

func drawLayersIcon(hdc uintptr, cx, cy int32, color uintptr) {
	for _, off := range []int32{-6, 0, 6} {
		line(hdc, cx-9, cy+off, cx, cy+off-4, color, 2)
		line(hdc, cx, cy+off-4, cx+9, cy+off, color, 2)
	}
}

func drawWorkspace(hdc uintptr) {
	panel := spatial.workspace
	shade(hdc, panel, 20, 22, 11, 32)
	card(hdc, panel, 20)
	text(hdc, tr("workspaceTitle"), rect{panel.Left + 24, panel.Top + 14, panel.Right - 24, panel.Top + 42}, hFontHeading, th.textPrimary, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	text(hdc, tr("workspaceSubtitle"), rect{panel.Left + 24, panel.Top + 40, panel.Right - 24, panel.Top + 60}, hFontSmall, th.textMuted, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)

	// A hover frame changes chrome, never the canvas. The canvas pixels already
	// in the back buffer are still correct, and re-rendering the neon envelope
	// to paint a menu highlight was the single largest cost of a hover frame.
	if !sceneSkipStage {
		drawStage(hdc)
	}
	drawToolStrip(hdc)

	sunkenChip(hdc, spatial.dimension, 15)
	text(hdc, currentVolumeLabel(), spatial.dimension, hFontSmall, th.textSecondary, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	drawDropZone(hdc, spatial.drop)
}

func currentVolumeLabel() string {
	if app.analysis != nil {
		a := *app.analysis
		return fmt.Sprintf("%.0f × %.0f × %.0f mm", a.Extents[0], a.Extents[1], a.Extents[2])
	}
	volume := [3]float64{220, 220, 220}
	if printer, ok := selectedPrinter(); ok && printer.BuildVolume[0] > 0 {
		volume = printer.BuildVolume
	}
	return fmt.Sprintf("%.0f × %.0f × %.0f mm", volume[0], volume[1], volume[2])
}

func drawToolStrip(hdc uintptr) {
	strip := rect{spatial.tools[0].Left - 6, spatial.tools[0].Top - 6, spatial.tools[3].Right + 6, spatial.tools[3].Bottom + 6}
	shade(hdc, strip, 15, 10, 5, 24)
	sunkenChip(hdc, strip, 15)
	for i, r := range spatial.tools {
		id := hoverTool1 + i
		if hoverOf(id) > 0 {
			accentTint(hdc, r, 9)
		}
		color := mixColor(th.textMuted, th.accent, hoverOf(id))
		cx, cy := (r.Left+r.Right)/2, (r.Top+r.Bottom)/2
		switch i {
		case 0:
			drawMoveIcon(hdc, cx, cy, color)
		case 1:
			drawRotateIcon(hdc, cx, cy, color)
		case 2:
			drawFitIcon(hdc, cx, cy, color)
		default:
			drawCubeIcon(hdc, cx, cy, color)
		}
	}
}

func drawMoveIcon(hdc uintptr, cx, cy int32, color uintptr) {
	line(hdc, cx-9, cy, cx+9, cy, color, 2)
	line(hdc, cx, cy-9, cx, cy+9, color, 2)
	line(hdc, cx-9, cy, cx-5, cy-4, color, 2)
	line(hdc, cx-9, cy, cx-5, cy+4, color, 2)
	line(hdc, cx+9, cy, cx+5, cy-4, color, 2)
	line(hdc, cx+9, cy, cx+5, cy+4, color, 2)
	line(hdc, cx, cy-9, cx-4, cy-5, color, 2)
	line(hdc, cx, cy-9, cx+4, cy-5, color, 2)
	line(hdc, cx, cy+9, cx-4, cy+5, color, 2)
	line(hdc, cx, cy+9, cx+4, cy+5, color, 2)
}

func drawRotateIcon(hdc uintptr, cx, cy int32, color uintptr) {
	circle(hdc, cx, cy, 8, th.surface, color)
	line(hdc, cx+6, cy-6, cx+9, cy-2, color, 2)
	line(hdc, cx+9, cy-2, cx+4, cy-1, color, 2)
}

func drawFitIcon(hdc uintptr, cx, cy int32, color uintptr) {
	for _, c := range [4][2]int32{{-8, -8}, {8, -8}, {-8, 8}, {8, 8}} {
		line(hdc, cx+c[0], cy+c[1], cx+c[0]/2, cy+c[1], color, 2)
		line(hdc, cx+c[0], cy+c[1], cx+c[0], cy+c[1]/2, color, 2)
	}
}

func drawDropZone(hdc uintptr, r rect) {
	if app.modelPath != "" {
		sunkenChip(hdc, r, 16)
		text(hdc, filepath.Base(app.modelPath), rect{r.Left + 20, r.Top + 12, r.Right - 20, r.Top + 42}, hFontBody, th.textPrimary, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
		text(hdc, tr("dropSubtitle"), rect{r.Left + 20, r.Top + 40, r.Right - 20, r.Bottom - 10}, hFontSmall, th.textMuted, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
		return
	}
	drawDashedFrame(hdc, r)
	box := rect{r.Left + 20, r.Top + 16, r.Left + 66, r.Bottom - 16}
	accentTint(hdc, box, 12)
	drawUploadIcon(hdc, (box.Left+box.Right)/2, (box.Top+box.Bottom)/2, th.accent)
	text(hdc, tr("dropTitle"), rect{box.Right + 16, r.Top + 14, r.Right - 16, r.Top + 44}, hFontHeading, th.textPrimary, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	text(hdc, ".stl, .obj, .3mf", rect{box.Right + 16, r.Top + 42, r.Right - 16, r.Bottom - 12}, hFontSmall, th.textMuted, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
}

func drawStatusBar(hdc uintptr) {
	r := spatial.status
	shade(hdc, r, 18, 16, 8, 28)
	card(hdc, r, 18)

	blocked := app.analysis != nil && shared.ValidateAnalysis(*app.analysis) != nil
	badge := rect{r.Left + 16, (r.Top+r.Bottom)/2 - 17, r.Left + 50, (r.Top+r.Bottom)/2 + 17}
	badgeColor := th.okColor
	if blocked {
		badgeColor = th.warnColor
	}
	glow(hdc, (badge.Left+badge.Right)/2, (badge.Top+badge.Bottom)/2, 30, 30, badgeColor, 60)
	circle(hdc, (badge.Left+badge.Right)/2, (badge.Top+badge.Bottom)/2, 17, badgeColor, badgeColor)
	text(hdc, "✓", badge, hFontBody, th.textOnAccent, DT_CENTER|DT_VCENTER|DT_SINGLELINE)

	headline := tr("statusHealthy")
	if blocked {
		headline = tr("statusBlocked")
	}
	text(hdc, headline, rect{badge.Right + 14, r.Top + 12, spatial.stats.Left - 20, r.Top + 38}, hFontBody, th.textPrimary, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	text(hdc, currentStatusText(), rect{badge.Right + 14, r.Top + 34, spatial.stats.Left - 20, r.Bottom - 12}, hFontSmall, th.textMuted, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)

	drawStatChips(hdc, spatial.stats)
}

func drawStatChips(hdc uintptr, r rect) {
	filament, duration, weight := "—", "—", "—"
	if app.recommendation != nil {
		minutes := app.recommendation.EstimatedModeMinutes
		duration = fmt.Sprintf("%dh %02dm", int(minutes)/60, int(minutes)%60)
	}
	if app.analysis != nil {
		cubicMM := app.analysis.Volume
		filament = fmt.Sprintf("%.2f m", cubicMM/(math.Pi*0.875*0.875)/1000)
		// Use the density of the filament actually selected. A fixed 1.24 is
		// PLA's; PETG is denser, and printing PETG the estimate came out low by
		// roughly that difference.
		density := 1.24
		if f, ok := selectedFilament(); ok && f.Density > 0 {
			density = f.Density
		}
		weight = fmt.Sprintf("%.0f g", cubicMM/1000*density)
	}
	chipW := width(r) / 3
	for i, pair := range [3][2]string{{tr("statFilament"), filament}, {tr("statTime"), duration}, {tr("statWeight"), weight}} {
		box := rect{r.Left + int32(i)*chipW, r.Top + 14, r.Left + int32(i+1)*chipW - 10, r.Bottom - 14}
		eyebrow(hdc, pair[0], rect{box.Left, box.Top, box.Right, box.Top + 16}, DT_LEFT|DT_VCENTER)
		text(hdc, pair[1], rect{box.Left, box.Top + 14, box.Right, box.Bottom}, hFontBody, th.textPrimary, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	}
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
	if spatialResizing {
		drawSpatialResizePlaceholder(hdc, client)
		return
	}
	if spatialViewportWidth != 0 && (spatialViewportWidth != w || spatialViewportHeight != h) {
		// Cached materials are keyed by geometry. Drop the old viewport in one
		// controlled operation so resize/maximize cannot grow GDI memory forever.
		cleanupSpatialMaterialSystem()
	}
	spatialViewportWidth, spatialViewportHeight = w, h
	if !mainSpatialBuffer.ensure(hdc, w, h) {
		drawSpatialScene(hdc, client)
		return
	}
	// Every partial repaint below reuses what the buffer already holds, so a
	// buffer that has never been fully painted must get a full frame first.
	if mainSpatialBuffer.fresh {
		stageOnlyFrame, sceneSkipStage = false, false
	}
	// An animation frame only redraws the stage into the existing buffer and
	// blits that rectangle back. The chrome around it is already correct, and
	// redrawing it sixty times a second was costing most of a CPU core.
	if stageOnlyFrame && spatialLayoutValid && width(spatial.stage) > 0 {
		stageOnlyFrame = false
		area := spatial.stage
		redrawStageRegion(mainSpatialBuffer.dc)
		pBitBlt.Call(hdc, i32arg(area.Left), i32arg(area.Top), uintptr(uint32(width(area))), uintptr(uint32(height(area))),
			mainSpatialBuffer.dc, i32arg(area.Left), i32arg(area.Top), SRCCOPY)
		return
	}
	stageOnlyFrame = false
	drawSpatialScene(mainSpatialBuffer.dc, client)
	sceneSkipStage = false
	spatialLayoutValid = true
	// The buffer now holds a complete frame, so partial repaints are safe again.
	mainSpatialBuffer.fresh = false
	pBitBlt.Call(hdc, 0, 0, uintptr(w), uintptr(h), mainSpatialBuffer.dc, 0, 0, SRCCOPY)
}

// redrawStageRegion repaints only the canvas, clipped to its own bounds so a
// stray glow cannot bleed over the chrome it is not allowed to touch.
func redrawStageRegion(hdc uintptr) {
	saved, _, _ := pSaveDC.Call(hdc)
	area := spatial.stage
	region, _, _ := pCreateRoundRectRgn.Call(i32arg(area.Left), i32arg(area.Top), i32arg(area.Right+1), i32arg(area.Bottom+1), 1, 1)
	if region != 0 {
		pSelectClipRgn.Call(hdc, region)
	}
	drawStage(hdc)
	if region != 0 {
		pDeleteObject.Call(region)
	}
	if saved != 0 {
		pRestoreDC.Call(hdc, saved)
	}
}

func drawSpatialResizePlaceholder(hdc uintptr, client rect) {
	fillCanvas(hdc, client)
	text(hdc, "FlashFit AI", client, hFontLogo, th.textPrimary, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
}

func spatialClick(x, y int32) {
	switch {
	case contains(spatial.theme, x, y):
		toggleUITheme()
	case contains(spatial.language, x, y):
		showLanguageMenu()
	case contains(spatial.aiLight, x, y):
		chooseAdvisorModel("light")
	case contains(spatial.aiHeavy, x, y):
		chooseAdvisorModel("strong")
	case contains(spatial.aiStatus, x, y):
		showAdvisorModelMenu()
	case contains(spatial.advanced, x, y):
		showAdvancedMenu()
	case contains(spatial.printer, x, y):
		showPrinterMenu()
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
	case contains(spatial.model, x, y), contains(spatial.drop, x, y):
		chooseAndSetModel()
	case contains(spatial.nav[0], x, y):
		chooseAndSetModel()
	case contains(spatial.nav[1], x, y):
		showPrinterPicker()
	case contains(spatial.nav[2], x, y):
		showFilamentPicker()
	case contains(spatial.nav[3], x, y):
		showAdvancedMenu()
	case contains(spatial.nav[4], x, y):
		if app.quality == "perfect" {
			showTexturePicker()
		} else {
			setQuality("perfect")
		}
	case contains(spatial.tools[2], x, y):
		resetStageCamera()
		invalidateSpatial()
	case contains(spatial.stage, x, y):
		chooseAndSetModel()
	}
}

func showPrinterMenu() {
	showPrinterPicker()
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
	saveUISettings()
	setSpatialTitle()
	updateFilamentDialogLanguage()
	invalidateSpatial()
}

// The model chooser. A heavier model recognises unusual parts better but needs
// the memory to match; the light one always runs. Print quality does not follow
// from this choice — the settings are computed from the recognised class, not
// by the model — so a smaller model prints just as correctly, it simply
// identifies fewer things.
func showAdvisorModelMenu() {
	choices := advisorAvailableModels()
	if len(choices) == 0 {
		messageBox(mainHwnd, trf("aiNoModels", advisorModelsDir()), appTitle, MB_OK|MB_ICONINFORMATION)
		return
	}
	items := make([]struct {
		id    int
		label string
	}, 0, len(choices)+1)
	for i, choice := range choices {
		label := choice.Label
		if choice.SizeMB > 0 {
			label = fmt.Sprintf("%s  (%d MB)", label, choice.SizeMB)
		}
		selected := (choice.Embedded && advisorSelectedModel == "") ||
			(!choice.Embedded && strings.EqualFold(choice.Path, advisorSelectedModel))
		if selected {
			label = "• " + label
		}
		items = append(items, struct {
			id    int
			label string
		}{idAdvisorModelBase + i, label})
	}
	items = append(items, struct {
		id    int
		label string
	}{idAdvisorFolder, tr("aiOpenFolder")})

	cmd := popupCommand(items)
	switch {
	case cmd == idAdvisorFolder:
		_ = os.MkdirAll(advisorModelsDir(), 0o700)
		_ = exec.Command("explorer.exe", advisorModelsDir()).Start()
	case cmd >= idAdvisorModelBase && cmd < idAdvisorModelBase+len(choices):
		selectAdvisorModel(choices[cmd-idAdvisorModelBase].Path)
	}
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
	h, _, _ := pCreateWindowEx.Call(0x00000001, uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(title)), WS_OVERLAPPEDWINDOW|WS_VISIBLE|WS_CLIPCHILDREN, 190, 105, 920, 670, mainHwnd, 0, inst, 0)
	if h != 0 {
		hFilamentDialog = h
		applyWindowChrome(h)
		pShowWindow.Call(h, SW_SHOWNORMAL)
		pSetForegroundWindow.Call(h)
	}
}

const filamentClassName = "FlashFitAI_Spatial_FilamentPicker_v41"

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
	case WM_GETMINMAXINFO:
		mmi := (*minMaxInfo)(unsafe.Pointer(lParam))
		mmi.MinTrackSize = point{X: 760, Y: 540}
		return 0
	case WM_CREATE:
		hFilamentDialog = hwnd
		hSearch = control(hwnd, "EDIT", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|ES_AUTOHSCROLL, 0, idFilamentSearch)
		cue := utf16Ptr(tr("searchFilament"))
		pSendMessage.Call(hSearch, EM_SETCUEBANNER, 1, uintptr(unsafe.Pointer(cue)))
		pSetTimer.Call(hwnd, idFilamentAnimation, spatialAnimationInterval, 0)
		refreshFilamentList()
		layoutFilamentDialog(hwnd)
		pSetFocus.Call(hSearch)
		return 0
	case WM_PAINT:
		paintFilamentPicker(hwnd)
		return 0
	case WM_ERASEBKGND:
		return 1
	case WM_CTLCOLOREDIT:
		return themedEditBackground(wParam)
	case WM_TIMER:
		foreground, _, _ := pGetForegroundWindow.Call()
		if wParam == idFilamentAnimation && foreground == hwnd {
			filamentPickerAnimationTick++
			pInvalidateRect.Call(hwnd, 0, 0)
		}
		return 0
	case WM_MOUSEWHEEL:
		delta := int16((wParam >> 16) & 0xffff)
		if delta > 0 {
			filamentPickerScroll--
		} else {
			filamentPickerScroll++
		}
		clampFilamentScroll()
		pInvalidateRect.Call(hwnd, 0, 0)
		return 0
	case WM_KEYDOWN:
		switch wParam {
		case VK_ESCAPE:
			pDestroyWindow.Call(hwnd)
		case VK_RETURN:
			pDestroyWindow.Call(hwnd)
		case VK_UP:
			if row := selectedFilamentRow(); row > 0 {
				selectFilamentFromFilteredRow(row - 1)
			}
		case VK_DOWN:
			if row := selectedFilamentRow(); row >= 0 && row < len(app.filtered)-1 {
				selectFilamentFromFilteredRow(row + 1)
			}
		}
		return 0
	case WM_LBUTTONUP:
		x, y := pointFromLParam(lParam)
		if contains(filamentLayout.search, x, y) {
			pSetFocus.Call(hSearch)
			return 0
		}
		if row := filamentPickerHitRow(x, y); row >= 0 {
			selectFilamentFromFilteredRow(row)
			return 0
		}
		if contains(filamentLayout.apply, x, y) || contains(filamentLayout.close, x, y) {
			pDestroyWindow.Call(hwnd)
			return 0
		}
		return 0
	case WM_SIZE:
		filamentPickerBuffer.reset()
		layoutFilamentDialog(hwnd)
		return 0
	case WM_COMMAND:
		id := int(wParam & 0xffff)
		notify := int((wParam >> 16) & 0xffff)
		switch id {
		case idFilamentSearch:
			if notify == EN_CHANGE {
				filamentPickerScroll = 0
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
		pKillTimer.Call(hwnd, idFilamentAnimation)
		filamentPickerBuffer.reset()
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
	filamentLayout = calculateFilamentLayout(int32(w), int32(h))
	search := filamentLayout.search
	move(hSearch, int(search.Left+54), int(search.Top+8), int(width(search)-74), int(height(search)-16))
	ensureSelectedFilamentVisible()
	pInvalidateRect.Call(hwnd, 0, 0)
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
		parts = append(parts, filepath.Base(app.slicer))
	}
	if app.machine != "" {
		parts = append(parts, selectedPrinterLabel())
	}
	if app.process != "" {
		parts = append(parts, app.quality)
	}
	return fmt.Sprintf("%d/3 %s", len(parts), strings.Join(parts, " • "))
}
