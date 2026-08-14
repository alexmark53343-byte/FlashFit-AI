package shared

import (
	"math"
	"sort"
)

// S.O.G's second job: the "part" that is really several parts.
//
// The plate splitter separates a file into pieces that are geometrically
// disconnected, which handles a download holding a car and its garage. It
// cannot handle the other case, and the other case is common: several objects
// welded into a single solid — a boolean union in CAD, a print-in-place
// assembly, a set of parts joined by sprues so they arrive as one file. To the
// connectivity test that is one piece, so if it overruns the plate the only
// answers left are "scale it down" or "refuse", and both throw away what the
// user actually wanted.
//
// The signature of a union is a waist. A single organic body has a
// cross-section that changes smoothly along its length; two bodies joined
// together have a place where the cross-section collapses to the size of the
// join and opens out again. So the piece is measured across its length, and if
// there is a pinch that the rest of the body does not explain, that is where it
// was joined and that is where it comes apart.
//
// Where it is separated it is also closed. Cutting a solid and leaving the face
// open would hand the slicer a shell with a hole in it, which prints as a hollow
// object with a missing wall — a worse outcome than the oversize it was meant to
// fix. So the crossing triangles are clipped properly and the exposed face is
// capped, and if the cut cannot be closed cleanly the split is abandoned rather
// than delivered broken.

// UnionSplit is the result of looking for a join in a single piece.
type UnionSplit struct {
	Found bool
	// Axis and Position locate the cut.
	Axis     int
	Position float64
	// Narrowness is the waist's cross-section as a fraction of the body's
	// typical one. Smaller means a more obvious join.
	Narrowness float64
	Pieces     []Piece
}

const (
	// A join has to be markedly narrower than the body around it. At half the
	// typical cross-section an hourglass or a waisted vase would qualify, and
	// cutting those would be vandalism; a third is the region where the shape
	// stops being explicable as one body.
	unionWaistRatio = 0.34
	// The ends of any shape taper, so the outer fifth of the sweep is not a
	// candidate: the narrowest cross-section of almost every object is its tip.
	unionEdgeExclusion = 0.20
	// Enough samples to find a neck a few millimetres wide on a large part,
	// cheap enough to run on a mesh with a hundred thousand triangles.
	unionSweepSamples = 96
	// Below this the cut face could not be closed and the split is refused.
	unionMinCappedFraction = 0.90
)

// SplitUnionedObjects looks for a join in a piece that does not fit, and
// separates the piece there if it finds one.
//
// It is only ever asked about pieces that have already failed to fit, so the
// cost is paid on the rare file rather than on every import, and a false
// negative simply leaves the existing behaviour in place.
func SplitUnionedObjects(piece Piece, usable [3]float64) (UnionSplit, bool) {
	if len(piece.Triangles) < 12 {
		return UnionSplit{}, false
	}
	axis, position, narrowness, ok := findWaist(piece.Triangles)
	if !ok || narrowness > unionWaistRatio {
		return UnionSplit{}, false
	}

	below, above, ok := cutAndClose(piece.Triangles, axis, position)
	if !ok {
		return UnionSplit{}, false
	}
	if len(below) == 0 || len(above) == 0 {
		return UnionSplit{}, false
	}

	parts := []Piece{newPiece(below), newPiece(above)}
	// The split has to actually achieve something. Separating a part into two
	// halves that still do not fit is churn, and worse, it has damaged the mesh
	// for nothing.
	for _, part := range parts {
		if !PieceFits(part, usable) {
			return UnionSplit{}, false
		}
	}
	return UnionSplit{
		Found: true, Axis: axis, Position: position,
		Narrowness: narrowness, Pieces: parts,
	}, true
}

// findWaist sweeps each axis and reports the most pronounced pinch.
//
// The cross-section is measured as the perimeter of the outline the plane cuts
// through the solid — the total length of the segments where the plane crosses
// a triangle. It is the right measure because it scales with the size of the
// section without needing the section to be closed, convex or singly connected,
// none of which can be assumed of a mesh that arrived from the internet.
func findWaist(tris []triangle) (bestAxis int, bestPos, bestNarrowness float64, ok bool) {
	bestNarrowness = math.Inf(1)
	for axis := 0; axis < 3; axis++ {
		low, high := axisRange(tris, axis)
		span := high - low
		if span <= 0 {
			continue
		}
		start := low + span*unionEdgeExclusion
		end := high - span*unionEdgeExclusion
		if end <= start {
			continue
		}

		samples := make([]float64, 0, unionSweepSamples)
		positions := make([]float64, 0, unionSweepSamples)
		for i := 0; i < unionSweepSamples; i++ {
			pos := start + (end-start)*float64(i)/float64(unionSweepSamples-1)
			samples = append(samples, sectionPerimeter(tris, axis, pos))
			positions = append(positions, pos)
		}

		typical := median(samples)
		if typical <= 0 {
			continue
		}
		for i, value := range samples {
			// A plane that crosses nothing is not a waist, it is a gap between
			// two disconnected bodies — which the connectivity split already
			// handles, and which would make a meaningless zero-width cut here.
			if value <= 0 {
				continue
			}
			if narrowness := value / typical; narrowness < bestNarrowness {
				bestAxis, bestPos, bestNarrowness, ok = axis, positions[i], narrowness, true
			}
		}
	}
	return bestAxis, bestPos, bestNarrowness, ok
}

