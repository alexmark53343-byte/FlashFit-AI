package shared

import (
	"errors"
	"fmt"
	"math"
	"strings"
)

type qualityPreset struct {
	Label                                           string
	Layer, Outer, Inner, Infill, Top, Small, Bridge float64
	OuterAccel, InnerAccel, TopAccel                float64
	Walls, TopLayers, BottomLayers                  int
	InfillPct                                       int
	Relative                                        float64
}

// The presets deliberately tune the parameters that are safe to make portable.
// Printer G-code, retraction, motion limits and vendor-specific calibration stay
// inherited from the installed Flash Studio profiles.
var presets = map[string]qualityPreset{
	"low":      {Label: "Veloce", Layer: 0.24, Outer: 72, Inner: 108, Infill: 138, Top: 56, Small: 30, Bridge: 28, OuterAccel: 2100, InnerAccel: 3900, TopAccel: 1600, Walls: 3, TopLayers: 4, BottomLayers: 4, InfillPct: 12, Relative: 0.68},
	"balanced": {Label: "Bilanciata", Layer: 0.20, Outer: 55, Inner: 82, Infill: 105, Top: 45, Small: 27, Bridge: 26, OuterAccel: 1500, InnerAccel: 3000, TopAccel: 1200, Walls: 3, TopLayers: 5, BottomLayers: 4, InfillPct: 15, Relative: 1.0},
	"perfect":  {Label: "Perfetta", Layer: 0.14, Outer: 34, Inner: 52, Infill: 68, Top: 28, Small: 18, Bridge: 20, OuterAccel: 850, InnerAccel: 1700, TopAccel: 700, Walls: 4, TopLayers: 7, BottomLayers: 6, InfillPct: 18, Relative: 1.52},
}

type texturePreset struct {
	Label, TopPattern, IroningType, IroningPattern string
	FuzzySkin                                      string
	FuzzyThickness, FuzzyDistance                  float64
}

var perfectTextures = map[string]texturePreset{
	"satin":       {Label: "Satin Pro", TopPattern: "monotonicline", IroningType: "topmost", IroningPattern: "rectilinear", FuzzySkin: "none"},
	"prism":       {Label: "Crystal Prism", TopPattern: "octagramspiral", IroningType: "no ironing", IroningPattern: "rectilinear", FuzzySkin: "none"},
	"carbon":      {Label: "Carbon Weave", TopPattern: "hilbertcurve", IroningType: "no ironing", IroningPattern: "rectilinear", FuzzySkin: "external", FuzzyThickness: 0.10, FuzzyDistance: 0.18},
	"topographic": {Label: "Topographic Flow", TopPattern: "archimedeanchords", IroningType: "no ironing", IroningPattern: "concentric", FuzzySkin: "none"},
}

// Recommend preserves the public engine API and uses the smooth premium finish
// when Perfect is requested without an explicit texture.
func Recommend(a ModelAnalysis, f Filament, quality string) (Recommendation, error) {
	return RecommendForPrinterWithTexture(a, f, DefaultPrinterProfile(), quality, "")
}

func RecommendWithTexture(a ModelAnalysis, f Filament, quality, texture string) (Recommendation, error) {
	return RecommendForPrinterWithTexture(a, f, DefaultPrinterProfile(), quality, texture)
}

func RecommendForPrinter(a ModelAnalysis, f Filament, printer PrinterProfile, quality string) (Recommendation, error) {
	return RecommendForPrinterWithTexture(a, f, printer, quality, "")
}

