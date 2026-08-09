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
	"perfect":  {Label: "Perfetta", Layer: 0.12, Outer: 34, Inner: 52, Infill: 68, Top: 28, Small: 18, Bridge: 20, OuterAccel: 850, InnerAccel: 1700, TopAccel: 700, Walls: 4, TopLayers: 8, BottomLayers: 6, InfillPct: 20, Relative: 1.82},
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
	return RecommendWithTexture(a, f, quality, "")
}

func RecommendWithTexture(a ModelAnalysis, f Filament, quality, texture string) (Recommendation, error) {
	if err := ValidateAnalysis(a); err != nil {
		return Recommendation{}, err
	}
	if err := ValidateFilament(f); err != nil {
		return Recommendation{}, err
	}
	p, ok := presets[quality]
	if !ok {
		return Recommendation{}, errors.New("qualità sconosciuta")
	}
	m := strings.ToUpper(strings.TrimSpace(f.Material))
	isPETG := strings.HasPrefix(m, "PETG")
	isSilk := strings.Contains(strings.ToUpper(f.Product+" "+f.Variant), "SILK")

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
	capSpeed := func(request, width float64) float64 {
		cap := safeMVS / (p.Layer * width)
		return math.Max(10, math.Min(request, math.Floor(cap)))
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

	nozzle, bed := f.NozzleDefault, f.BedDefault
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
		"travel_acceleration": "5000", "travel_speed": "350", "bridge_flow": fmt2(bridgeFlow),
		"avoid_crossing_wall": "1", "reduce_infill_retraction": "0", "overhang_1_4_speed": "0", "overhang_2_4_speed": fmt0(math.Min(outer, 42)), "overhang_3_4_speed": fmt0(math.Min(bridge+8, 34)), "overhang_4_4_speed": fmt0(math.Min(bridge, 24)),
		"enable_support": bool01(a.SupportSuggested), "support_type": supportType(a), "support_threshold_angle": "45", "brim_type": brimType(a), "brim_width": brimWidth(a),
		"ironing_type": "no ironing", "ironing_pattern": "rectilinear", "ironing_flow": "10%", "ironing_spacing": "0.1", "ironing_inset": "0.12", "ironing_speed": "24",
		"fuzzy_skin": "none", "fuzzy_skin_thickness": "0", "fuzzy_skin_point_distance": "0.3", "fuzzy_skin_first_layer": "0",
	}

	textureID, textureLabel := "none", "Standard"
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
		}
	}

	fil := map[string]any{
		"filament_density": fmt2(f.Density), "filament_flow_ratio": fmt3(flow), "filament_max_volumetric_speed": fmt2(safeMVS),
		"nozzle_temperature": fmt0(nozzle), "nozzle_temperature_initial_layer": fmt0(math.Min(f.NozzleMax, nozzle+5)),
		"hot_plate_temp": fmt0(bed), "hot_plate_temp_initial_layer": fmt0(math.Min(f.BedMax, bed+5)),
		"textured_plate_temp": fmt0(bed), "textured_plate_temp_initial_layer": fmt0(math.Min(f.BedMax, bed+5)),
		"fan_min_speed": fmt0(f.FanMin), "fan_max_speed": fmt0(f.FanMax),
	}
	if f.PressureAdvance != nil {
		fil["enable_pressure_advance"] = "1"
		fil["pressure_advance"] = fmt3(*f.PressureAdvance)
	} else {
		fil["enable_pressure_advance"] = "0"
	}

	reasons := []string{
		fmt.Sprintf("Parete esterna limitata a %.0f mm/s per ridurre ghosting e artefatti.", outer),
		fmt.Sprintf("Portata limitata al %.0f%% della MVS dichiarata: %.1f su %.1f mm³/s.", safety*100, safeMVS, f.MaxVolumetricSpeed),
		fmt.Sprintf("%d pareti, guscio superiore minimo %.1f mm e infill gyroid al %d%% per una resistenza equilibrata.", p.Walls, topThickness, p.InfillPct),
		"Arachne, parete precisa e cuciture interne sfalsate proteggono dettagli e continuità dei gusci.",
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
		Quality: quality, QualityLabel: p.Label, Texture: textureID, TextureLabel: textureLabel,
		Process: process, Filament: fil, Reasons: reasons, Warnings: a.Warnings,
		EstimatedRelativeTime: p.Relative, CriticalValues: critical, CriticalSettings: criticalSettings,
	}, nil
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
