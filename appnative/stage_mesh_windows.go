//go:build windows

package main

import (
	"math"

	"flashfitai/shared"
)

// Triangle soups carry no normals, so the stage derives them. Corners that meet
// under the crease angle are averaged into one smooth normal; anything sharper
// keeps its own. Without this every surface renders faceted, which is what makes
// software-rendered previews look like folded paper.

const stageCreaseCos = 0.55 // ~57°

type shadedTriangle struct {
	P [3]stageVec
	N [3]stageVec
}

type meshCorner struct {
	face int32
	slot int8
}

type quantizedKey struct{ X, Y, Z int64 }

func vecSub(a, b stageVec) stageVec { return stageVec{a.X - b.X, a.Y - b.Y, a.Z - b.Z} }

func vecCross(a, b stageVec) stageVec {
	return stageVec{a.Y*b.Z - a.Z*b.Y, a.Z*b.X - a.X*b.Z, a.X*b.Y - a.Y*b.X}
}

func vecDot(a, b stageVec) float32 { return a.X*b.X + a.Y*b.Y + a.Z*b.Z }

func vecNormalize(v stageVec) stageVec {
	length := float32(math.Sqrt(float64(vecDot(v, v))))
	if length == 0 {
		return stageVec{0, 0, 1}
	}
	return stageVec{v.X / length, v.Y / length, v.Z / length}
}

func buildShadedMesh(tris []shared.PreviewTriangle) []shadedTriangle {
	if len(tris) == 0 {
		return nil
	}
	positions := make([][3]stageVec, len(tris))
	faceNormals := make([]stageVec, len(tris))
	minX, minY, minZ := float32(math.MaxFloat32), float32(math.MaxFloat32), float32(math.MaxFloat32)
	maxX, maxY, maxZ := float32(-math.MaxFloat32), float32(-math.MaxFloat32), float32(-math.MaxFloat32)

	for i, t := range tris {
		p := [3]stageVec{{t.AX, t.AY, t.AZ}, {t.BX, t.BY, t.BZ}, {t.CX, t.CY, t.CZ}}
		positions[i] = p
		// Unnormalized so the accumulation stays area weighted.
		faceNormals[i] = vecCross(vecSub(p[1], p[0]), vecSub(p[2], p[0]))
		for _, v := range p {
			if v.X < minX {
				minX = v.X
			}
			if v.Y < minY {
				minY = v.Y
			}
			if v.Z < minZ {
				minZ = v.Z
			}
			if v.X > maxX {
				maxX = v.X
			}
			if v.Y > maxY {
				maxY = v.Y
			}
			if v.Z > maxZ {
				maxZ = v.Z
			}
		}
	}

	span := float64(maxX - minX)
	if float64(maxY-minY) > span {
		span = float64(maxY - minY)
	}
	if float64(maxZ-minZ) > span {
		span = float64(maxZ - minZ)
	}
	if span <= 0 {
		span = 1
	}
	// Welding tolerance: fine enough to keep detail, coarse enough that exported
	// meshes with per-face duplicated vertices still join up.
	quantum := span / 20000

	groups := make(map[quantizedKey][]meshCorner, len(tris))
	key := func(v stageVec) quantizedKey {
		return quantizedKey{
			X: int64(math.Round(float64(v.X) / quantum)),
			Y: int64(math.Round(float64(v.Y) / quantum)),
			Z: int64(math.Round(float64(v.Z) / quantum)),
		}
	}
	for i := range tris {
		for slot := 0; slot < 3; slot++ {
			k := key(positions[i][slot])
			groups[k] = append(groups[k], meshCorner{face: int32(i), slot: int8(slot)})
		}
	}

	out := make([]shadedTriangle, len(tris))
	for i := range tris {
		out[i].P = positions[i]
	}

	var clusterSums []stageVec
	var clusterOf []int
	for _, corners := range groups {
		clusterSums = clusterSums[:0]
		clusterOf = clusterOf[:0]
		for _, c := range corners {
			n := vecNormalize(faceNormals[c.face])
			assigned := -1
			for index, sum := range clusterSums {
				if vecDot(vecNormalize(sum), n) >= stageCreaseCos {
					assigned = index
					break
				}
			}
			if assigned < 0 {
				clusterSums = append(clusterSums, faceNormals[c.face])
				assigned = len(clusterSums) - 1
			} else {
				s := clusterSums[assigned]
				clusterSums[assigned] = stageVec{s.X + faceNormals[c.face].X, s.Y + faceNormals[c.face].Y, s.Z + faceNormals[c.face].Z}
			}
			clusterOf = append(clusterOf, assigned)
		}
		for index, c := range corners {
			out[c.face].N[c.slot] = vecNormalize(clusterSums[clusterOf[index]])
		}
	}
	return out
}