func RecommendForPrinterWithTexture(a ModelAnalysis, f Filament, printer PrinterProfile, quality, texture string) (Recommendation, error) {
	if err := ValidateAnalysis(a); err != nil {
		return Recommendation{}, err
	}
	if err := ValidateFilament(f); err != nil {
		return Recommendation{}, err
	}
	if strings.TrimSpace(printer.ID) == "" {
		return Recommendation{}, errors.New("profilo stampante non risolto")
	}
	if err := ValidateModelForPrinter(a, printer); err != nil {
		return Recommendation{}, err
	}
	p, ok := presets[quality]
	if !ok {
		return Recommendation{}, errors.New("qualità sconosciuta")
	}
	m := strings.ToUpper(strings.TrimSpace(f.Material))
	isPETG := strings.HasPrefix(m, "PETG")
	isSilk := strings.Contains(strings.ToUpper(f.Product+" "+f.Variant), "SILK")
	estimatedBalancedMinutes := EstimateBalancedMinutes(a, f)
	durationClass := durationClassForMinutes(estimatedBalancedMinutes)
	p = adaptPresetForDuration(p, quality, durationClass, a)
	applyPrinterMotionGuardrails(&p, printer, a)

	// A producer MVS is an upper bound, not a quality target. Every mode keeps a
	// filament-aware margin and all requested speeds are volume-capped below it.
	safety := 0.74
	if quality == "balanced" {
		safety = 0.70
	} else if quality == "perfect" {
		safety = 0.64
	}
	if isPETG || isSilk {
		safety -= 0.05
	}
	safeMVS := f.MaxVolumetricSpeed * safety
	linearLimit := math.Min(300, printer.MaxTravelSpeed)
	if f.RecommendedSpeedMax > 0 {
		linearLimit = math.Min(linearLimit, f.RecommendedSpeedMax)
	}
	if isSilk {
		// Both Flashforge and Bambu document loss of gloss at high speed.
		linearLimit = math.Min(linearLimit, 80)
		if quality == "perfect" {
			linearLimit = math.Min(linearLimit, 60)
		}
	}
	p = applyAdvisor(p, a, quality, printer)
	capSpeed := func(request, width float64) float64 {
		cap := safeMVS / (p.Layer * width)
		return math.Max(10, math.Min(request, math.Min(linearLimit, math.Floor(cap))))
	}
	outer := capSpeed(p.Outer, 0.42)
	inner := capSpeed(p.Inner, 0.45)
	infill := capSpeed(p.Infill, 0.45)
	top := capSpeed(p.Top, 0.42)
	bridge := capSpeed(p.Bridge, 0.42)
	small := math.Min(p.Small, outer)
	if a.Category == "Miniatura dettagliata" || a.Category == "Superficie complessa" {
		outer = math.Min(outer, 40)
		small = math.Min(small, 21)
		p.OuterAccel = math.Min(p.OuterAccel, 1050)
	}
	if a.ThinOrTall {
		outer = math.Min(outer, 40)
		inner = math.Min(inner, 60)
		p.OuterAccel = math.Min(p.OuterAccel, 950)
		p.InnerAccel = math.Min(p.InnerAccel, 1750)
	}
	if isPETG {
		bridge = math.Min(bridge, 21)
		small = math.Min(small, 24)
		p.OuterAccel = math.Min(p.OuterAccel, 1350)
	}

	if f.NozzleDefault > printer.MaxNozzleTemperature {
		return Recommendation{}, fmt.Errorf("%s richiede %.0f °C ma %s è limitata a %.0f °C", f.Product, f.NozzleDefault, printer.Model, printer.MaxNozzleTemperature)
	}
	if f.BedDefault > printer.MaxBedTemperature {
		return Recommendation{}, fmt.Errorf("%s richiede piano a %.0f °C ma %s è limitata a %.0f °C", f.Product, f.BedDefault, printer.Model, printer.MaxBedTemperature)
	}
	nozzle, bed := math.Min(f.NozzleDefault, printer.MaxNozzleTemperature), math.Min(f.BedDefault, printer.MaxBedTemperature)
	// High-flow modes need a small, documented temperature margin. Never exceed
	// the TDS range and do not heat short prints just to save a negligible time.
	if quality == "low" && durationClass != "short" {
		nozzle = math.Min(printer.MaxNozzleTemperature, math.Min(f.NozzleMax, nozzle+5))
	}
	flow := f.FlowRatio
	if flow == 0 {
		flow = 1
	}

	topThickness, bottomThickness := 0.80, 0.80
	verticalShell := "ensure_critical_only"
	seamGap := "10%"
	if quality == "balanced" {
		topThickness = 1.00
		verticalShell = "ensure_moderate"
	}
	if quality == "perfect" {
		topThickness = 1.00
		verticalShell = "ensure_all"
		seamGap = "5%"
	}
	initialSpeed := 30.0
	if isPETG {
		initialSpeed = 25
	}
	solidSpeed := capSpeed(math.Min(inner, top+12), 0.45)
	gapSpeed := math.Min(small, 24)
	supportSpeed := capSpeed(math.Min(50, inner), 0.45)
	supportInterfaceSpeed := capSpeed(math.Min(28, top), 0.42)
	bridgeFlow := 0.95
	if isPETG {
		bridgeFlow = 0.90
	}

	process := map[string]any{
		"layer_height": fmt2(p.Layer), "initial_layer_print_height": fmt2(math.Min(0.24, p.Layer+0.04)),
		"line_width": "0.45", "outer_wall_line_width": "0.42", "inner_wall_line_width": "0.45", "sparse_infill_line_width": "0.45",
		"wall_loops": fmt.Sprintf("%d", p.Walls), "top_shell_layers": fmt.Sprintf("%d", p.TopLayers), "bottom_shell_layers": fmt.Sprintf("%d", p.BottomLayers),
		"top_shell_thickness": fmt2(topThickness), "bottom_shell_thickness": fmt2(bottomThickness),
		"sparse_infill_density": fmt.Sprintf("%d%%", p.InfillPct), "sparse_infill_pattern": "gyroid",
		"top_surface_pattern": "monotonicline", "bottom_surface_pattern": "monotonic", "top_surface_density": "100%", "bottom_surface_density": "100%",
		"wall_generator": "arachne", "precise_outer_wall": "1", "only_one_wall_top": "0", "ensure_vertical_shell_thickness": verticalShell,
		"infill_wall_overlap": "12%", "top_bottom_infill_wall_overlap": "15%", "seam_position": "aligned_back", "staggered_inner_seams": "1", "seam_gap": seamGap,
		"outer_wall_speed": fmt0(outer), "inner_wall_speed": fmt0(inner), "sparse_infill_speed": fmt0(infill), "internal_solid_infill_speed": fmt0(solidSpeed),
		"top_surface_speed": fmt0(top), "small_perimeter_speed": fmt0(small), "gap_infill_speed": fmt0(gapSpeed), "bridge_speed": fmt0(bridge),
		"support_speed": fmt0(supportSpeed), "support_interface_speed": fmt0(supportInterfaceSpeed), "initial_layer_speed": fmt0(initialSpeed), "initial_layer_infill_speed": fmt0(initialSpeed + 5),
		"outer_wall_acceleration": fmt0(p.OuterAccel), "inner_wall_acceleration": fmt0(p.InnerAccel), "sparse_infill_acceleration": fmt0(p.InnerAccel),
		"internal_solid_infill_acceleration": fmt0(p.TopAccel), "top_surface_acceleration": fmt0(p.TopAccel), "bridge_acceleration": fmt0(p.OuterAccel), "initial_layer_acceleration": "500",
		"travel_acceleration": fmt0(math.Min(5000, printer.MaxAcceleration*0.45)), "travel_speed": fmt0(math.Min(350, printer.MaxTravelSpeed*0.75)), "bridge_flow": fmt2(bridgeFlow),
		"avoid_crossing_wall": "1", "reduce_infill_retraction": "0", "overhang_1_4_speed": "0", "overhang_2_4_speed": fmt0(math.Min(outer, 42)), "overhang_3_4_speed": fmt0(math.Min(bridge+8, 34)), "overhang_4_4_speed": fmt0(math.Min(bridge, 24)),
		"enable_support": bool01(a.SupportSuggested), "support_type": supportType(a), "support_threshold_angle": "45", "brim_type": brimType(a), "brim_width": brimWidth(a),
		// Supports that will not come off ruin the surface they were protecting,
		// which matters most on the tier chosen for looks. These leave a
		// deliberate gap under and around them and space the interface out, so
		// they peel rather than weld.
		"support_top_z_distance":     supportTopGap(quality, p.Layer),
		"support_bottom_z_distance":  supportTopGap(quality, p.Layer),
		"support_object_xy_distance": supportSideGap(quality),
		"support_interface_spacing":  supportInterfaceSpacing(quality),
		"support_interface_loop_pattern": "0",
		"ironing_type": "no ironing", "ironing_pattern": "rectilinear", "ironing_flow": "10%", "ironing_spacing": "0.1", "ironing_inset": "0.12", "ironing_speed": "24",
		"fuzzy_skin": "none", "fuzzy_skin_thickness": "0", "fuzzy_skin_point_distance": "0.3", "fuzzy_skin_first_layer": "0",
	}

	textureID, textureLabel := "none", "Standard"
	relativeTime := p.Relative
	if quality == "perfect" {
		textureID = strings.ToLower(strings.TrimSpace(texture))
		if textureID == "" {
			textureID = "satin"
		}
		t, exists := perfectTextures[textureID]
		if !exists {
			return Recommendation{}, errors.New("texture premium sconosciuta")
		}
		textureLabel = t.Label
		process["top_surface_pattern"] = t.TopPattern
		process["ironing_type"] = t.IroningType
		process["ironing_pattern"] = t.IroningPattern
		process["fuzzy_skin"] = t.FuzzySkin
		if t.FuzzySkin == "external" {
			thickness := t.FuzzyThickness
			if isPETG {
				thickness = math.Min(thickness, 0.08)
			}
			process["fuzzy_skin_thickness"] = fmt2(thickness)
			process["fuzzy_skin_point_distance"] = fmt2(t.FuzzyDistance)
		}
		if t.IroningType != "no ironing" {
			flowPct := 10
			ironingSpeed := 22.0
			if isPETG {
				flowPct, ironingSpeed = 8, 18
			}
			process["ironing_flow"] = fmt.Sprintf("%d%%", flowPct)
			process["ironing_speed"] = fmt0(ironingSpeed)
			relativeTime += 0.10
		} else if textureID == "carbon" {
			relativeTime += 0.05
		} else {
			relativeTime += 0.03
		}
	}

	closedFanLayers, fullFanLayer, slowLayerTime, minPrintSpeed := 1, 3, 8, 15
	if isPETG {
		closedFanLayers, fullFanLayer, slowLayerTime = 3, 5, 10
	}
	if isSilk {
		minPrintSpeed = 12
	}
	fil := map[string]any{
		"filament_density": fmt2(f.Density), "filament_flow_ratio": fmt3(flow), "filament_max_volumetric_speed": fmt2(safeMVS),
		"nozzle_temperature": fmt0(nozzle), "nozzle_temperature_initial_layer": fmt0(math.Min(printer.MaxNozzleTemperature, math.Min(f.NozzleMax, nozzle+5))),
		"hot_plate_temp": fmt0(bed), "hot_plate_temp_initial_layer": fmt0(math.Min(printer.MaxBedTemperature, math.Min(f.BedMax, bed+5))),
		"textured_plate_temp": fmt0(bed), "textured_plate_temp_initial_layer": fmt0(math.Min(printer.MaxBedTemperature, math.Min(f.BedMax, bed+5))),
		"fan_min_speed": fmt0(f.FanMin), "fan_max_speed": fmt0(f.FanMax),
		"close_fan_the_first_x_layers": fmt.Sprintf("%d", closedFanLayers), "full_fan_speed_layer": fmt.Sprintf("%d", fullFanLayer),
		"slow_down_for_layer_cooling": "1", "slow_down_layer_time": fmt.Sprintf("%d", slowLayerTime), "min_print_speed": fmt.Sprintf("%d", minPrintSpeed),
	}
	if f.PressureAdvance != nil {
		fil["enable_pressure_advance"] = "1"
		fil["pressure_advance"] = fmt3(*f.PressureAdvance)
	}

	reasons := []string{
		fmt.Sprintf("Profilo macchina %s %s verificato; G-code, cinematica, retrazione e calibrazioni vendor restano invariati.", printer.Brand, printer.Model),
		fmt.Sprintf("Parete esterna limitata a %.0f mm/s per ridurre ghosting e artefatti.", outer),
		fmt.Sprintf("Portata limitata al %.0f%% della MVS dichiarata: %.1f su %.1f mm³/s.", safety*100, safeMVS, f.MaxVolumetricSpeed),
		fmt.Sprintf("%d pareti, guscio superiore minimo %.1f mm e infill gyroid al %d%% per una resistenza equilibrata.", p.Walls, topThickness, p.InfillPct),
		"Arachne, parete precisa e cuciture interne sfalsate proteggono dettagli e continuità dei gusci.",
		durationReason(durationClass, estimatedBalancedMinutes, relativeTime),
	}
	if f.RecommendedSpeedMax > 0 {
		reasons = append(reasons, fmt.Sprintf("Velocità lineare limitata a %.0f mm/s come tetto della scheda tecnica; la MVS resta il limite dominante.", linearLimit))
	}
	if f.DryTemperature > 0 && f.DryHours > 0 {
		reasons = append(reasons, fmt.Sprintf("Scheda tecnica: se la bobina ha assorbito umidità, essiccare a %.0f °C per %.0f h prima della calibrazione.", f.DryTemperature, f.DryHours))
	}
	if quality == "perfect" {
		reasons = append(reasons, fmt.Sprintf("Finitura Ultra Premium %s applicata come percorso reale dello slicer.", textureLabel))
	}
	if a.SupportSuggested {
		reasons = append(reasons, "Supporti automatici attivati perché la geometria mostra sbalzi marcati.")
	}
	if a.BrimSuggested {
		reasons = append(reasons, "Brim da 5 mm attivato per impronta piccola o modello alto.")
	}
	warnings := append([]string(nil), a.Warnings...)
	if printer.PLAEnclosureHeatRisk && strings.HasPrefix(m, "PLA") {
		warnings = append(warnings, "Su questa stampante chiusa il PLA può soffrire heat-creep nelle stampe lunghe: usa la ventilazione/apertura prevista dal manuale e controlla la temperatura camera.")
	}
	if printer.Motion == "bedslinger" && a.ThinOrTall {
		warnings = append(warnings, "Modello alto su bed-slinger: accelerazioni esterne ridotte per limitare oscillazioni, ringing e distacco dal piano.")
	}
	if printer.PreserveToolchange {
		reasons = append(reasons, "Cambio utensile/filamento, spurgo, wipe e torre di pulizia restano quelli del profilo ufficiale installato.")
	}
	if f.PressureAdvance == nil {
		reasons = append(reasons, "Flow Dynamics/Pressure Advance non viene sovrascritto: resta il valore calibrato nello slicer o sulla stampante.")
	}
	critical := map[string]float64{
		"layer_height": p.Layer, "outer_wall_speed": outer, "inner_wall_speed": inner, "infill_speed": infill, "bridge_speed": bridge,
		"outer_acceleration": p.OuterAccel, "max_volumetric_speed": safeMVS, "nozzle_temperature": nozzle, "bed_temperature": bed,
	}
	criticalSettings := map[string]string{
		"wall_generator": "arachne", "wall_loops": fmt.Sprintf("%d", p.Walls), "top_shell_layers": fmt.Sprintf("%d", p.TopLayers),
		"sparse_infill_density": fmt.Sprintf("%d%%", p.InfillPct), "top_surface_pattern": fmt.Sprint(process["top_surface_pattern"]),
		"ironing_type": fmt.Sprint(process["ironing_type"]), "fuzzy_skin": fmt.Sprint(process["fuzzy_skin"]),
	}
	return Recommendation{
		PrinterID: printer.ID, PrinterLabel: printer.Brand + " " + printer.Model,
		Quality: quality, QualityLabel: p.Label, Texture: textureID, TextureLabel: textureLabel,
		Process: process, Filament: fil, Reasons: reasons, Warnings: warnings,
		EstimatedRelativeTime: relativeTime, EstimatedBalancedMinutes: estimatedBalancedMinutes,
		EstimatedModeMinutes: estimatedBalancedMinutes * relativeTime, DurationClass: durationClass,
		CriticalValues: critical, CriticalSettings: criticalSettings,
	}, nil
}

