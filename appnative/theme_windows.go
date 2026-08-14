//go:build windows

package main

import "unsafe"

// Every colour the Windows frontend paints comes from one of these roles, so a
// theme switch is a single palette swap instead of a sweep over draw calls.
type palette struct {
	dark bool

	canvas     uintptr
	canvasTop  uintptr
	surface    uintptr
	surfaceAlt uintptr
	sunken     uintptr
	sunkenAlt  uintptr
	stroke     uintptr
	strokeSoft uintptr

	textPrimary   uintptr
	textSecondary uintptr
	textMuted     uintptr
	textOnAccent  uintptr

	accent       uintptr
	accentAlt    uintptr
	accentStroke uintptr
	accentText   uintptr
	accentTintA  uintptr
	accentTintB  uintptr
	okColor      uintptr
	warnColor    uintptr

	// Rounded surfaces are composited from a bitmap, so shadows need their own
	// colour and a per-theme gain: the same alpha that reads as a soft drop on
	// white disappears entirely on a dark canvas.
	shadow     uintptr
	shadowGain uint16

	glowCool uintptr
	glowWarm uintptr
	glowMint uintptr
	glowGain uint16

	stageTop    uintptr
	stageBottom uintptr
	stageStroke uintptr
	stageGrid   uintptr
	stageGridHi uintptr

	captionBG   uintptr
	captionText uintptr
	captionEdge uintptr
}

var lightPalette = palette{
	dark: false,

	canvas:     rgb(238, 242, 249),
	canvasTop:  rgb(250, 251, 254),
	surface:    rgb(255, 255, 255),
	surfaceAlt: rgb(244, 247, 252),
	sunken:     rgb(249, 251, 254),
	sunkenAlt:  rgb(239, 243, 250),
	stroke:     rgb(225, 231, 242),
	strokeSoft: rgb(235, 239, 247),

	textPrimary:   rgb(23, 28, 40),
	textSecondary: rgb(91, 102, 128),
	textMuted:     rgb(138, 148, 168),
	textOnAccent:  rgb(255, 255, 255),

	accent:       rgb(76, 111, 242),
	accentAlt:    rgb(109, 70, 242),
	accentStroke: rgb(135, 151, 255),
	accentText:   rgb(70, 88, 214),
	accentTintA:  rgb(248, 250, 255),
	accentTintB:  rgb(236, 241, 255),
	okColor:      rgb(52, 190, 128),
	warnColor:    rgb(240, 160, 60),

	shadow:     rgb(56, 72, 125),
	shadowGain: 100,

	glowCool: rgb(214, 224, 255),
	glowWarm: rgb(224, 212, 255),
	glowMint: rgb(198, 224, 255),
	glowGain: 100,

	stageTop:    rgb(252, 253, 255),
	stageBottom: rgb(240, 244, 251),
	stageStroke: rgb(222, 229, 242),
	stageGrid:   rgb(226, 232, 245),
	stageGridHi: rgb(206, 217, 240),

	captionBG:   rgb(246, 248, 252),
	captionText: rgb(24, 29, 40),
	captionEdge: rgb(219, 226, 240),
}

var darkPalette = palette{
	dark: true,

	canvas:     rgb(8, 10, 15),
	canvasTop:  rgb(20, 24, 34),
	surface:    rgb(28, 33, 45),
	surfaceAlt: rgb(22, 26, 36),
	sunken:     rgb(33, 39, 52),
	sunkenAlt:  rgb(26, 31, 42),
	stroke:     rgb(48, 56, 72),
	strokeSoft: rgb(38, 45, 59),

	textPrimary:   rgb(236, 241, 249),
	textSecondary: rgb(154, 166, 188),
	textMuted:     rgb(112, 124, 145),
	textOnAccent:  rgb(255, 255, 255),

	accent:       rgb(96, 133, 255),
	accentAlt:    rgb(129, 94, 255),
	accentStroke: rgb(126, 149, 255),
	accentText:   rgb(150, 176, 255),
	accentTintA:  rgb(42, 51, 76),
	accentTintB:  rgb(33, 40, 62),
	okColor:      rgb(46, 200, 130),
	warnColor:    rgb(245, 170, 70),

	shadow:     rgb(0, 0, 0),
	shadowGain: 210,

	glowCool: rgb(56, 84, 190),
	glowWarm: rgb(88, 62, 190),
	glowMint: rgb(44, 96, 178),
	glowGain: 62,

	stageTop:    rgb(24, 29, 40),
	stageBottom: rgb(15, 19, 27),
	stageStroke: rgb(46, 54, 70),
	stageGrid:   rgb(40, 48, 64),
	stageGridHi: rgb(56, 67, 90),

	captionBG:   rgb(13, 16, 23),
	captionText: rgb(236, 241, 249),
	captionEdge: rgb(40, 47, 62),
}

