//go:build windows

package main

import "math"

// Texture swatches are rendered as genuinely lit surfaces rather than drawn as
// line art: each finish defines a height field, the height field gives a normal,
// and the normal is lit. That is what makes a satin sheen look anisotropic and a
// carbon weave look woven instead of hatched.

type texturePreviewKey struct {
	Kind          string
	Width, Height int32
	Accent        uint32
	Dark          bool
}

var texturePreviewCache = map[texturePreviewKey]spatialCachedBitmap{}

const texturePreviewCacheLimit = 24

// heightAt returns the surface height in [0,1] for a finish at a point.
func textureHeight(kind string, x, y float64) float64 {
	switch kind {
	case "satin":
		// Fine parallel grooves plus a slow waviness, so the highlight smears
		// along the grain the way brushed and satin finishes do.
		grain := math.Sin(y*2.6) * 0.5
		micro := math.Sin(y*23.0+math.Sin(x*0.7)*1.4) * 0.5
		return 0.5 + grain*0.32 + micro*0.14

	case "prism":
		// Triangular facets: quantise into a triangular lattice and give each
		// cell a flat but distinct height.
		const cell = 26.0
		u := x / cell
		v := y / cell
		fu := u - math.Floor(u)
		fv := v - math.Floor(v)
		upper := 0.0
		if fu+fv > 1 {
			upper = 1
		}
		seed := math.Floor(u)*2.7 + math.Floor(v)*4.1 + upper*1.9
		return 0.5 + math.Sin(seed)*0.45

	case "carbon":
		// 2x2 twill: strands run one way in half the cells and the other way in
		// the rest, each strand rounded across its width.
		const strand = 13.0
		cu := math.Floor(x / strand)
		cv := math.Floor(y / strand)
		warp := math.Mod(cu+cv, 2.0) < 1
		across := math.Mod(y, strand) / strand
		if warp {
			across = math.Mod(x, strand) / strand
		}
		// Rounded profile across the strand gives the woven relief.
		return 0.25 + math.Sin(across*math.Pi)*0.75

	default: // topographic
		// Smooth landscape, then contour terraces cut into it.
		field := math.Sin(x*0.045)*math.Cos(y*0.052) + math.Sin((x+y)*0.021)*0.6
		normalized := (field + 1.6) / 3.2
		const bands = 9.0
		terraced := math.Floor(normalized*bands) / bands
		// Keep a little of the smooth field so terraces are not perfectly flat.
		return terraced*0.82 + normalized*0.18
	}
}

// textureShade lights a height field sample and returns the surface colour.
func textureShade(kind string, x, y float64, ar, ag, ab float64, dark bool) (uint8, uint8, uint8) {
	const delta = 1.0
	here := textureHeight(kind, x, y)
	dx := textureHeight(kind, x+delta, y) - here
	dy := textureHeight(kind, x, y+delta) - here

	// Relief strength differs per finish: a weave stands proud, satin barely.
	relief := 3.4
	switch kind {
	case "satin":
		relief = 1.5
	case "prism":
		relief = 5.0
	case "carbon":
		relief = 4.2
	}
	nx, ny, nz := -dx*relief, -dy*relief, 1.0
	length := math.Sqrt(nx*nx + ny*ny + nz*nz)
	nx, ny, nz = nx/length, ny/length, nz/length

	// Key light from the upper left, as everywhere else in the interface.
	lx, ly, lz := -0.42, -0.62, 0.66
	diffuse := math.Max(nx*lx+ny*ly+nz*lz, 0)

	// Blinn-Phong highlight. Satin gets a wide, stretched one; prism a tight one.
	hx, hy, hz := lx, ly, lz+1.0
	hLength := math.Sqrt(hx*hx + hy*hy + hz*hz)
	specAngle := math.Max(nx*hx/hLength+ny*hy/hLength+nz*hz/hLength, 0)
	power, specWeight := 26.0, 0.5
	switch kind {
	case "satin":
		power, specWeight = 9.0, 0.72
	case "prism":
		power, specWeight = 60.0, 0.85
	case "carbon":
		power, specWeight = 34.0, 0.45
	case "topographic":
		power, specWeight = 14.0, 0.3
	}
	specular := math.Pow(specAngle, power) * specWeight

	ambient := 0.30
	if dark {
		ambient = 0.20
	}
	lit := ambient + diffuse*0.72

	// Prism shifts hue across facets; the others stay on the accent.
	r, g, b := ar, ag, ab
	if kind == "prism" {
		shift := textureHeight(kind, x, y)
		r = ar*(0.55+shift*0.9) + 90*(1-shift)
		g = ag * (0.62 + shift*0.7)
		b = ab*(0.72+shift*0.5) + 60*shift
	}
	if kind == "topographic" {
		// Higher ground reads warmer, so the terraces separate by eye.
		high := textureHeight(kind, x, y)
		r = ar*(0.6+high*0.8) + 70*high
		g = ag * (0.7 + high*0.5)
		b = ab * (1.05 - high*0.35)
	}

	out := func(channel float64) uint8 {
		value := channel*lit + 255*specular
		return uint8(spatialClamp(value, 0, 255))
	}
	return out(r), out(g), out(b)
}

func texturePreviewBitmap(kind string, w, h int32) spatialCachedBitmap {
	key := texturePreviewKey{Kind: kind, Width: w, Height: h, Accent: uint32(th.accent), Dark: th.dark}
	if bitmap, ok := texturePreviewCache[key]; ok {
		return bitmap
	}
	if len(texturePreviewCache) > texturePreviewCacheLimit {
		clearTexturePreviewCache()
	}
	ar, ag, ab := spatialColorChannels(th.accent)
	baseR, baseG, baseB := float64(ar), float64(ag), float64(ab)
	// Lift the base towards a light material so the finish, not the hue, reads.
	baseR = baseR*0.45 + 150
	baseG = baseG*0.45 + 156
	baseB = baseB*0.45 + 170

	radius := 18.0
	bitmap := createSpatialBitmap(w, h, func(x, y int32) (uint8, uint8, uint8, uint8) {
		coverage := spatialCoverage(spatialRoundedDistance(float64(x)+0.5, float64(y)+0.5, float64(w), float64(h), radius))
		if coverage <= 0 {
			return 0, 0, 0, 0
		}
		r, g, b := textureShade(kind, float64(x), float64(y), baseR, baseG, baseB, th.dark)
		// Gentle vignette keeps the swatch from fighting the card around it.
		fx := (float64(x)/float64(w) - 0.5) * 2
		fy := (float64(y)/float64(h) - 0.5) * 2
		vignette := 1 - spatialClamp((fx*fx+fy*fy)*0.22, 0, 0.42)
		return uint8(float64(r) * vignette), uint8(float64(g) * vignette), uint8(float64(b) * vignette),
			uint8(spatialClamp(coverage*255, 0, 255))
	})
	texturePreviewCache[key] = bitmap
	return bitmap
}

func clearTexturePreviewCache() {
	for _, bitmap := range texturePreviewCache {
		if bitmap.Handle != 0 {
			pDeleteObject.Call(bitmap.Handle)
		}
	}
	texturePreviewCache = map[texturePreviewKey]spatialCachedBitmap{}
}