// sectionPerimeter is the total length of the outline the plane cuts.
func sectionPerimeter(tris []triangle, axis int, pos float64) float64 {
	total := 0.0
	for _, t := range tris {
		if a, b, crosses := trianglePlaneSegment(t, axis, pos); crosses {
			total += distance(a, b)
		}
	}
	return total
}

// trianglePlaneSegment returns the segment where a plane crosses a triangle.
func trianglePlaneSegment(t triangle, axis int, pos float64) (vec3, vec3, bool) {
	verts := [3]vec3{t.A, t.B, t.C}
	var points []vec3
	for i := 0; i < 3; i++ {
		p, q := verts[i], verts[(i+1)%3]
		pv, qv := axisValue(p, axis), axisValue(q, axis)
		if (pv < pos && qv >= pos) || (qv < pos && pv >= pos) {
			points = append(points, lerpToPlane(p, q, axis, pos))
		}
	}
	if len(points) != 2 {
		return vec3{}, vec3{}, false
	}
	return points[0], points[1], true
}

// cutAndClose clips every triangle against the plane and closes both exposed
// faces, so each side comes out a solid rather than an open shell.
func cutAndClose(tris []triangle, axis int, pos float64) (below, above []triangle, ok bool) {
	var seams []segment
	for _, t := range tris {
		lo, hi, seam, crossed := clipTriangle(t, axis, pos)
		below = append(below, lo...)
		above = append(above, hi...)
		if crossed {
			seams = append(seams, seam)
		}
	}
	if len(seams) == 0 {
		return nil, nil, false
	}
	loops, capped := buildLoops(seams)
	if capped < unionMinCappedFraction {
		// The outline did not close. Capping it anyway would invent a surface
		// that is not there, so the split is refused and the caller keeps the
		// behaviour it had.
		return nil, nil, false
	}
	for _, loop := range loops {
		// Each side is closed with the same outline, wound opposite ways so
		// both keep their normals pointing out of their own solid.
		below = append(below, fanTriangulate(loop, axis, false)...)
		above = append(above, fanTriangulate(loop, axis, true)...)
	}
	return below, above, true
}

type segment struct{ A, B vec3 }

// clipTriangle splits one triangle against the plane, and contributes the
// segment where it met it.
//
// Both sides are built by walking the triangle's edges in order and collecting
// what survives, which is the standard polygon clip. Doing it that way rather
// than sorting vertices into two buckets is what keeps the winding intact and
// the two sides' cut edges identical — pairing the cut points by hand looks
// simpler and gets the correspondence wrong whenever the lone vertex is not the
// first one, which leaves the seam not quite matching and the face not quite
// closed.
func clipTriangle(t triangle, axis int, pos float64) (below, above []triangle, seam segment, crossed bool) {
	verts := [3]vec3{t.A, t.B, t.C}
	var loPoly, hiPoly, cuts []vec3

	for i := 0; i < 3; i++ {
		current, next := verts[i], verts[(i+1)%3]
		currentBelow := axisValue(current, axis) < pos
		nextBelow := axisValue(next, axis) < pos

		if currentBelow {
			loPoly = append(loPoly, current)
		} else {
			hiPoly = append(hiPoly, current)
		}
		if currentBelow != nextBelow {
			// The crossing point belongs to both sides, so both keep the same
			// edge and the surfaces stay joined to their caps.
			p := lerpToPlane(current, next, axis, pos)
			loPoly = append(loPoly, p)
			hiPoly = append(hiPoly, p)
			cuts = append(cuts, p)
		}
	}

	if len(cuts) < 2 {
		if len(hiPoly) == 0 {
			return []triangle{t}, nil, segment{}, false
		}
		if len(loPoly) == 0 {
			return nil, []triangle{t}, segment{}, false
		}
		// Touching the plane without crossing it: keep it whole on the side it
		// has most of itself on.
		if len(loPoly) >= len(hiPoly) {
			return []triangle{t}, nil, segment{}, false
		}
		return nil, []triangle{t}, segment{}, false
	}
	return fanPolygon(loPoly), fanPolygon(hiPoly), segment{A: cuts[0], B: cuts[1]}, true
}

// fanPolygon turns a clipped polygon into triangles. A triangle cut by a plane
// is always convex, so a fan from its first vertex is exact rather than an
// approximation.
func fanPolygon(poly []vec3) []triangle {
	if len(poly) < 3 {
		return nil
	}
	out := make([]triangle, 0, len(poly)-2)
	for i := 1; i < len(poly)-1; i++ {
		t := triangle{A: poly[0], B: poly[i], C: poly[i+1]}
		if !degenerateTriangle(t) {
			out = append(out, t)
		}
	}
	return out
}

