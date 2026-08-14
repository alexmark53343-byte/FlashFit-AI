package shared

import (
	"math"
	"testing"
)

// box returns the closed surface of an axis-aligned box.
func box(minX, minY, minZ, sizeX, sizeY, sizeZ float64) []triangle {
	x0, y0, z0 := minX, minY, minZ
	x1, y1, z1 := minX+sizeX, minY+sizeY, minZ+sizeZ
	corner := [8]vec3{
		{x0, y0, z0}, {x1, y0, z0}, {x1, y1, z0}, {x0, y1, z0},
		{x0, y0, z1}, {x1, y0, z1}, {x1, y1, z1}, {x0, y1, z1},
	}
	quads := [6][4]int{
		{0, 3, 2, 1}, {4, 5, 6, 7}, // bottom, top
		{0, 1, 5, 4}, {2, 3, 7, 6}, // front, back
		{1, 2, 6, 5}, {3, 0, 4, 7}, // right, left
	}
	out := make([]triangle, 0, 12)
	for _, q := range quads {
		out = append(out,
			triangle{A: corner[q[0]], B: corner[q[1]], C: corner[q[2]]},
			triangle{A: corner[q[0]], B: corner[q[2]], C: corner[q[3]]},
		)
	}
	return out
}

// Two bodies welded into one solid by a thin neck: a boolean union in CAD, a
// print-in-place assembly, parts joined by sprues. The connectivity split sees
// one piece, so before this the only answers were "scale it down" or "refuse".
func TestUnionedObjectsAreSeparatedAtTheirJoin(t *testing.T) {
	var tris []triangle
	tris = append(tris, box(0, 0, 0, 150, 150, 150)...)   // first body
	tris = append(tris, box(150, 70, 70, 10, 10, 10)...)  // the join
	tris = append(tris, box(160, 0, 0, 150, 150, 150)...) // second body

	piece := newPiece(tris)
	usable := [3]float64{212, 212, 216} // an Adventurer 5M's usable plate
	if PieceFits(piece, usable) {
		t.Fatal("presupposto del test non valido: il pezzo intero non deve entrare")
	}

	split, ok := SplitUnionedObjects(piece, usable)
	if !ok {
		t.Fatal("due corpi uniti da un collo sottile non sono stati riconosciuti")
	}
	if split.Axis != 0 {
		t.Fatalf("il taglio doveva cadere sull'asse X, invece è %d", split.Axis)
	}
	if split.Position < 150 || split.Position > 160 {
		t.Fatalf("il taglio è caduto a %.1f, fuori dal collo (150–160)", split.Position)
	}
	if len(split.Pieces) != 2 {
		t.Fatalf("attesi 2 pezzi, ottenuti %d", len(split.Pieces))
	}
	for i, part := range split.Pieces {
		if !PieceFits(part, usable) {
			t.Fatalf("il pezzo %d continua a non entrare: %v", i, part.Extents)
		}
	}
}

// The other half of the guarantee: a single body must not be cut up. An
// hourglass or a waisted vase narrows in the middle and is still one object,
// and separating it would be vandalism rather than a fix.
func TestSingleBodyIsNeverCutUp(t *testing.T) {
	// A tapered body: wide at both ends, narrower in the middle, but never
	// pinched to anything like a join.
	var tris []triangle
	for i := 0; i < 20; i++ {
		z := float64(i) * 15
		// Waist at the centre reaches 60% of the ends, well above the threshold.
		scale := 1.0 - 0.4*math.Sin(float64(i)/19*math.Pi)
		side := 200 * scale
		tris = append(tris, box(-side/2, -side/2, z, side, side, 15)...)
	}
	piece := newPiece(tris)
	usable := [3]float64{212, 212, 216}

	if _, ok := SplitUnionedObjects(piece, usable); ok {
		t.Fatal("un corpo unico rastremato è stato scambiato per un'unione e tagliato")
	}
}

// A cut that leaves the face open hands the slicer a shell with a hole in it,
// which prints as a hollow object with a missing wall — worse than the oversize
// it was meant to fix. Both sides have to come out closed.
func TestBothSidesOfTheCutAreClosed(t *testing.T) {
	var tris []triangle
	tris = append(tris, box(0, 0, 0, 150, 150, 150)...)
	tris = append(tris, box(150, 70, 70, 10, 10, 10)...)
	tris = append(tris, box(160, 0, 0, 150, 150, 150)...)

	split, ok := SplitUnionedObjects(newPiece(tris), [3]float64{212, 212, 216})
	if !ok {
		t.Fatal("il taglio non è avvenuto")
	}
	for i, part := range split.Pieces {
		if open := openEdgeCount(part.Triangles); open != 0 {
			t.Fatalf("il pezzo %d ha %d spigoli aperti: la faccia di taglio non è stata chiusa", i, open)
		}
	}
}

// openEdgeCount counts edges used by exactly one triangle. A closed surface has
// none: every edge is shared by the two faces that meet along it.
func openEdgeCount(tris []triangle) int {
	uses := map[edge]int{}
	for _, t := range tris {
		verts := [3]vec3{t.A, t.B, t.C}
		for i := 0; i < 3; i++ {
			a, b := quant(verts[i]), quant(verts[(i+1)%3])
			if lessQ(b, a) {
				a, b = b, a
			}
			uses[edge{A: a, B: b}]++
		}
	}
	open := 0
	for _, count := range uses {
		if count == 1 {
			open++
		}
	}
	return open
}
