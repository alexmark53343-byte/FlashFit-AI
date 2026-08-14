package shared

import (
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Everything the model says passes through here before it can reach a profile
// or the screen.
//
// The veto in recommend.go guards *magnitude*: it refuses advice that leaves
// the envelope or costs more time than the tier allows. That is a real
// guarantee, but a one-sided one — it stops the model asking for too much and
// waves through anything that asks for less.
//
// A wrong class is almost always the cheap direction. "hollow" on a solid
// load-bearing bracket takes six points of infill *out* and slows the print
// down: safe by every rule above, inside the budget by construction, and
// wrong. That is the shape of the failure that once put a vase profile on a
// Porsche, and no amount of prompt wording closes it, because the prompt is a
// request and this is a check.
//
// So the reply is checked here on three counts, in this order:
//
//  1. Is the class a word we actually defined? Anything else is not a quiet
//     "no change", it is an unusable answer.
//  2. Does the mesh support the claim? A class the measurements flatly deny is
//     dropped, while the object name the model read off the file is kept.
//  3. Is the text fit to display? Model output is untrusted input like any
//     other, and it ends up in a label.

// advisorClasses is the whole vocabulary. The prompt asks for exactly one of
// these; this is where that stops being a request.
//
// A word outside the set is not treated as "unknown". The two are genuinely
// different: "unknown" is the model doing as it was told and declining to
// guess, while "cylindrical" is the model ignoring the contract, and an answer
// that ignores one instruction is not evidence for having followed the others.
var advisorClasses = map[string]bool{
	"hollow":     true,
	"decorative": true,
	"mechanical": true,
	"slender":    true,
	"unknown":    true,
}

// Thresholds for the contradiction check.
//
// These are deliberately far looser than the guidance in the prompt. The job
// here is not to grade the classification — the model sees the file name and
// the whole descriptor set, and it is allowed to weigh them. The job is to
// catch a claim the geometry rules out, so each bound sits well clear of any
// plausible honest answer and only fires on the flat contradictions.
const (
	// A vase is near 0.10 and even a thick-walled cup stays under 0.30. At
	// 0.45 nothing that is really a shell gets refused.
	hollowSolidityCeiling = 0.45
	// Under a tenth of its own bounding box there is no body to reinforce, so
	// adding density is buying nothing. Frame-like brackets sit far above this.
	mechanicalSolidityFloor = 0.10
	// The prompt asks for elongation above 4 or a height ratio above 2. A part
	// that reaches neither of these lower figures is not slender by any reading.
	slenderElongationFloor = 2.5
	slenderFlatnessFloor   = 1.5
)

// How much model-written text may reach a label. Long enough for the answers
// the prompt asks for, short enough that a rambling reply cannot take the row
// over.
const (
	advisorObjectMaxRunes = 40
	advisorReasonMaxRunes = 120
)

// shapeFacts are the derived proportions, computed once and used both to
// describe the mesh to the model and to check what it says back. Deriving them
// twice would let the description and the check drift apart, which is the one
// way this guard could quietly stop guarding.
type shapeFacts struct {
	Solidity   float64
	Shell      float64
	Elongation float64
	Flatness   float64
	// Volume comes from the mesh, and a mesh with holes has no reliable volume.
	// Everything derived from it has to be treated as absent rather than as
	// zero, or a broken STL reads as an infinitely thin shell.
	VolumeTrusted bool
}

func shapeFactsOf(a ModelAnalysis) shapeFacts {
	x, y, z := a.Extents[0], a.Extents[1], a.Extents[2]
	facts := shapeFacts{VolumeTrusted: a.Watertight && a.Volume > 0}

	if box := x * y * z; box > 0 && a.Volume > 0 {
		facts.Solidity = a.Volume / box
	}
	// Surface area per unit volume, normalised by size so it compares across
	// scales. High means thin-walled or highly detailed.
	if a.Volume > 0 {
		facts.Shell = a.SurfaceArea / math.Pow(a.Volume, 2.0/3.0)
	}
	longest := math.Max(x, math.Max(y, z))
	if shortest := math.Min(x, math.Min(y, z)); shortest > 0 {
		facts.Elongation = longest / shortest
	}
	if wide := math.Max(x, y); wide > 0 {
		facts.Flatness = z / wide
	}
	return facts
}

// classContradiction reports the measurement that rules a class out, or "" when
// the mesh does not object.
//
// "decorative" has no entry on purpose. Anything at all can be made to be
// looked at, so there is no measurement that refutes it, and inventing one
// would mean overruling the model on the one judgement it is better placed to
// make than a threshold is.
func classContradiction(class string, s shapeFacts) string {
	switch class {
	case "hollow":
		if s.VolumeTrusted && s.Solidity > hollowSolidityCeiling {
			return "solidità " + trimFloat(s.Solidity) + ": è un corpo pieno, non un guscio"
		}
	case "mechanical":
		if s.VolumeTrusted && s.Solidity < mechanicalSolidityFloor {
			return "solidità " + trimFloat(s.Solidity) + ": è un guscio, non c'è materiale da rinforzare"
		}
	case "slender":
		if s.Elongation < slenderElongationFloor && s.Flatness < slenderFlatnessFloor {
			return "proporzioni " + trimFloat(s.Elongation) + " e " + trimFloat(s.Flatness) + ": non è né lungo né alto"
		}
	}
	return ""
}

// trimFloat renders a proportion for a one-line explanation: two decimals, and
// no trailing zeros to read past.
func trimFloat(v float64) string {
	text := strconv.FormatFloat(v, 'f', 2, 64)
	text = strings.TrimRight(text, "0")
	return strings.TrimSuffix(text, ".")
}

// sanitiseAdvisorText makes model output fit to put in a label.
//
// The reply is untrusted input. It is also the one string in the app written by
// something other than the app, so it gets the treatment any such string gets:
// no control characters, no line breaks smuggled into a single-line row, no
// unbounded length, and no leftover JSON when the model wraps its answer badly
// enough that the brace-matching parser recovers a fragment.
func sanitiseAdvisorText(s string, maxRunes int) string {
	if !utf8.ValidString(s) {
		return ""
	}
	cleaned := strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			return ' '
		case unicode.IsControl(r):
			return -1
		case r == '{' || r == '}' || r == '"':
			// Structure leaking out of the reply, not part of an answer.
			return -1
		}
		return r
	}, s)
	cleaned = strings.Join(strings.Fields(cleaned), " ")
	if cleaned == "" {
		return ""
	}
	if utf8.RuneCountInString(cleaned) <= maxRunes {
		return cleaned
	}
	// Cut on a rune boundary, and prefer a word boundary when there is one
	// close enough that the result still says something.
	truncated := string([]rune(cleaned)[:maxRunes])
	if cut := strings.LastIndexByte(truncated, ' '); cut > len(truncated)/2 {
		truncated = truncated[:cut]
	}
	return strings.TrimSpace(truncated) + "…"
}