// degenerateTriangle reports a face with no area — which a clip produces
// whenever a vertex sits exactly on the plane. Keeping them would leave edges
// that pair with nothing and make a closed surface look open.
func degenerateTriangle(t triangle) bool {
	return quant(t.A) == quant(t.B) || quant(t.B) == quant(t.C) || quant(t.A) == quant(t.C)
}

// buildLoops chains the cut segments into closed outlines, and reports what
// fraction of them ended up in one.
func buildLoops(seams []segment) ([][]vec3, float64) {
	type endpoint struct{ seam, side int }
	index := map[qv][]endpoint{}
	for i, s := range seams {
		index[quant(s.A)] = append(index[quant(s.A)], endpoint{i, 0})
		index[quant(s.B)] = append(index[quant(s.B)], endpoint{i, 1})
	}
	used := make([]bool, len(seams))
	var loops [][]vec3
	inLoops := 0

	for start := range seams {
		if used[start] {
			continue
		}
		used[start] = true
		loop := []vec3{seams[start].A, seams[start].B}
		count := 1
		current := seams[start].B

		for step := 0; step < len(seams); step++ {
			next := -1
			for _, candidate := range index[quant(current)] {
				if used[candidate.seam] {
					continue
				}
				next = candidate.seam
				if candidate.side == 0 {
					current = seams[next].B
				} else {
					current = seams[next].A
				}
				break
			}
			if next < 0 {
				break
			}
			used[next] = true
			count++
			loop = append(loop, current)
			if quant(current) == quant(loop[0]) {
				break
			}
		}
		if len(loop) >= 4 && quant(loop[len(loop)-1]) == quant(loop[0]) {
			loops = append(loops, loop[:len(loop)-1])
			inLoops += count
		}
	}
	if len(seams) == 0 {
		return nil, 0
	}
	return loops, float64(inLoops) / float64(len(seams))
}

// fanTriangulate closes an outline with a fan from its centroid. At a genuine
// join the outline is small and close to convex, which is exactly where a fan
// is the right answer rather than a compromise.
func fanTriangulate(loop []vec3, axis int, flip bool) []triangle {
	if len(loop) < 3 {
		return nil
	}
	var centre vec3
	for _, p := range loop {
		centre.X += p.X
		centre.Y += p.Y
		centre.Z += p.Z
	}
	n := float64(len(loop))
	centre.X, centre.Y, centre.Z = centre.X/n, centre.Y/n, centre.Z/n

	out := make([]triangle, 0, len(loop))
	for i := range loop {
		a, b := loop[i], loop[(i+1)%len(loop)]
		if flip {
			a, b = b, a
		}
		out = append(out, triangle{A: centre, B: a, C: b})
	}
	return out
}

func axisValue(v vec3, axis int) float64 {
	switch axis {
	case 0:
		return v.X
	case 1:
		return v.Y
	}
	return v.Z
}

func lerpToPlane(p, q vec3, axis int, pos float64) vec3 {
	pv, qv := axisValue(p, axis), axisValue(q, axis)
	if qv == pv {
		return p
	}
	t := (pos - pv) / (qv - pv)
	return vec3{
		X: p.X + (q.X-p.X)*t,
		Y: p.Y + (q.Y-p.Y)*t,
		Z: p.Z + (q.Z-p.Z)*t,
	}
}

func axisRange(tris []triangle, axis int) (low, high float64) {
	low, high = math.Inf(1), math.Inf(-1)
	for _, t := range tris {
		for _, v := range []vec3{t.A, t.B, t.C} {
			value := axisValue(v, axis)
			low, high = math.Min(low, value), math.Max(high, value)
		}
	}
	return low, high
}

func distance(a, b vec3) float64 {
	dx, dy, dz := a.X-b.X, a.Y-b.Y, a.Z-b.Z
	return math.Sqrt(dx*dx + dy*dy + dz*dz)
}

func median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	return sorted[len(sorted)/2]
}

// SeparateOversizedUnions is S.O.G looking again at the pieces the plate
// splitter could not place.
//
// It only ever examines a piece that does not fit, and only replaces it when
// the separation actually produces parts that do. A piece it cannot explain as
// a union comes back exactly as it went in, so the worst case is the behaviour
// there was before: the caller reports it as too large and says so.
//
// Several rounds, because an assembly is often more than two objects in a row —
// a sprue with four parts on it comes apart one join at a time.
func SeparateOversizedUnions(pieces []Piece, usable [3]float64) (out []Piece, separated int) {
	const maxRounds = 4
	out = append(out, pieces...)
	for round := 0; round < maxRounds; round++ {
		changed := false
		next := make([]Piece, 0, len(out)+1)
		for _, piece := range out {
			if PieceFits(piece, usable) {
				next = append(next, piece)
				continue
			}
			split, ok := SplitUnionedObjects(piece, usable)
			if !ok {
				next = append(next, piece)
				continue
			}
			next = append(next, split.Pieces...)
			separated++
			changed = true
		}
		out = next
		if !changed {
			break
		}
	}
	return out, separated
}
