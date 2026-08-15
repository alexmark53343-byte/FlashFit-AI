package shared

import (
	"math"
	"testing"
)

// A mesh arrives wherever its author left it — centred on the origin, off in a
// corner, or a hundred millimetres away because that is where it sat in the
// scene it came from. Nothing moved it, so the project opened with the part
// beside the bed and the user dragged it back every time.
func TestModelIsPlacedOnThePlate(t *testing.T) {
	plate := ManualFor(DefaultPrinterProfile()).UsablePlate
	for name, tris := range map[string][]triangle{
		"centrato sull'origine": box(-20, -15, -10, 40, 30, 20),
		"lontano dal piano":     box(500, -400, 87, 40, 30, 20),
		"sotto il piano":        box(0, 0, -60, 40, 30, 20),
		"già a posto":           box(plate[0]/2-20, plate[1]/2-15, 0, 40, 30, 20),
	} {
		placed := centreOnPlate(tris, plate)
		min, max := meshBounds(placed)
		if math.Abs(min.Z) > 1e-9 {
			t.Fatalf("%s: il pezzo non poggia sul piano, base a z=%.3f", name, min.Z)
		}
		if cx := (min.X + max.X) / 2; math.Abs(cx-plate[0]/2) > 1e-9 {
			t.Fatalf("%s: non centrato in X: %.3f invece di %.3f", name, cx, plate[0]/2)
		}
		if cy := (min.Y + max.Y) / 2; math.Abs(cy-plate[1]/2) > 1e-9 {
			t.Fatalf("%s: non centrato in Y: %.3f invece di %.3f", name, cy, plate[1]/2)
		}
		if min.X < 0 || min.Y < 0 || max.X > plate[0] || max.Y > plate[1] {
			t.Fatalf("%s: resta fuori dal piano: %v..%v", name, min, max)
		}
	}
}

// Placing is not resizing. A part too big for the plate stays too big — that is
// the splitter's job, and shrinking it quietly is what this project removed.
func TestPlacingNeverResizesTheModel(t *testing.T) {
	plate := ManualFor(DefaultPrinterProfile()).UsablePlate
	huge := box(0, 0, 0, 400, 380, 300)
	before, beforeMax := meshBounds(huge)
	after, afterMax := meshBounds(centreOnPlate(huge, plate))
	for axis, pair := range [][2]float64{
		{beforeMax.X - before.X, afterMax.X - after.X},
		{beforeMax.Y - before.Y, afterMax.Y - after.Y},
		{beforeMax.Z - before.Z, afterMax.Z - after.Z},
	} {
		if math.Abs(pair[0]-pair[1]) > 1e-9 {
			t.Fatalf("asse %d ridimensionato: %.3f diventato %.3f", axis, pair[0], pair[1])
		}
	}
}

// Moving the mesh must not change the mesh: a translation has no business
// touching the triangle count or the winding.
func TestPlacingPreservesTheMesh(t *testing.T) {
	plate := ManualFor(DefaultPrinterProfile()).UsablePlate
	tris := box(37, -12, 5, 40, 30, 20)
	placed := centreOnPlate(tris, plate)
	if len(placed) != len(tris) {
		t.Fatalf("%d triangoli su %d dopo il posizionamento", len(placed), len(tris))
	}
	for i := range tris {
		b, a := triangleNormal(tris[i]), triangleNormal(placed[i])
		if math.Abs(b.X-a.X) > 1e-9 || math.Abs(b.Y-a.Y) > 1e-9 || math.Abs(b.Z-a.Z) > 1e-9 {
			t.Fatalf("triangolo %d: normale alterata dalla traslazione", i)
		}
	}
}

func triangleNormal(t triangle) vec3 {
	ux, uy, uz := t.B.X-t.A.X, t.B.Y-t.A.Y, t.B.Z-t.A.Z
	vx, vy, vz := t.C.X-t.A.X, t.C.Y-t.A.Y, t.C.Z-t.A.Z
	return vec3{uy*vz - uz*vy, uz*vx - ux*vz, ux*vy - uy*vx}
}