var th = lightPalette

func applyThemePalette() {
	if uiTheme == "dark" {
		th = darkPalette
		return
	}
	th = lightPalette
}

// Shadow and glow bitmaps are cached under their colour, so a theme swap must
// drop them or the old palette keeps being blitted.
func setUITheme(name string) {
	if name != "dark" {
		name = "light"
	}
	if name == uiTheme {
		return
	}
	uiTheme = name
	applyThemePalette()
	saveUISettings()
	cleanupSpatialMaterialSystem()
	clearBackdropCache()
	clearTexturePreviewCache()
	releaseThemedEditBrush()
	for _, hwnd := range []uintptr{mainHwnd, hPrinterPicker, hFilamentDialog, hTexturePicker} {
		if hwnd != 0 {
			applyWindowChrome(hwnd)
			pInvalidateRect.Call(hwnd, 0, 0)
		}
	}
}

func toggleUITheme() {
	if uiTheme == "dark" {
		setUITheme("light")
		return
	}
	setUITheme("dark")
}

// Native title bar tinting. Unsupported attributes on older builds are ignored.
func applyWindowChrome(hwnd uintptr) {
	corner := uint32(2) // DWMWCP_ROUND
	pDwmSetWindowAttr.Call(hwnd, 33, uintptr(unsafe.Pointer(&corner)), unsafe.Sizeof(corner))
	darkMode := uint32(0)
	if th.dark {
		darkMode = 1
	}
	pDwmSetWindowAttr.Call(hwnd, 20, uintptr(unsafe.Pointer(&darkMode)), unsafe.Sizeof(darkMode))
	caption := uint32(th.captionBG)
	captionText := uint32(th.captionText)
	border := uint32(th.captionEdge)
	pDwmSetWindowAttr.Call(hwnd, 35, uintptr(unsafe.Pointer(&caption)), unsafe.Sizeof(caption))
	pDwmSetWindowAttr.Call(hwnd, 36, uintptr(unsafe.Pointer(&captionText)), unsafe.Sizeof(captionText))
	pDwmSetWindowAttr.Call(hwnd, 34, uintptr(unsafe.Pointer(&border)), unsafe.Sizeof(border))
}

var themedEditBrush uintptr

// Native EDIT controls paint themselves with the system colours, which reads as
// a white slab inside a dark window. WM_CTLCOLOREDIT hands them the theme.
func themedEditBackground(deviceContext uintptr) uintptr {
	pSetTextColor.Call(deviceContext, th.textPrimary)
	pSetBkColor.Call(deviceContext, th.sunken)
	if themedEditBrush == 0 {
		themedEditBrush = brush(th.sunken)
	}
	return themedEditBrush
}

func releaseThemedEditBrush() {
	if themedEditBrush != 0 {
		pDeleteObject.Call(themedEditBrush)
		themedEditBrush = 0
	}
}

func scaleOpacity(value uint8, gain uint16) uint8 {
	scaled := uint32(value) * uint32(gain) / 100
	if scaled > 255 {
		scaled = 255
	}
	return uint8(scaled)
}

func shade(hdc uintptr, r rect, radius, blur, offset int32, strength uint8) {
	drawSpatialSoftShadow(hdc, r, radius, blur, offset, th.shadow, scaleOpacity(strength, th.shadowGain))
}

