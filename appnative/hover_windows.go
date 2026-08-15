//go:build windows

package main

import "unsafe"

// Pointer feedback for the main shell. Every interactive region owns an eased
// 0..1 amount that the animation tick walks toward its target, so controls lift
// and settle instead of snapping between two states.

const (
	hoverNone = iota
	hoverTheme
	hoverLanguage
	hoverAdvanced
	hoverModel
	hoverPrinter
	hoverFilament
	hoverFast
	hoverBalanced
	hoverPerfect
	hoverOpen
	hoverStage
	hoverNav1
	hoverNav2
	hoverNav3
	hoverNav4
	hoverNav5
	hoverTool1
	hoverTool2
	hoverTool3
	hoverTool4
	hoverAILight
	hoverAIHeavy
	hoverRepoLink
	hoverRegionCount
)

var (
	hoverAmounts  [hoverRegionCount]float32
	pressAmounts  [hoverRegionCount]float32
	hoveredRegion = hoverNone
	pressedRegion = hoverNone
)

func regionAt(x, y int32) int {
	switch {
	case contains(spatial.theme, x, y):
		return hoverTheme
	case contains(spatial.language, x, y):
		return hoverLanguage
	case contains(spatial.aiLight, x, y):
		return hoverAILight
	case contains(spatial.aiHeavy, x, y):
		return hoverAIHeavy
	case contains(spatial.advanced, x, y):
		return hoverAdvanced
	case contains(spatial.model, x, y):
		return hoverModel
	case contains(spatial.printer, x, y):
		return hoverPrinter
	case contains(spatial.filament, x, y):
		return hoverFilament
	case contains(spatial.fast, x, y):
		return hoverFast
	case contains(spatial.balanced, x, y):
		return hoverBalanced
	case contains(spatial.perfect, x, y):
		return hoverPerfect
	case contains(spatial.open, x, y):
		return hoverOpen
	case contains(spatial.repoLink, x, y):
		return hoverRepoLink
	}
	for i, r := range spatial.tools {
		if contains(r, x, y) {
			return hoverTool1 + i
		}
	}
	for i, r := range spatial.nav {
		if contains(r, x, y) {
			return hoverNav1 + i
		}
	}
	if contains(spatial.stage, x, y) {
		return hoverStage
	}
	return hoverNone
}

// Polling the cursor each tick means the hover state clears correctly when the
// pointer leaves the window, without a WM_MOUSELEAVE subscription.
func refreshHoverFromCursor(hwnd uintptr) {
	var pt point
	if ok, _, _ := pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt))); ok == 0 {
		hoveredRegion = hoverNone
		return
	}
	pScreenToClient.Call(hwnd, uintptr(unsafe.Pointer(&pt)))
	var client rect
	pGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&client)))
	if !contains(client, pt.X, pt.Y) {
		hoveredRegion = hoverNone
		return
	}
	hoveredRegion = regionAt(pt.X, pt.Y)
}

func approach(current, target, rate float32) float32 {
	if current < target {
		current += rate
		if current > target {
			return target
		}
		return current
	}
	current -= rate
	if current < target {
		return target
	}
	return current
}

// Rounded surfaces are cached per colour and per size, so a continuously
// varying ease would mint a fresh bitmap every frame. Snapping the eased value
// to a handful of steps keeps the cache — and the cost of a frame — bounded,
// and the steps are close enough together to still read as motion.
const hoverSteps = 5

func quantizeHover(value float32) float32 {
	return float32(int32(value*hoverSteps+0.5)) / hoverSteps
}

// Easing rates, in fraction of remaining distance per second. Expressed as
// rates rather than per-frame steps so the feel is identical whether the window
// is painting at 60 fps or struggling at 12.
const (
	hoverRatePerSecond = 14.0
	pressRatePerSecond = 30.0
)

// animateHover advances every amount by real elapsed time and reports whether a
// repaint is needed. Only a change that survives quantization is worth one.
func animateHover(dt float64) bool {
	changed := false
	for id := 0; id < hoverRegionCount; id++ {
		hoverTarget := float64(0)
		if id == hoveredRegion {
			hoverTarget = 1
		}
		pressTarget := float64(0)
		if id == pressedRegion {
			pressTarget = 1
		}
		if next := float32(approachRate(float64(hoverAmounts[id]), hoverTarget, hoverRatePerSecond, dt)); next != hoverAmounts[id] {
			if quantizeHover(next) != quantizeHover(hoverAmounts[id]) {
				changed = true
			}
			hoverAmounts[id] = next
		}
		if next := float32(approachRate(float64(pressAmounts[id]), pressTarget, pressRatePerSecond, dt)); next != pressAmounts[id] {
			if quantizeHover(next) != quantizeHover(pressAmounts[id]) {
				changed = true
			}
			pressAmounts[id] = next
		}
	}
	return changed
}

// hoverSettled reports whether every track has reached its target, so the
// scheduler can stop asking for frames instead of easing forever.
func hoverSettled() bool {
	for id := 0; id < hoverRegionCount; id++ {
		want := float32(0)
		if id == hoveredRegion {
			want = 1
		}
		if hoverAmounts[id] != want {
			return false
		}
		press := float32(0)
		if id == pressedRegion {
			press = 1
		}
		if pressAmounts[id] != press {
			return false
		}
	}
	return true
}

func hoverOf(id int) float32 {
	if id <= hoverNone || id >= hoverRegionCount {
		return 0
	}
	return quantizeHover(hoverAmounts[id])
}

func pressOf(id int) float32 {
	if id <= hoverNone || id >= hoverRegionCount {
		return 0
	}
	return quantizeHover(pressAmounts[id])
}

// lift grows a control under the pointer and pushes it back in on press.
func lift(r rect, id int) rect {
	grow := int32(hoverOf(id)*2.5 - pressOf(id)*3.5)
	if grow == 0 {
		return r
	}
	return rect{r.Left - grow, r.Top - grow, r.Right + grow, r.Bottom + grow}
}

func hoverShadow(id int, base uint8) uint8 {
	boost := float32(base) * (1 + hoverOf(id)*0.75 - pressOf(id)*0.45)
	if boost > 255 {
		boost = 255
	}
	if boost < 0 {
		return 0
	}
	return uint8(boost)
}

func mixColor(a, b uintptr, t float32) uintptr {
	if t <= 0 {
		return a
	}
	if t >= 1 {
		return b
	}
	ar, ag, ab := spatialColorChannels(a)
	br, bg, bb := spatialColorChannels(b)
	return rgb(
		uint8(float32(ar)+(float32(br)-float32(ar))*t),
		uint8(float32(ag)+(float32(bg)-float32(ag))*t),
		uint8(float32(ab)+(float32(bb)-float32(ab))*t),
	)
}

// A themed surface that warms toward the accent tint as the pointer arrives.
func hoverCard(hdc uintptr, r rect, radius int32, id int) {
	amount := hoverOf(id) * 0.85
	drawSpatialRoundedMaterial(hdc, r, radius,
		mixColor(th.surface, th.accentTintA, amount),
		mixColor(th.surfaceAlt, th.accentTintB, amount),
		mixColor(th.stroke, th.accentStroke, hoverOf(id)))
}

func hoverSunken(hdc uintptr, r rect, radius int32, id int) {
	amount := hoverOf(id) * 0.9
	drawSpatialRoundedMaterial(hdc, r, radius,
		mixColor(th.sunken, th.accentTintA, amount),
		mixColor(th.sunkenAlt, th.accentTintB, amount),
		mixColor(th.strokeSoft, th.accentStroke, hoverOf(id)))
}