// The advisor is deliberately split in two halves: a proposer that may suggest
// anything, and a veto that decides whether the suggestion is allowed to stand.
// Only the veto is trusted. That is what makes "it must never make things
// worse" a property of the architecture rather than a promise about the
// proposer's quality — and it is the seat any future model plugs into, without
// gaining the power to produce an unsafe or slower profile.
//
// A proposal is rejected when it would:
//   - push shell or density outside the hand-checked envelope, or
//   - cost more time than the quality tier is allowed to spend.
func applyAdvisor(base qualityPreset, a ModelAnalysis, quality string, printer PrinterProfile) qualityPreset {
	LastAdvisorOutcome = AdvisorOutcome{}
	// The model is consulted off the caller's thread. A cached answer is used
	// straight away; a miss starts a request and falls through to the rules, so
	// this function never waits on the network.
	if deltas, ok := lookupOrRequestAdvice(advisorConfigFromEnv(), a, quality, base); ok {
		LastAdvisorOutcome = AdvisorOutcome{Used: true, Object: deltas.Object, Reason: deltas.Reason}
		if proposal, scaled, accepted := admitAdvice(base, deltas, quality); accepted {
			LastAdvisorOutcome.Accepted = true
			LastAdvisorOutcome.Scaled = scaled
			return proposal
		}
		// Unsafe advice gets no second chance at the profile: fall back to the
		// rules, which face the same veto below.
		LastAdvisorOutcome.Detail = advisorRejectionReason(base, applyAdvisorDeltas(base, deltas), quality)
	}
	proposal := base
	tuneForCategory(&proposal, a, quality)
	if !advisorProposalAllowed(base, proposal, quality) {
		return base
	}
	return proposal
}

