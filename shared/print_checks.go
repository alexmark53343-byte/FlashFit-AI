package shared

import (
	"fmt"
	"math"
	"strconv"
)

// A dry run over the finished profile, before the project is opened.
//
// The settings are chosen one at a time, each defensible on its own, but a
// print fails on how they combine: an acceleration that is fine on a small part
// rings on a tall one, a speed that suits PLA out-runs PETG's melt rate, a
// bridge that is fast enough on paper droops in practice. So once everything is
// decided, the whole set is walked through against the physical limits of the
// machine and the material, and anything that would show up in the print is
// reported before the user commits to it.
//
// This is a check, not a simulation of the toolpath: it reasons about rates and
// distances, not about G-code. It catches the failures that come from the
// numbers disagreeing with each other, which is the class the user would
// otherwise only discover on the plate.

// PrintIssue is one predicted defect.
type PrintIssue struct {
	Key      string  // stable identifier, for translation
	Severity int     // 1 advisory, 2 blocking
	Detail   string  // the numbers that triggered it
	Value    float64 // the offending value, for the message
	Limit    float64 // what it should not exceed
}

// PrintReadiness is the verdict on a finished profile.
type PrintReadiness struct {
	Issues  []PrintIssue
	Blocked bool
}

// LastPrintReadiness is the most recent verdict, for the interface to show.

// CheckPrintReadiness walks the decided settings against the machine and the
// material. Nothing here re-decides anything: it only reports.
func CheckPrintReadiness(rec Recommendation, a ModelAnalysis, f Filament, printer PrinterProfile) PrintReadiness {
	var out PrintReadiness
	add := func(key string, severity int, value, limit float64, detail string) {
		out.Issues = append(out.Issues, PrintIssue{Key: key, Severity: severity, Value: value, Limit: limit, Detail: detail})
		if severity >= 2 {
			out.Blocked = true
		}
	}
	num := func(key string) float64 {
		text := fmt.Sprint(rec.Process[key])
		v, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return 0
		}
		return v
	}

	layer := num("layer_height")
	outerWidth := num("outer_wall_line_width")
	if outerWidth <= 0 {
		outerWidth = 0.42
	}

	manual := ManualFor(printer)

	// A layer the fitted nozzle cannot lay down. This is the check that was
	// missing entirely: a fine quality tier on a wide nozzle asked for an
	// extrusion with nowhere to go, and nothing said so.
	if layer > 0 {
		if _, ok := manual.LayerFits(layer); !ok {
			limit := manual.MaxLayer
			if layer < manual.MinLayer {
				limit = manual.MinLayer
			}
			add("checkLayerNozzle", 2, layer, limit,
				fmt.Sprintf("layer %.2f mm con ugello %.1f mm (ammesso %.2f–%.2f)",
					layer, manual.Printer.NozzleDiameter, manual.MinLayer, manual.MaxLayer))
		}
	}

	// Ghosting: ringing behind corners comes from accelerating a moving mass
	// faster than the frame can absorb, and a tall part amplifies it because the
	// gantry has more leverage over the bed. The ceiling is the manual's, so the
	// repair that aims at it cannot drift away from the check that reports it.
	outerAccel := num("outer_wall_acceleration")
	ghostLimit := manual.AccelerationCeiling(a.Extents[2], a.ThinOrTall)
	if outerAccel > ghostLimit && ghostLimit > 0 {
		add("checkGhosting", 1, outerAccel, ghostLimit,
			fmt.Sprintf("%.0f mm/s² su un pezzo alto %.0f mm", outerAccel, a.Extents[2]))
	}

	// Flow: the extruder cannot melt faster than the filament allows, and every
	// slicer answers an impossible request by silently slowing down, which makes
	// the estimate wrong rather than the print.
	outerSpeed := num("outer_wall_speed")
	demanded := outerSpeed * layer * outerWidth
	if f.MaxVolumetricSpeed > 0 && demanded > f.MaxVolumetricSpeed {
		add("checkFlow", 2, demanded, f.MaxVolumetricSpeed,
			fmt.Sprintf("%.1f mm³/s richiesti, %s ne regge %.1f", demanded, f.Material, f.MaxVolumetricSpeed))
	}

	// Bridges and steep overhangs droop when they are laid down faster than the
	// air can set them.
	bridge := num("bridge_speed")
	bridgeLimit := 30.0
	if isPETGMaterial(f.Material) {
		// PETG stays soft longer, so it needs to be laid more slowly.
		bridgeLimit = 22
	}
	if bridge > bridgeLimit && a.OverhangRatio > 0.05 {
		add("checkBridge", 1, bridge, bridgeLimit,
			fmt.Sprintf("%.0f mm/s con %.0f%% di sbalzi", bridge, a.OverhangRatio*100))
	}

	// A very fine layer on a fast outer wall leaves too little time per layer
	// for the previous one to cool, and the part slumps.
	if layer > 0 && layer <= 0.16 && outerSpeed > 45 {
		add("checkCooling", 1, outerSpeed, 45,
			fmt.Sprintf("layer %.2f mm a %.0f mm/s", layer, outerSpeed))
	}

	// A tall narrow part on a small footprint topples; a brim is the cheap fix
	// and its absence is worth saying out loud.
	if a.ThinOrTall && fmt.Sprint(rec.Process["brim_type"]) == "no_brim" {
		add("checkAdhesionRisk", 1, 0, 0, "pezzo alto e stretto senza brim")
	}

	// Temperature has to be inside what both the material and the machine allow.
	if nozzle := firstFloatFromAny(rec.Filament["nozzle_temperature"]); nozzle > 0 {
		if nozzle > printer.MaxNozzleTemperature {
			add("checkTemp", 2, nozzle, printer.MaxNozzleTemperature,
				fmt.Sprintf("%.0f °C oltre il limite di %s", nozzle, printer.Model))
		}
		if f.NozzleMax > 0 && nozzle > f.NozzleMax {
			add("checkTemp", 2, nozzle, f.NozzleMax,
				fmt.Sprintf("%.0f °C oltre il massimo di %s", nozzle, f.Product))
		}
	}
	return out
}

func isPETGMaterial(material string) bool {
	return len(material) >= 4 && (material[:4] == "PETG" || material[:4] == "petg")
}

func firstFloatFromAny(v any) float64 {
	switch value := v.(type) {
	case float64:
		return value
	case string:
		f, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return 0
		}
		return f
	case []any:
		if len(value) > 0 {
			return firstFloatFromAny(value[0])
		}
	case []string:
		// Filament settings are per-extruder and sometimes arrive already
		// wrapped. Reading the first entry rather than returning 0 means a
		// wrapped temperature is still seen as the number it is, instead of
		// silently reading as absent.
		if len(value) > 0 {
			return firstFloatFromAny(value[0])
		}
	}
	return 0
}

// WorstSeverity is what the interface shows as the overall verdict.
func (p PrintReadiness) WorstSeverity() int {
	worst := 0
	for _, issue := range p.Issues {
		if issue.Severity > worst {
			worst = issue.Severity
		}
	}
	return worst
}

// Summary is a one-line verdict for the status bar.
func (p PrintReadiness) Summary() string {
	if len(p.Issues) == 0 {
		return ""
	}
	blocking := 0
	for _, issue := range p.Issues {
		if issue.Severity >= 2 {
			blocking++
		}
	}
	if blocking > 0 {
		return fmt.Sprintf("%d problemi bloccanti", blocking)
	}
	return fmt.Sprintf("%d avvisi", len(p.Issues))
}

var _ = math.Abs
