package shared

import (
	"fmt"
	"math"
	"sort"
)

// Splitting a model that does not fit across several plates.
//
// Scaling an oversized download down to the plate is the wrong answer when the
// file is really several separate pieces — a car plus its garage, a kit, a set
// of parts. The user wants them at full size, just not all at once. So the mesh
// is separated into the pieces that are actually disconnected from one another,
// and those pieces are packed onto as many plates as it takes.
//
// The split is done on the geometry rather than on the file's object list, so
// it works the same for an STL, which has no notion of objects at all.

// Piece is one connected run of geometry, with the size it occupies.
type Piece struct {
	Triangles []triangle
	Extents    [3]float64
	Footprint  float64
}

// Plate is a set of pieces that fit on one build plate together.
type Plate struct {
	Pieces  []Piece
	Extents [3]float64
}

// SplitIntoPieces separates a mesh into its disconnected parts. Vertices are
// matched by quantised position, the same way the geometry writer welds them,
// so pieces that merely touch in the file are still recognised as one.
func SplitIntoPieces(tris []triangle) []Piece {
	if len(tris) == 0 {
		return nil
	}
	// Union-find over triangles, joined through shared vertices.
	parent := make([]int, len(tris))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(x int) int {
		for parent[x] != x {
			parent[x] = parent[parent[x]]
			x = parent[x]
		}
		return x
	}
	union := func(a, b int) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[rb] = ra
		}
	}

	seen := make(map[qv]int, len(tris)*2)
	for i, t := range tris {
		for _, v := range []vec3{t.A, t.B, t.C} {
			key := quant(v)
			if first, ok := seen[key]; ok {
				union(first, i)
			} else {
				seen[key] = i
			}
		}
	}

	groups := make(map[int][]triangle)
	for i, t := range tris {
		root := find(i)
		groups[root] = append(groups[root], t)
	}

	pieces := make([]Piece, 0, len(groups))
	for _, group := range groups {
		pieces = append(pieces, newPiece(group))
	}
	// Largest first: the pieces that constrain packing get placed first.
	sort.Slice(pieces, func(i, j int) bool { return pieces[i].Footprint > pieces[j].Footprint })
	return pieces
}

func newPiece(tris []triangle) Piece {
	min := vec3{math.Inf(1), math.Inf(1), math.Inf(1)}
	max := vec3{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	for _, t := range tris {
		for _, v := range []vec3{t.A, t.B, t.C} {
			min.X, max.X = math.Min(min.X, v.X), math.Max(max.X, v.X)
			min.Y, max.Y = math.Min(min.Y, v.Y), math.Max(max.Y, v.Y)
			min.Z, max.Z = math.Min(min.Z, v.Z), math.Max(max.Z, v.Z)
		}
	}
	extents := [3]float64{max.X - min.X, max.Y - min.Y, max.Z - min.Z}
	return Piece{Triangles: tris, Extents: extents, Footprint: extents[0] * extents[1]}
}

// PieceFits reports whether a single piece can be printed at all on a machine.
func PieceFits(p Piece, volume [3]float64) bool {
	return p.Extents[0] <= volume[0]+0.01 &&
		p.Extents[1] <= volume[1]+0.01 &&
		p.Extents[2] <= volume[2]+0.01
}

// PackIntoPlates distributes pieces over plates. Packing is by footprint area
// with a deliberate margin: this decides how many plates are needed, while the
// slicer does the real arranging when it opens each project.
//
// Pieces too large for the machine even alone are returned separately rather
// than silently dropped — those need scaling or cutting, and saying so is more
// use than pretending they were handled.
func PackIntoPlates(pieces []Piece, volume [3]float64) (plates []Plate, oversized []Piece) {
	const usableArea = 0.80 // arranging never achieves perfect coverage
	plateArea := volume[0] * volume[1] * usableArea

	for _, piece := range pieces {
		if !PieceFits(piece, volume) {
			oversized = append(oversized, piece)
			continue
		}
		placed := false
		for i := range plates {
			used := 0.0
			for _, existing := range plates[i].Pieces {
				used += existing.Footprint
			}
			if used+piece.Footprint <= plateArea && piece.Extents[2] <= volume[2] {
				plates[i].Pieces = append(plates[i].Pieces, piece)
				plates[i].Extents = plateExtents(plates[i].Pieces)
				placed = true
				break
			}
		}
		if !placed {
			plates = append(plates, Plate{Pieces: []Piece{piece}, Extents: piece.Extents})
		}
	}
	return plates, oversized
}

func plateExtents(pieces []Piece) [3]float64 {
	var out [3]float64
	for _, p := range pieces {
		for i := 0; i < 3; i++ {
			if p.Extents[i] > out[i] {
				out[i] = p.Extents[i]
			}
		}
	}
	return out
}

// PlateTriangles merges the pieces of a plate back into one mesh.
func PlateTriangles(plate Plate) []triangle {
	total := 0
	for _, p := range plate.Pieces {
		total += len(p.Triangles)
	}
	out := make([]triangle, 0, total)
	for _, p := range plate.Pieces {
		out = append(out, p.Triangles...)
	}
	return out
}

// DescribePlates summarises the split for the user.
func DescribePlates(plates []Plate, oversized []Piece) string {
	if len(plates) == 0 {
		return "nessun pezzo stampabile"
	}
	text := fmt.Sprintf("%d piatti", len(plates))
	if len(plates) == 1 {
		text = "1 piatto"
	}
	pieces := 0
	for _, plate := range plates {
		pieces += len(plate.Pieces)
	}
	text += fmt.Sprintf(", %d pezzi", pieces)
	if len(oversized) > 0 {
		text += fmt.Sprintf(", %d troppo grandi", len(oversized))
	}
	return text
}
