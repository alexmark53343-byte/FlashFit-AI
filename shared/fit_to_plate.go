package shared

import "math"

// Fitting an oversized model to the plate.
//
// Refusing a model that does not fit is correct but unhelpful on its own: a
// downloaded model often arrives at display scale, or bundled with a base or a
// diorama, and the user's real intent is to print it as large as the machine
// allows. So instead of stopping at "too big", FlashFit works out the single
// uniform factor that makes it fit and applies it to the copy it prints —
// never to the user's own file.
//
// The scale is uniform on purpose. Squashing one axis to fit would silently
// change the shape of the part, which is a far worse surprise than a smaller
// but faithful print.

// PlateFit describes what has to happen for a model to fit a machine.
type PlateFit struct {
	Fits    bool
	Scale   float64 // 1.0 when no change is needed, otherwise < 1
	Percent int     // Scale as a whole percentage, for showing the user
	// Axis and Overshoot name the worst offender, so the message can say what
	// is actually in the way rather than just that something is.
	Axis      string
	Overshoot float64
}

// FitToPlate reports whether the model fits and, when it does not, the uniform
// scale that would make it.
func FitToPlate(extents [3]float64, volume [3]float64) PlateFit {
	fit := PlateFit{Fits: true, Scale: 1, Percent: 100}
	worst := 1.0
	for i, axis := range []string{"X", "Y", "Z"} {
		if volume[i] <= 0 || extents[i] <= 0 {
			continue
		}
		if extents[i] > volume[i]+0.01 {
			fit.Fits = false
			if over := extents[i] - volume[i]; over > fit.Overshoot {
				fit.Overshoot = over
				fit.Axis = axis
			}
		}
		if ratio := volume[i] / extents[i]; ratio < worst {
			worst = ratio
		}
	}
	if fit.Fits {
		return fit
	}
	// A whisker of margin keeps the part off the very edge of the plate, where
	// most machines cannot actually reach.
	fit.Scale = math.Floor(worst*0.98*1000) / 1000
	if fit.Scale <= 0 || fit.Scale >= 1 {
		fit.Scale = worst * 0.98
	}
	fit.Percent = int(math.Round(fit.Scale * 100))
	return fit
}

// scaleTriangles returns the mesh at a different size. The original slice is
// left alone: the caller may still need the measured geometry.
func scaleTriangles(tris []triangle, scale float64) []triangle {
	if scale == 1 || scale <= 0 {
		return tris
	}
	out := make([]triangle, len(tris))
	for i, t := range tris {
		out[i] = triangle{
			A: vec3{t.A.X * scale, t.A.Y * scale, t.A.Z * scale},
			B: vec3{t.B.X * scale, t.B.Y * scale, t.B.Z * scale},
			C: vec3{t.C.X * scale, t.C.Y * scale, t.C.Z * scale},
		}
	}
	return out
}