func glow(hdc uintptr, centerX, centerY, radiusX, radiusY int32, color uintptr, strength uint8) {
	drawSpatialGlow(hdc, centerX, centerY, radiusX, radiusY, color, scaleOpacity(strength, th.glowGain))
}

// Panels are frosted rather than opaque: they take their fill from the backdrop
// they cover, lifted toward the surface colour. Because the backdrop has no
// detail to lose, this is what a real blur of it would look like.
const (
	frostPanelOpacity = 0.74
	frostChipOpacity  = 0.82
)

func card(hdc uintptr, r rect, radius int32) {
	cardOutlined(hdc, r, radius, th.stroke)
}

func cardOutlined(hdc uintptr, r rect, radius int32, stroke uintptr) {
	if backdropUsable(backdropViewportW, backdropViewportH) {
		top, bottom := frostedStops(r, backdropViewportW, backdropViewportH, frostPanelOpacity)
		drawSpatialRoundedMaterial(hdc, r, radius, top, bottom, stroke)
		return
	}
	drawSpatialRoundedMaterial(hdc, r, radius, th.surface, th.surfaceAlt, stroke)
}

func sunkenChip(hdc uintptr, r rect, radius int32) {
	if backdropUsable(backdropViewportW, backdropViewportH) {
		top, bottom := frostedStops(r, backdropViewportW, backdropViewportH, frostChipOpacity)
		drawSpatialRoundedMaterial(hdc, r, radius, mixColor(top, th.sunken, 0.45), mixColor(bottom, th.sunkenAlt, 0.45), th.strokeSoft)
		return
	}
	drawSpatialRoundedMaterial(hdc, r, radius, th.sunken, th.sunkenAlt, th.strokeSoft)
}

func accentFill(hdc uintptr, r rect, radius int32) {
	drawSpatialRoundedMaterial(hdc, r, radius, th.accent, th.accentAlt, th.accentStroke)
}

func accentTint(hdc uintptr, r rect, radius int32) {
	drawSpatialRoundedMaterial(hdc, r, radius, th.accentTintA, th.accentTintB, th.accentStroke)
}

type trivertex struct {
	X, Y                    int32
	Red, Green, Blue, Alpha uint16
}

type gradientRect struct{ UpperLeft, LowerRight uint32 }

func channels16(color uintptr) (uint16, uint16, uint16) {
	r, g, b := spatialColorChannels(color)
	return uint16(r) << 8, uint16(g) << 8, uint16(b) << 8
}

func verticalGradient(hdc uintptr, r rect, top, bottom uintptr) bool {
	tr, tg, tb := channels16(top)
	br, bg, bb := channels16(bottom)
	vertices := [2]trivertex{
		{X: r.Left, Y: r.Top, Red: tr, Green: tg, Blue: tb, Alpha: 0},
		{X: r.Right, Y: r.Bottom, Red: br, Green: bg, Blue: bb, Alpha: 0},
	}
	mesh := gradientRect{UpperLeft: 0, LowerRight: 1}
	ok, _, _ := pGradientFill.Call(
		hdc, uintptr(unsafe.Pointer(&vertices[0])), 2,
		uintptr(unsafe.Pointer(&mesh)), 1, 1, // GRADIENT_FILL_RECT_V
	)
	return ok != 0
}

func fillCanvas(hdc uintptr, client rect) {
	if drawBackdrop(hdc, client) {
		return
	}
	if verticalGradient(hdc, client, th.canvasTop, th.canvas) {
		return
	}
	background := brush(th.canvas)
	pFillRect.Call(hdc, uintptr(unsafe.Pointer(&client)), background)
	pDeleteObject.Call(background)
}

// The ambient rig every FlashFit surface is lit with: two broad colour washes
// and a tight accent, sized to the window so the light reads at any dimension.
func drawAmbientLight(hdc uintptr, w, h int32) {
	glow(hdc, w*20/100, 40, min32(560, w*44/100), 330, th.glowCool, 46)
	glow(hdc, w*84/100, h-20, min32(620, w*46/100), 380, th.glowWarm, 40)
	glow(hdc, w*58/100, 96, min32(360, w*27/100), 240, th.glowMint, 26)
}