// vetReply is the single door between the model and everything downstream. It
// returns the reply with its text cleaned and its class either confirmed or
// withdrawn, the measurement that withdrew it, and whether the answer is usable
// at all.
func vetReply(d advisorDeltas, a ModelAnalysis) (advisorDeltas, string, bool) {
	class := strings.ToLower(strings.TrimSpace(d.Class))
	if !advisorClasses[class] {
		// Off-contract answer: not a class we can act on, and not a signal we
		// should read anything else from either.
		return advisorDeltas{}, "", false
	}
	d.Class = class
	d.Object = sanitiseAdvisorText(d.Object, advisorObjectMaxRunes)
	d.Reason = sanitiseAdvisorText(d.Reason, advisorReasonMaxRunes)

	// The finish word is treated more leniently than the class, on purpose.
	//
	// An unrecognised class has no safe default: it drives the setting deltas,
	// so guessing one would be inventing an answer the model did not give. An
	// unrecognised finish does have one — "unknown" means S.O.G keeps its
	// baseline clearance, which is exactly what it does with no model at all.
	// Throwing away a sound identification because the second word was
	// misspelled would cost something and protect nothing.
	if finish := strings.ToLower(strings.TrimSpace(d.Finish)); sogFinishLevels[finish] {
		d.Finish = finish
	} else {
		d.Finish = "unknown"
	}

	mismatch := classContradiction(class, shapeFactsOf(a))
	if mismatch != "" {
		// The name is usually right even when the class is not — the model read
		// it off the file, which is the part it is genuinely good at. So the
		// recognition survives and only the claim about the shape is dropped.
		d.Class = "unknown"
	}
	return d, mismatch, true
}

// hasEffect reports whether the advice actually asks for anything.
//
// This exists because "no change" and "accepted" used to be the same thing, and
// they should not be. When the model answers "unknown" — which the prompt
// explicitly asks it to do rather than guess — the deltas are all zero, the
// resulting proposal equals the base preset, and the veto accepted it happily
// because a profile identical to the base is trivially safe and trivially
// within budget.
//
// The effect was that an honest abstention *replaced* the deterministic
// category tuning with nothing at all: asking the model and having it decline
// left the profile worse than never asking. Abstention has to fall through to
// the rules instead, and that needs the two cases told apart.
func (d advisorDeltas) hasEffect() bool {
	return d.Walls != 0 || d.TopLayers != 0 || d.BotLayers != 0 ||
		d.Infill != 0 || d.SpeedScale != 1
}