// admitAdvice decides what, if anything, of the model's advice survives.
//
// The veto separates two very different failures. Breaking a *safety* rule —
// changing the layer height, speeding the machine up, leaving the envelope —
// is disqualifying: that advice is thrown away whole, because a half-applied
// unsafe suggestion is its own kind of wrong.
//
// Merely costing too much time is not a safety failure. There the model's
// direction is fine and only its magnitude is too bold, so the deltas are
// scaled back until they fit the tier's budget. Discarding those outright was
// wasteful: the user saw "advice rejected" and got nothing, when a gentler
// version of the same advice was perfectly acceptable.
//
// Returns the preset to use, whether it had to be toned down, and whether any
// of it was admitted at all.
func admitAdvice(base qualityPreset, deltas advisorDeltas, quality string) (qualityPreset, bool, bool) {
	full := applyAdvisorDeltas(base, deltas)
	if !advisorProposalSafe(base, full) {
		return base, false, false
	}
	if advisorWithinBudget(base, full, quality) {
		return full, false, true
	}
	trimmed, changed := trimToBudget(base, full, quality)
	if changed && advisorProposalSafe(base, trimmed) && advisorWithinBudget(base, trimmed, quality) {
		return trimmed, true, true
	}
	// Nothing of it fits: the base preset already spends what this tier allows.
	return base, false, false
}

