package shared

import (
	"strconv"
	"testing"
)

// Vendors name filaments freely. The family has to survive that, or the whole
// catalogue narrows back down to whatever spells itself exactly "PLA".
func TestFamilyClassification(t *testing.T) {
	cases := map[string]MaterialFamily{
		"PLA":        FamilyPLA,
		"PLA+":       FamilyPLA,
		"PLA-CF":     FamilyPLA,
		"PLA MATTE":  FamilyPLA,
		"HS PLA":     FamilyPLA,
		"PLA SILK":   FamilyPLA,
		"PETG":       FamilyPETG,
		"PETG-CF":    FamilyPETG,
		"HS PETG":    FamilyPETG,
		"PET-CF":     FamilyPETG,
		"PET-GF":     FamilyPETG,
		"ABS":        FamilyABS,
		"ASA":        FamilyABS,
		"ASA-CF":     FamilyABS,
		"TPU 95A":    FamilyTPU,
		"PA":         FamilyNylon,
		"PA-CF":      FamilyNylon,
		"PC":         FamilyPC,
		"PVA":        FamilySupport,
		"HIPS":       FamilySupport,
	}
	for material, want := range cases {
		if got := FamilyOf(material); got != want {
			t.Fatalf("%s classificato %s invece di %s", material, got, want)
		}
	}
}

// PLA must not swallow PVA, and PETG must not be caught by the PET rule.
func TestFamilyPrefixesDoNotCollide(t *testing.T) {
	if FamilyOf("PVA") == FamilyPLA {
		t.Fatal("PVA classificato come PLA")
	}
	if FamilyOf("PETG") != FamilyPETG {
		t.Fatal("PETG non riconosciuto")
	}
	if FamilyOf("PA-CF") == FamilyPETG {
		t.Fatal("PA-CF classificato come PETG")
	}
}

// Every material in the shipped catalogue must be one the engine can tune for,
// otherwise it is listed and then refused.
func TestEveryCatalogueMaterialIsSupported(t *testing.T) {
	filaments, err := LoadBuiltinFilaments()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[MaterialFamily]int{}
	for _, f := range filaments {
		if !SupportedMaterial(f.Material) {
			t.Fatalf("%s (%s) è nel catalogo ma non è supportato dal motore", f.Product, f.Material)
		}
		seen[FamilyOf(f.Material)]++
	}
	// The catalogue is meant to be broad, not just PLA and PETG.
	if len(seen) < 5 {
		t.Fatalf("catalogo troppo stretto: solo %d famiglie", len(seen))
	}
	t.Logf("famiglie coperte: %d, filamenti: %d", len(seen), len(filaments))
	for family, count := range seen {
		t.Logf("  %-6s %d", family, count)
	}
}

// The families that warp must come out with cooling held down and a brim on,
// whatever the shape of the part says.
func TestWarpingMaterialsGetColdFanAndBrim(t *testing.T) {
	a := ModelAnalysis{
		Extents: [3]float64{60, 60, 40}, SurfaceArea: 16800, Volume: 144000,
		TriangleCount: 5000, Watertight: true, BedContactRatio: 0.3,
	}
	filaments, err := LoadBuiltinFilaments()
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, f := range filaments {
		family := FamilyOf(f.Material)
		if family != FamilyABS && family != FamilyPC && family != FamilyNylon {
			continue
		}
		rec, err := Recommend(a, f, "balanced")
		if err != nil {
			continue // some need a hotter machine than the default profile
		}
		fan, _ := strconv.ParseFloat(asText(rec.Filament["fan_max_speed"]), 64)
		limit := float64(BehaviourOf(f.Material).MaxFanPercent)
		if fan > limit {
			t.Fatalf("%s (%s): ventola al %.0f%%, oltre il limite %.0f%% della famiglia", f.Product, f.Material, fan, limit)
		}
		if asText(rec.Process["brim_type"]) == "no_brim" {
			t.Fatalf("%s (%s): nessun brim su un materiale che si imbarca", f.Product, f.Material)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("nessun materiale tecnico verificato")
	}
	t.Logf("verificati %d filamenti tecnici", checked)
}

// A flexible is limited by the extruder, not by how fast it melts.
func TestFlexibleIsSpeedLimited(t *testing.T) {
	a := ModelAnalysis{
		Extents: [3]float64{40, 40, 20}, SurfaceArea: 6400, Volume: 32000,
		TriangleCount: 2000, Watertight: true, BedContactRatio: 0.3,
	}
	filaments, _ := LoadBuiltinFilaments()
	for _, f := range filaments {
		if FamilyOf(f.Material) != FamilyTPU {
			continue
		}
		rec, err := Recommend(a, f, "balanced")
		if err != nil {
			continue
		}
		outer := rec.CriticalValues["outer_wall_speed"]
		if outer > float64(BehaviourOf(f.Material).SpeedCeiling) {
			t.Fatalf("%s: parete esterna a %.0f mm/s, oltre il limite per un flessibile", f.Product, outer)
		}
		return
	}
	t.Skip("nessun flessibile nel catalogo")
}

func asText(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
