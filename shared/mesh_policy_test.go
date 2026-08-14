package shared

import "testing"

// The line between "imperfect" and "unusable" is proportional, not absolute.
// A handful of bad triangles in a large mesh is normal and the slicer repairs
// it; a mesh where a large share is malformed is genuinely broken.
func TestMeshDefectPolicyIsProportional(t *testing.T) {
	cases := []struct {
		name       string
		triangles  int
		degenerate int
		blocked    bool
	}{
		{"modello reale con 4 difetti", 746_632, 4, false},
		{"mesh pulita", 100_000, 0, false},
		{"difetti entro la soglia", 100_000, 1_000, false},
		{"mesh compromessa", 100_000, 30_000, true},
		{"mesh quasi tutta rotta", 1_000, 900, true},
	}
	for _, c := range cases {
		a := ModelAnalysis{
			TriangleCount:   c.triangles,
			DegenerateFaces: c.degenerate,
			Extents:         [3]float64{100, 100, 100},
			Watertight:      c.degenerate == 0,
		}
		err := ValidateAnalysis(a)
		if c.blocked && err == nil {
			t.Fatalf("%s: doveva essere bloccata", c.name)
		}
		if !c.blocked && err != nil {
			t.Fatalf("%s: non doveva essere bloccata: %v", c.name, err)
		}
	}
}

// Things that make a mesh genuinely unprocessable must still be refused.
func TestUnusableMeshesStillRefused(t *testing.T) {
	for name, a := range map[string]ModelAnalysis{
		"senza triangoli": {TriangleCount: 0, Extents: [3]float64{10, 10, 10}, Watertight: true},
		"oltre il limite": {TriangleCount: MaxTriangles + 1, Extents: [3]float64{10, 10, 10}, Watertight: true},
		"fuori misura":    {TriangleCount: 100, Extents: [3]float64{900, 10, 10}, Watertight: true},
	} {
		if ValidateAnalysis(a) == nil {
			t.Fatalf("%s: doveva essere rifiutata", name)
		}
	}
}