// trimToBudget gives back the most of the advice that the tier can afford.
//
// Scaling every delta by the same factor does not work: rounding keeps each one
// at a minimum of a whole wall or layer, so a bold proposal stays over budget
// even at a quarter strength. Instead the single most expensive increase is
// given up at a time, which always converges and keeps whatever was cheap.
func trimToBudget(base, proposal qualityPreset, quality string) (qualityPreset, bool) {
	changed := false
	for i := 0; i < 64 && !advisorWithinBudget(base, proposal, quality); i++ {
		// Cost per unit, matching presetWorkIndex.
		type lever struct {
			weight float64
			give   func()
		}
		levers := []lever{
			{1.6, func() { proposal.Walls-- }},
			{0.6, func() { proposal.TopLayers-- }},
			{0.4, func() { proposal.BottomLayers-- }},
			{0.35, func() { proposal.InfillPct-- }},
		}
		above := []bool{
			proposal.Walls > base.Walls,
			proposal.TopLayers > base.TopLayers,
			proposal.BottomLayers > base.BottomLayers,
			proposal.InfillPct > base.InfillPct,
		}
		best := -1
		for index := range levers {
			if !above[index] {
				continue
			}
			if best < 0 || levers[index].weight > levers[best].weight {
				best = index
			}
		}
		if best < 0 {
			// Nothing left to give back: the proposal is at or below base and
			// still over budget, which means base itself is the ceiling.
			return base, false
		}
		levers[best].give()
		changed = true
	}
	return proposal, changed
}

