package shared

import (
	"archive/zip"
	"io"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The coordinates written have to reproduce the mesh, not approximate it.
//
// Vertices are welded on a 1e-5 mm grid, so the file has to carry five
// decimals: fewer would make two vertices that are distinct on that grid print
// identically, and a triangle whose corners collapse that way arrives at the
// slicer as a degenerate face to find and repair.
func TestGeometryRoundTripsWithoutLoss(t *testing.T) {
	// Coordinates chosen to exercise the rounding: long decimals, negatives,
	// values that differ only in the last preserved digit.
	tris := []triangle{
		{A: vec3{0, 0, 0}, B: vec3{1.0 / 3.0, 2.0 / 7.0, -5.0 / 3.0}, C: vec3{10.000012, -0.000019, 3}},
		{A: vec3{10.000012, -0.000019, 3}, B: vec3{10.000021, -0.000011, 3}, C: vec3{0, 0, 0}},
		{A: vec3{-123.456789, 99.999995, 0.000004}, B: vec3{1, 2, 3}, C: vec3{4, 5, 6}},
	}
	path := filepath.Join(t.TempDir(), "geo.3mf")
	if err := writeGeometryOnly3MF(path, tris); err != nil {
		t.Fatalf("scrittura: %v", err)
	}
	back, err := loadModelTriangles(path)
	if err != nil {
		t.Fatalf("rilettura: %v", err)
	}
	if len(back) != len(tris) {
		t.Fatalf("%d triangoli riletti su %d scritti: qualcuno è collassato", len(back), len(tris))
	}
	for i := range tris {
		for _, pair := range [][2]vec3{{tris[i].A, back[i].A}, {tris[i].B, back[i].B}, {tris[i].C, back[i].C}} {
			for axis, delta := range []float64{pair[0].X - pair[1].X, pair[0].Y - pair[1].Y, pair[0].Z - pair[1].Z} {
				// The weld grid is 1e-5 mm; anything inside it is exact as far
				// as this file is concerned.
				if math.Abs(delta) > 1e-5 {
					t.Fatalf("triangolo %d asse %d: scostamento %.9f oltre la griglia di saldatura", i, axis, delta)
				}
			}
		}
	}
}

// The slicer has to parse every byte of this, so the file should not spell out
// the noise of a float64 division. Nine significant figures did.
func TestGeometryCarriesNoSuperfluousDigits(t *testing.T) {
	tris := []triangle{
		{A: vec3{0, 0, 0}, B: vec3{1.0 / 3.0, 2.0 / 3.0, 1}, C: vec3{2, 3, 4}},
		{A: vec3{2, 3, 4}, B: vec3{1.0 / 7.0, 5, 6}, C: vec3{0, 0, 0}},
	}
	path := filepath.Join(t.TempDir(), "geo.3mf")
	if err := writeGeometryOnly3MF(path, tris); err != nil {
		t.Fatalf("scrittura: %v", err)
	}
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	for _, entry := range zr.File {
		if entry.Name != modelEntry {
			continue
		}
		r, _ := entry.Open()
		body, _ := io.ReadAll(r)
		r.Close()
		text := string(body)
		// A whole number keeps no decimal point at all, and nothing carries
		// more than the five decimals the weld preserves.
		if strings.Contains(text, `x="2.00000"`) || strings.Contains(text, `x="2."`) {
			t.Fatal("gli interi vengono scritti con decimali inutili")
		}
		for _, sample := range strings.Split(text, `"`) {
			if dot := strings.IndexByte(sample, '.'); dot >= 0 && len(sample)-dot-1 > 5 {
				if _, err := parseCoordForTest(sample); err == nil {
					t.Fatalf("coordinata con più di cinque decimali: %q", sample)
				}
			}
		}
		return
	}
	t.Fatal("modello non trovato nell'archivio")
}

func parseCoordForTest(s string) (float64, error) {
	return strconvParseFloat(s)
}

func strconvParseFloat(s string) (float64, error) {
	return strconv.ParseFloat(s, 64)
}
