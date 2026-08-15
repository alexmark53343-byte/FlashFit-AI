package shared

import (
	"strings"
	"testing"
)

// PROBE: after S.O.G repairs, can the outer wall end up FASTER than the inner
// wall? That is physically backwards — the outer wall is the visible one and is
// meant to be the slower. S.O.G only lowers outer, so it should never happen,
// but let me force the checks to fire hard and verify.
func TestAudit_SpeedOrderingAfterSOG(t *testing.T) {
	rec, a, f, printer := sogFixture(t)
	// Drive cooling + flow hard so outer wall gets lowered a lot.
	rec.Process["outer_wall_speed"] = "300"
	rec.Process["layer_height"] = "0.10"
	SecureProfile(&rec, a, f, printer)
	outer := processFloat(&rec, "outer_wall_speed")
	inner := processFloat(&rec, "inner_wall_speed")
	t.Logf("dopo S.O.G: outer=%.0f inner=%.0f", outer, inner)
	if outer > inner {
		t.Errorf("parete esterna (%.0f) più veloce dell'interna (%.0f): ordine invertito", outer, inner)
	}
}

// PROBE: a recommendation with a valid profile whose bridge speed exceeds the
// bridge acceleration's implied capability — a nonsensical combo the checks
// might not catch. More generally: does every produced profile have a positive
// layer height and positive speeds? A zero or negative would pass a JSON check
// but produce nothing.
func TestAudit_NoZeroOrNegativeCoreSettings(t *testing.T) {
	filaments, _ := LoadBuiltinFilaments()
	a := ModelAnalysis{Filename: "x.stl", Category: "Oggetto tecnico/decorativo",
		Extents: [3]float64{60, 40, 30}, Volume: 24000, SurfaceArea: 5200,
		Watertight: true, TriangleCount: 12}
	for _, printer := range SupportedPrinters() {
		for _, quality := range []string{"low", "balanced", "perfect"} {
			rec, err := RecommendForPrinter(a, filaments[0], printer, quality)
			if err != nil {
				continue
			}
			SecureProfile(&rec, a, filaments[0], printer)
			for _, key := range []string{"layer_height", "outer_wall_speed", "inner_wall_speed",
				"sparse_infill_speed", "bridge_speed", "initial_layer_speed", "travel_speed"} {
				if v := processFloat(&rec, key); v <= 0 {
					t.Errorf("%s %s: %s = %.2f (≤0, produce una stampa impossibile)", printer.Model, quality, key, v)
				}
			}
		}
	}
}

// PROBE: projectSettingsJSON with a filament value containing characters that
// would break JSON if not escaped — quotes, backslashes, newlines.
func TestAudit_ProjectJSONEscaping(t *testing.T) {
	rec, _, _, printer := sogFixture(t)
	rec.Filament["filament_type"] = `PLA" injected":"evil`
	rec.Process["bad_key"] = "value\"with\"quotes\nand newline"
	out := projectSettingsJSON(rec, printer, "P", "", "", "")
	// It must still parse as JSON.
	if !strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Fatal("output non è un oggetto JSON")
	}
	if !json_ok(out) {
		t.Errorf("projectSettingsJSON ha prodotto JSON non valido con valori ostili")
	}
}

func json_ok(s string) bool {
	var v any
	return jsonUnmarshalProbe([]byte(s), &v) == nil
}

func jsonUnmarshalProbe(b []byte, v any) error {
	return jsonUnmarshalReal(b, v)
}