// advisorProposalSafe covers the rules that are never negotiable.
func advisorProposalSafe(base, proposal qualityPreset) bool {
	switch {
	case proposal.Layer != base.Layer:
		return false
	case proposal.Walls < 2 || proposal.Walls > 6:
		return false
	case proposal.TopLayers < 3 || proposal.TopLayers > 10:
		return false
	case proposal.BottomLayers < 3 || proposal.BottomLayers > 10:
		return false
	case proposal.InfillPct < 8 || proposal.InfillPct > 40:
		return false
	case proposal.Outer > base.Outer || proposal.Inner > base.Inner || proposal.Bridge > base.Bridge:
		return false
	case proposal.OuterAccel > base.OuterAccel || proposal.InnerAccel > base.InnerAccel:
		return false
	}
	return true
}

func advisorWithinBudget(base, proposal qualityPreset, quality string) bool {
	baseWork := presetWorkIndex(base)
	if baseWork <= 0 {
		return false
	}
	return presetWorkIndex(proposal)/baseWork <= advisorTimeBudget(quality)
}

// advisorRejectionReason names the rule a proposal broke, so the interface can
// say why the model's advice was refused rather than only that it was.
func advisorRejectionReason(base, proposal qualityPreset, quality string) string {
	switch {
	case proposal.Layer != base.Layer:
		return "altezza layer"
	case proposal.Walls < 2 || proposal.Walls > 6:
		return "pareti fuori limite"
	case proposal.TopLayers < 3 || proposal.TopLayers > 10 || proposal.BottomLayers < 3 || proposal.BottomLayers > 10:
		return "gusci fuori limite"
	case proposal.InfillPct < 8 || proposal.InfillPct > 40:
		return "infill fuori limite"
	case proposal.Outer > base.Outer || proposal.Inner > base.Inner || proposal.Bridge > base.Bridge:
		return "velocità aumentata"
	case proposal.OuterAccel > base.OuterAccel || proposal.InnerAccel > base.InnerAccel:
		return "accelerazione aumentata"
	default:
		return "oltre il budget tempo"
	}
}

// advisorTimeBudget is how much extra shell/density time a tier may accept from
// the advisor. Fast prints exist to be fast, so their budget is the tightest.
func advisorTimeBudget(quality string) float64 {
	switch quality {
	case "low":
		return 1.06
	case "perfect":
		return 1.20
	default:
		return 1.12
	}
}

// presetWorkIndex is a monotone stand-in for how long a preset takes: extra
// walls, solid layers and density all add material at a similar cost, so their
// weighted sum orders two presets the same way a real estimate would.
func presetWorkIndex(p qualityPreset) float64 {
	return float64(p.Walls)*1.6 + float64(p.TopLayers)*0.6 + float64(p.BottomLayers)*0.4 + float64(p.InfillPct)*0.35
}

// advisorProposalAllowed is the full check: safe and affordable.
func advisorProposalAllowed(base, proposal qualityPreset, quality string) bool {
	return advisorProposalSafe(base, proposal) && advisorWithinBudget(base, proposal, quality)
}

