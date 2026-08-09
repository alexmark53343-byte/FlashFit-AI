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

var presets = map[string]qualityPreset{
	"low":      {Label: "Bassa qualità", Layer: 0.28, Outer: 78, Inner: 115, Infill: 145, Top: 65, Small: 35, Bridge: 30, OuterAccel: 2200, InnerAccel: 4200, TopAccel: 1800, Walls: 2, TopLayers: 4, BottomLayers: 3, InfillPct: 12, Relative: 0.62},
	"balanced": {Label: "Bilanciata", Layer: 0.20, Outer: 55, Inner: 82, Infill: 105, Top: 45, Small: 27, Bridge: 26, OuterAccel: 1500, InnerAccel: 3000, TopAccel: 1200, Walls: 3, TopLayers: 5, BottomLayers: 4, InfillPct: 15, Relative: 1.0},
	"perfect":  {Label: "Perfetta", Layer: 0.12, Outer: 36, Inner: 55, Infill: 72, Top: 30, Small: 20, Bridge: 21, OuterAccel: 900, InnerAccel: 1800, TopAccel: 750, Walls: 3, TopLayers: 7, BottomLayers: 6, InfillPct: 18, Relative: 1.72},
}

func Recommend(a ModelAnalysis, f Filament, quality string) (Recommendation, error) {
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
	m := strings.ToUpper(f.Material)
	mvs := f.MaxVolumetricSpeed
	// Un profilo del produttore può essere aggressivo. FlashFit mantiene un margine reale.
	safety := 0.72
	if quality == "perfect" {
		safety = 0.66
	}
	if strings.HasPrefix(m, "PETG") {
		safety -= 0.05
	}
	safeMVS := mvs * safety
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
		outer = math.Min(outer, 42)
		small = math.Min(small, 22)
		p.OuterAccel = math.Min(p.OuterAccel, 1100)
	}
	if a.ThinOrTall {
		outer = math.Min(outer, 42)
		inner = math.Min(inner, 62)
		p.OuterAccel = math.Min(p.OuterAccel, 1000)
		p.InnerAccel = math.Min(p.InnerAccel, 1800)
	}
	if strings.HasPrefix(m, "PETG") {
		bridge = math.Min(bridge, 22)
		small = math.Min(small, 25)
		p.OuterAccel = math.Min(p.OuterAccel, 1400)
	}
	nozzle := f.NozzleDefault
	bed := f.BedDefault
	flow := f.FlowRatio
	if flow == 0 {
		flow = 1
	}
	process := map[string]any{
		"layer_height": fmt2(p.Layer), "initial_layer_print_height": fmt2(math.Min(0.24, p.Layer+0.04)), "line_width": "0.45", "outer_wall_line_width": "0.42", "inner_wall_line_width": "0.45", "sparse_infill_line_width": "0.45",
		"wall_loops": fmt.Sprintf("%d", p.Walls), "top_shell_layers": fmt.Sprintf("%d", p.TopLayers), "bottom_shell_layers": fmt.Sprintf("%d", p.BottomLayers), "sparse_infill_density": fmt.Sprintf("%d%%", p.InfillPct), "sparse_infill_pattern": "gyroid",
		"outer_wall_speed": fmt0(outer), "inner_wall_speed": fmt0(inner), "sparse_infill_speed": fmt0(infill), "top_surface_speed": fmt0(top), "small_perimeter_speed": fmt0(small), "bridge_speed": fmt0(bridge),
		"outer_wall_acceleration": fmt0(p.OuterAccel), "inner_wall_acceleration": fmt0(p.InnerAccel), "top_surface_acceleration": fmt0(p.TopAccel), "travel_acceleration": "5000", "travel_speed": "350",
		"avoid_crossing_wall": "1", "reduce_infill_retraction": "0", "overhang_1_4_speed": "0", "overhang_2_4_speed": fmt0(math.Min(outer, 45)), "overhang_3_4_speed": fmt0(math.Min(bridge+8, 35)), "overhang_4_4_speed": fmt0(math.Min(bridge, 25)),
		"enable_support": bool01(a.SupportSuggested), "support_type": supportType(a), "support_threshold_angle": "45", "brim_type": brimType(a), "brim_width": brimWidth(a),
	}
	fil := map[string]any{
		"filament_density": fmt2(f.Density), "filament_flow_ratio": fmt3(flow), "filament_max_volumetric_speed": fmt2(safeMVS), "nozzle_temperature": fmt0(nozzle), "nozzle_temperature_initial_layer": fmt0(math.Min(f.NozzleMax, nozzle+5)),
		"hot_plate_temp": fmt0(bed), "hot_plate_temp_initial_layer": fmt0(math.Min(f.BedMax, bed+5)), "textured_plate_temp": fmt0(bed), "textured_plate_temp_initial_layer": fmt0(math.Min(f.BedMax, bed+5)), "fan_min_speed": fmt0(f.FanMin), "fan_max_speed": fmt0(f.FanMax),
	}
	if f.PressureAdvance != nil {
		fil["enable_pressure_advance"] = "1"
		fil["pressure_advance"] = fmt3(*f.PressureAdvance)
	} else {
		fil["enable_pressure_advance"] = "0"
	}
	reasons := []string{fmt.Sprintf("Velocità parete esterna limitata a %.0f mm/s per ridurre ghosting.", outer), fmt.Sprintf("Portata globale limitata al %.0f%% della MVS dichiarata: %.1f su %.1f mm³/s.", safety*100, safeMVS, mvs), fmt.Sprintf("Accelerazione esterna limitata a %.0f mm/s².", p.OuterAccel)}
	if a.SupportSuggested {
		reasons = append(reasons, "Supporti automatici attivati perché la geometria mostra sbalzi marcati.")
	}
	if a.BrimSuggested {
		reasons = append(reasons, "Brim da 5 mm attivato per impronta piccola/modello alto.")
	}
	critical := map[string]float64{"layer_height": p.Layer, "outer_wall_speed": outer, "inner_wall_speed": inner, "infill_speed": infill, "bridge_speed": bridge, "outer_acceleration": p.OuterAccel, "max_volumetric_speed": safeMVS, "nozzle_temperature": nozzle, "bed_temperature": bed}
	return Recommendation{Quality: quality, QualityLabel: p.Label, Process: process, Filament: fil, Reasons: reasons, Warnings: a.Warnings, EstimatedRelativeTime: p.Relative, CriticalValues: critical}, nil
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
