package shared

import "math"

// The machine manual: one place that answers "what will this printer actually
// do", consulted by the guardrail before a project is laid out and by S.O.G
// before a profile is cleared.
//
// The limits were there already, but scattered — a build volume read raw in one
// place, an acceleration ceiling multiplied by a hand-written 0.55 in another,
// and nothing at all about the layer heights a nozzle can physically lay down.
// Scattered limits drift: the ringing ceiling and the repair that aims at it
// lived in different files, and a plate check and a plate packer disagreed
// about how much of the bed is usable. Gathering them means a machine has one
// description, and both layers read the same one.
//
// Two kinds of number live here, and the difference matters:
//
//   - Machine facts come from PrinterProfile, which carries a link to the
//     manufacturer's own technical page in OfficialTechnicalSource. Build
//     volume, temperature ceilings, acceleration, kinematics.
//   - Derived limits are standard practice applied to those facts — the layer
//     range a nozzle can lay, the margin a bed loses to clips and the prime
//     line. These are FlashFit's rules, not vendor specifications, and they are
//     deliberately conservative because being wrong costs a failed print.
//
// Nothing here is invented per machine. A number this file cannot derive from
// the profile or from physics is not stated at all.

// PrinterManual is the resolved envelope for one machine.
type PrinterManual struct {
	Printer PrinterProfile

	// UsablePlate is what a part may actually occupy: the build volume less the
	// area the machine spends on itself. Checking a part against the raw volume
	// passes a 220 mm part on a 220 mm bed, which does not print.
	UsablePlate [3]float64
	// PlateMargin is what was taken off each horizontal axis, for explaining.
	PlateMargin [2]float64

	// MinLayer and MaxLayer are what the fitted nozzle can lay down. A 0.8 mm
	// nozzle cannot print a 0.12 mm layer at all: the extrusion has nowhere to
	// go and the wall tears. Nothing enforced this before, so a fine quality
	// tier on a wide nozzle produced a profile the machine could not run.
	MinLayer, MaxLayer float64
	// MinLineWidth and MaxLineWidth bound extrusion width for the same reason.
	MinLineWidth, MaxLineWidth float64

	// RingingSensitivity scales the acceleration a part may see before the
	// frame starts echoing corners into the surface. A moving bed carries the
	// part itself, so it rings at accelerations a fixed bed absorbs.
	RingingSensitivity float64
}

// Derived-limit constants, with the reasoning that fixes each one.
const (
	// A layer thinner than a quarter of the nozzle cannot be laid: the melt has
	// nowhere to go and the extruder grinds. Thicker than three quarters and
	// the bead has no shoulder to bond to the layer below.
	layerFloorOfNozzle   = 0.25
	layerCeilingOfNozzle = 0.75
	// Extrusion width below the nozzle bore is not achievable, and much above
	// it stops being a controlled bead.
	widthFloorOfNozzle   = 1.00
	widthCeilingOfNozzle = 1.50
	// Every bed loses a strip to clips, the prime line and the edge the nozzle
	// cannot reach squarely. Kept small so it never refuses a part that would
	// have printed, and never zero, because zero is the value that lets a part
	// exactly the size of the bed through.
	plateEdgeMarginMM = 4.0
	// A moving bed adds its own travel, so the usable depth shrinks further.
	bedslingerExtraMarginMM = 6.0
)

// ManualFor resolves the envelope for a machine.
func ManualFor(p PrinterProfile) PrinterManual {
	m := PrinterManual{Printer: p, UsablePlate: p.BuildVolume}

	marginX, marginY := plateEdgeMarginMM, plateEdgeMarginMM
	if p.Motion == "bedslinger" {
		// The part travels with the bed, so the axis it travels along gives up
		// more than the others.
		marginY += bedslingerExtraMarginMM
	}
	m.PlateMargin = [2]float64{marginX, marginY}
	m.UsablePlate[0] = math.Max(0, p.BuildVolume[0]-2*marginX)
	m.UsablePlate[1] = math.Max(0, p.BuildVolume[1]-2*marginY)
	// Height loses nothing horizontal, but the last millimetres are where a
	// part is most likely to touch the gantry.
	m.UsablePlate[2] = math.Max(0, p.BuildVolume[2]-plateEdgeMarginMM)

	nozzle := p.NozzleDiameter
	if nozzle <= 0 {
		nozzle = 0.4
	}
	m.MinLayer = roundTo(nozzle*layerFloorOfNozzle, 0.01)
	m.MaxLayer = roundTo(nozzle*layerCeilingOfNozzle, 0.01)
	m.MinLineWidth = roundTo(nozzle*widthFloorOfNozzle, 0.01)
	m.MaxLineWidth = roundTo(nozzle*widthCeilingOfNozzle, 0.01)

	m.RingingSensitivity = 1.0
	if p.Motion == "bedslinger" {
		m.RingingSensitivity = 1.35
	}
	return m
}

// AccelerationCeiling is the acceleration a part of this height may see before
// corners start echoing into the surface.
//
// This is the single definition of that limit. The readiness check reports
// against it and S.O.G repairs down to it, so the two cannot disagree about
// where the line is — which they could when each carried its own arithmetic.
func (m PrinterManual) AccelerationCeiling(heightMM float64, thinOrTall bool) float64 {
	// Better than half the machine's rated maximum is where ringing starts to
	// be visible on a wall, on a frame that is otherwise in good order.
	ceiling := m.Printer.MaxAcceleration * 0.55
	if heightMM > 120 {
		// A tall part gives the frame more leverage over the same impulse.
		ceiling *= 0.75
	}
	if thinOrTall {
		ceiling *= 0.8
	}
	return ceiling / m.RingingSensitivity
}

// LayerFits reports whether a layer height is one this nozzle can lay, and
// returns the nearest height that is.
func (m PrinterManual) LayerFits(layer float64) (float64, bool) {
	switch {
	case layer < m.MinLayer:
		return m.MinLayer, false
	case layer > m.MaxLayer:
		return m.MaxLayer, false
	}
	return layer, true
}

// FitsPlate reports whether a part of these extents fits the usable plate.
func (m PrinterManual) FitsPlate(extents [3]float64) bool {
	for axis := 0; axis < 3; axis++ {
		if extents[axis] > m.UsablePlate[axis]+0.01 {
			return false
		}
	}
	return true
}

func roundTo(v, step float64) float64 {
	if step <= 0 {
		return v
	}
	return math.Round(v/step) * step
}