// tuneForCategory turns the recognised object type into actual settings. The
// quality preset decides how fine the print is; the category decides what the
// part needs — a large shell wastes hours on infill it will never use, while a
// miniature needs shell and slow walls far more than it needs density.
//
// Adjustments are deliberately bounded and never touch layer height, so the
// chosen quality still governs time and finish.
func tuneForCategory(p *qualityPreset, a ModelAnalysis, quality string) {
	clampInt := func(value, low, high int) int {
		if value < low {
			return low
		}
		if value > high {
			return high
		}
		return value
	}
	switch a.Category {
	case "Miniatura dettagliata":
		// Detail lives in the shell. Buy top layers, spend nothing on density.
		p.Walls = clampInt(p.Walls+1, 2, 6)
		p.TopLayers = clampInt(p.TopLayers+1, 3, 10)
		p.InfillPct = clampInt(p.InfillPct-4, 8, 40)

	case "Forma alta o sottile":
		// Tall parts fail by wobbling or snapping, not by lacking infill.
		p.Walls = clampInt(p.Walls+1, 2, 6)
		p.BottomLayers = clampInt(p.BottomLayers+1, 3, 10)
		p.InfillPct = clampInt(p.InfillPct+3, 8, 40)
		p.Outer = math.Min(p.Outer, 45)
		p.OuterAccel = math.Min(p.OuterAccel, 1200)

	case "Geometria con molti sbalzi":
		// Overhangs are bridged, so slow the bridge and thicken what sits on it.
		p.Bridge = math.Min(p.Bridge, 22)
		p.BottomLayers = clampInt(p.BottomLayers+1, 3, 10)
		p.InfillPct = clampInt(p.InfillPct+2, 8, 40)

	case "Oggetto grande":
		// The main lever on time. A big shell needs walls, not density; dropping
		// infill is what keeps a large part from turning into a day-long job.
		p.InfillPct = clampInt(p.InfillPct-4, 8, 40)
		p.Walls = clampInt(p.Walls+1, 2, 6)
		if quality != "perfect" {
			p.TopLayers = clampInt(p.TopLayers-1, 3, 10)
		}

	case "Superficie complessa":
		p.TopLayers = clampInt(p.TopLayers+1, 3, 10)
		p.Outer = math.Min(p.Outer, 42)
		p.OuterAccel = math.Min(p.OuterAccel, 1100)

	default: // Oggetto tecnico/decorativo
		// Functional parts are the one case where density earns its time.
		p.InfillPct = clampInt(p.InfillPct+5, 8, 40)
		p.Walls = clampInt(p.Walls+1, 2, 6)
	}
}

func applyPrinterMotionGuardrails(p *qualityPreset, printer PrinterProfile, a ModelAnalysis) {
	if p == nil {
		return
	}
	p.OuterAccel = math.Min(p.OuterAccel, printer.MaxAcceleration*0.18)
	p.InnerAccel = math.Min(p.InnerAccel, printer.MaxAcceleration*0.32)
	p.TopAccel = math.Min(p.TopAccel, printer.MaxAcceleration*0.14)
	if printer.Motion == "bedslinger" {
		p.OuterAccel = math.Min(p.OuterAccel, 1500)
		p.InnerAccel = math.Min(p.InnerAccel, 2800)
		p.TopAccel = math.Min(p.TopAccel, 1100)
		p.Outer = math.Min(p.Outer, 65)
		p.Inner = math.Min(p.Inner, 95)
		if a.ThinOrTall || a.Extents[2] > printer.BuildVolume[2]*0.55 {
			p.OuterAccel = math.Min(p.OuterAccel, 750)
			p.InnerAccel = math.Min(p.InnerAccel, 1400)
			p.TopAccel = math.Min(p.TopAccel, 650)
			p.Outer = math.Min(p.Outer, 42)
			p.Inner = math.Min(p.Inner, 58)
		}
	}
	if printer.BuildVolume[2] >= 500 && (a.ThinOrTall || a.Extents[2] > 250) {
		p.OuterAccel = math.Min(p.OuterAccel, 800)
		p.InnerAccel = math.Min(p.InnerAccel, 1500)
		p.TopAccel = math.Min(p.TopAccel, 700)
		p.Outer = math.Min(p.Outer, 42)
	}
}

// EstimateBalancedMinutes is a conservative geometry estimate used only to
// decide how far the quality modes should diverge. Flash Studio remains the
// authority for the final sliced time shown to the user.
func EstimateBalancedMinutes(a ModelAnalysis, f Filament) float64 {
	area, volume := a.SurfaceArea, a.Volume
	if area <= 0 {
		x, y, z := a.Extents[0], a.Extents[1], a.Extents[2]
		area = 2 * (x*y + x*z + y*z)
	}
	if volume <= 0 {
		volume = a.Extents[0] * a.Extents[1] * a.Extents[2]
	}
	// Approximate material in walls/top-bottom plus 15% gyroid infill.
	extrudedMM3 := area*1.05 + volume*0.15

	// Two allowances used to be far too cautious, and together they made every
	// estimate about half again too long. Measured against a real slice — a
	// 911 at 0.4 mm on the Perfect tier: 20 h 14 m predicted, 13 h 14 m actual —
	// the flow derating and the overhead padding account for almost exactly the
	// gap, so both were brought closer to what a slicer really achieves.
	//
	// This is calibrated against one print. It is a large improvement on being
	// 53% out, not a claim of precision, and it should be re-checked as more
	// real times come in.
	const flowUtilisation = 0.47 // was 0.36: slicers hold closer to the limit
	const overheadFactor = 1.15  // was 1.35: travel and acceleration cost less

	effectiveFlow := math.Min(5.2, f.MaxVolumetricSpeed*flowUtilisation)
	effectiveFlow = math.Max(2.4, effectiveFlow)
	minutes := extrudedMM3/effectiveFlow/60*overheadFactor + math.Max(0, a.Extents[2])/0.20*0.025 + 4
	if a.SupportSuggested {
		minutes *= 1.12
	}
	if a.BrimSuggested {
		minutes += 2
	}
	return math.Max(8, minutes)
}

// Support release settings.
//
// A support that fuses to the part is worse than no support: removing it tears
// the surface it existed to protect. The gap has to scale with the layer height
// — a fixed distance that releases cleanly at 0.20 mm is barely a gap at 0.14 —
// and the finer the tier, the more the finish is worth protecting.
func supportTopGap(quality string, layer float64) string {
	multiplier := 1.0
	if quality == "perfect" {
		// Chosen for looks, so bias towards clean release over support quality.
		multiplier = 1.5
	}
	gap := layer * multiplier
	// Below about 0.15 mm the gap stops releasing; above 0.35 the support stops
	// supporting.
	return fmt2(math.Max(0.15, math.Min(0.35, gap)))
}

func supportSideGap(quality string) string {
	if quality == "perfect" {
		return "0.45"
	}
	return "0.35"
}

// Wider interface spacing means fewer welded contact points.
func supportInterfaceSpacing(quality string) string {
	if quality == "perfect" {
		return "0.35"
	}
	return "0.2"
}

func durationClassForMinutes(minutes float64) string {
	switch {
	case minutes <= 60:
		return "short"
	case minutes <= 180:
		return "medium"
	default:
		return "long"
	}
}

func adaptPresetForDuration(p qualityPreset, quality, durationClass string, a ModelAnalysis) qualityPreset {
	switch quality {
	case "low":
		switch durationClass {
		case "short":
			// Saving a few minutes is not worth giving up layer quality.
			p.Layer, p.Outer, p.Inner, p.Infill, p.Top = 0.20, 60, 88, 112, 48
			p.Small, p.Bridge = 27, 26
			p.OuterAccel, p.InnerAccel, p.TopAccel = 1650, 3200, 1300
			p.Walls, p.TopLayers, p.BottomLayers, p.InfillPct = 3, 5, 4, 15
			p.Relative = 0.96
		case "medium":
			p.Layer, p.Outer, p.Inner, p.Infill, p.Top = 0.22, 65, 96, 122, 51
			p.Small, p.Bridge = 29, 27
			p.OuterAccel, p.InnerAccel, p.TopAccel = 1850, 3550, 1450
			p.Walls, p.TopLayers, p.BottomLayers, p.InfillPct = 3, 4, 4, 14
			p.Relative = 0.84
		default:
			p.Relative = 0.72
		}
	case "perfect":
		switch durationClass {
		case "short":
			p.Layer, p.TopLayers, p.InfillPct, p.Relative = 0.14, 7, 18, 1.38
		case "medium":
			p.Layer, p.TopLayers, p.InfillPct, p.Relative = 0.14, 7, 18, 1.48
		default:
			// 0.16 keeps a multi-hour print premium without doubling its duration.
			p.Layer, p.Outer, p.Inner, p.Infill, p.Top = 0.16, 38, 58, 76, 32
			p.Small, p.Bridge = 20, 21
			p.TopLayers, p.BottomLayers, p.InfillPct, p.Relative = 7, 6, 18, 1.42
		}
		if durationClass == "short" && (a.Category == "Miniatura dettagliata" || a.Category == "Superficie complessa") {
			p.Layer = 0.12
			p.TopLayers = 8
			p.Relative = math.Max(p.Relative, 1.50)
		}
	}
	return p
}

func durationReason(class string, balancedMinutes, relative float64) string {
	modeMinutes := balancedMinutes * relative
	switch class {
	case "short":
		return fmt.Sprintf("Stampa breve stimata: Veloce conserva quasi tutta la qualità; riferimento %.0f min, modalità scelta circa %.0f min.", balancedMinutes, modeMinutes)
	case "medium":
		return fmt.Sprintf("Stampa media stimata: differenza tempo/qualità moderata; riferimento %.0f min, modalità scelta circa %.0f min.", balancedMinutes, modeMinutes)
	default:
		return fmt.Sprintf("Stampa lunga stimata: risparmio controllato e Perfetta limitata per evitare tempi estremi; riferimento %.0f min, modalità scelta circa %.0f min.", balancedMinutes, modeMinutes)
	}
}

func fmt0(v float64) string { return fmt.Sprintf("%.0f", v) }
func fmt2(v float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", v), "0"), ".")
}
func fmt3(v float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.3f", v), "0"), ".")
}
func bool01(v bool) string {
	if v {
		return "1"
	}
	return "0"
}
func supportType(a ModelAnalysis) string {
	if strings.Contains(a.Category, "organica") {
		return "tree(auto)"
	}
	return "normal(auto)"
}
func brimType(a ModelAnalysis) string {
	if a.BrimSuggested {
		return "outer_only"
	}
	return "no_brim"
}
func brimWidth(a ModelAnalysis) string {
	if a.BrimSuggested {
		return "5"
	}
	return "0"
}
