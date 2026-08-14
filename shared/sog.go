package shared

import (
	"fmt"
	"math"
	"strconv"
)

// S.O.G — Security On Guardrail.
//
// The third layer, and the only one that changes a finished profile.
//
//	model      says what the part is
//	guardrail  decides whether to believe it
//	S.O.G      decides what that means for the print, and proves it is safe
//
// What it exists to fix: CheckPrintReadiness could already predict the defects
// that show on the surface — ringing behind corners, banding where the hotend
// cannot melt fast enough, sagging bridges, slumped fine layers. It only
// reported them. A predicted defect either produced a warning the user had to
// interpret, or, when it was serious, refused the import outright with "profilo
// non sicuro" and left them with nothing. Knowing the print will come out with
// ugly lines in it and handing that back as an error message is not a safety
// feature, it is a shrug.
//
// So S.O.G closes the loop. It reads the prediction, corrects the setting that
// caused it, re-runs the prediction, and repeats until the profile comes back
// clean — then, and only then, it clears the print. That is the "give the go
// ahead" step: the slicer opens on a profile that has been checked *after* its
// last change, not before it.
//
// Two properties make this safe to put in the path of every import:
//
//   - Every repair is one-way. A repair may lower a speed, an acceleration or a
//     temperature, and may add adhesion; nothing here can raise any of them.
//     So no sequence of repairs, and no answer from the model, can produce a
//     print that is faster or hotter than the one the tier already approved.
//     TestSOGOnlyEverSlowsDown holds the line.
//   - Every threshold comes from the checker, not from here. A repair aims at
//     the Limit the check itself reported, so the two cannot drift apart: if a
//     limit is ever retuned, the correction follows it with no second edit.
//
// The model's part in this, and its bound, are in sogMargin.

// SOGRepair is one correction, kept so the interface can say what was changed
// rather than silently handing back a different profile.
type SOGRepair struct {
	Issue   string  // the defect key it was answering
	Setting string  // the setting it moved
	From    float64
	To      float64
}

// SOGVerdict is the outcome of securing a profile.
type SOGVerdict struct {
	// Cleared is the go-ahead: the profile was checked after its final change
	// and nothing blocking remains.
	Cleared bool
	Passes  int
	Repairs []SOGRepair
	// Remaining are the predictions S.O.G had no correction for. Advisory ones
	// are shown; a blocking one is what keeps Cleared false.
	Remaining []PrintIssue
	// Finish is the sensitivity it worked to, and Source says whether that came
	// from the model or from the rules.
	Finish string
	Source string
	// SpeedFactor is how much slower the repaired profile is, for scaling the
	// time estimate. 1 means nothing that affects the estimate moved.
	SpeedFactor float64
}

// LastSOGVerdict is the most recent verdict, for the interface to show.
var LastSOGVerdict SOGVerdict

// sogFinishLevels is the vocabulary for the model's second recognition: how
// much the look of this part matters. Like the class, it is a question about
// what the part *is*, which is the kind the model answers well — not a
// question about numbers, which is the kind it does not.
var sogFinishLevels = map[string]bool{
	"showpiece":  true,
	"visible":    true,
	"functional": true,
	"hidden":     true,
	"unknown":    true,
}

// sogMargin converts that recognition into how much clearance S.O.G insists on
// below each limit.
//
// This is the whole of the model's influence over S.O.G, and it is deliberately
// shaped so that influence only ever points one way. The baseline is 1.0 —
// repairs land exactly on the limit the checker named — and every answer the
// model can give either leaves it there or pulls it *further* below. There is
// no word it can return, and no failure it can have, that buys back speed or
// loosens a bound.
//
// That is what makes it safe to let a 1.5B model into a safety decision at all:
// not that its answer is trusted, but that the worst case of a wrong answer is
// a print that is more careful than it needed to be.
func sogMargin(finish string) float64 {
	switch finish {
	case "showpiece":
		// The surface is the point of the part. Stay well clear of the limit,
		// because a defect at 95% of a threshold is still a defect.
		return 0.80
	case "visible":
		return 0.90
	default:
		// functional, hidden, unknown, or no model at all.
		return 1.0
	}
}

// SecureProfile is the go-ahead step: repair, re-check, repeat, then clear.
//
// It mutates the recommendation in place because the caller is about to write
// it, and returns what it did. The loop is bounded and monotone — each repair
// puts a value at or below the limit that flagged it, so no defect can flag
// twice — which is why it terminates rather than merely usually terminating.
func SecureProfile(rec *Recommendation, a ModelAnalysis, f Filament, printer PrinterProfile) SOGVerdict {
	verdict := SOGVerdict{SpeedFactor: 1, Finish: "unknown", Source: "regole"}
	if rec == nil {
		return verdict
	}
	// The model's recognition, if one arrived. It reached here through the
	// guard, so the word is already known to be in the vocabulary.
	if finish := LastAdvisorOutcome.Finish; finish != "" && finish != "unknown" {
		verdict.Finish, verdict.Source = finish, "modello"
	}
	margin := sogMargin(verdict.Finish)

	outerBefore := processFloat(rec, "outer_wall_speed")

	const maxPasses = 8
	for pass := 1; pass <= maxPasses; pass++ {
		verdict.Passes = pass
		readiness := CheckPrintReadiness(*rec, a, f, printer)
		if len(readiness.Issues) == 0 {
			verdict.Remaining = nil
			break
		}
		repaired := false
		var stuck []PrintIssue
		for _, issue := range readiness.Issues {
			if repair, ok := repairIssue(rec, issue, margin); ok {
				verdict.Repairs = append(verdict.Repairs, repair)
				repaired = true
				// One repair at a time: the next check runs against the changed
				// profile, so a correction that also settles another defect is
				// not paid for twice.
				break
			}
			stuck = append(stuck, issue)
		}
		if !repaired {
			verdict.Remaining = stuck
			break
		}
	}

	// A final check on the profile as it now stands. Clearing on the strength
	// of a check that ran before the last edit would defeat the point.
	final := CheckPrintReadiness(*rec, a, f, printer)
	LastPrintReadiness = final
	verdict.Remaining = final.Issues
	verdict.Cleared = !final.Blocked

	// The estimate is built as inversely proportional to the outer wall speed
	// (see relativeTimeFor), so a slower wall scales it by exactly this ratio.
	// Leaving the old number on screen after slowing the print down is the kind
	// of quiet lie that made the estimate untrustworthy in the first place.
	if outerAfter := processFloat(rec, "outer_wall_speed"); outerAfter > 0 && outerBefore > outerAfter {
		verdict.SpeedFactor = outerBefore / outerAfter
		rec.EstimatedModeMinutes *= verdict.SpeedFactor
		rec.EstimatedRelativeTime *= verdict.SpeedFactor
	}
	return verdict
}

// repairIssue applies the correction for one predicted defect, aiming at the
// limit the check itself reported. It returns false when there is no correction
// that would not cost more than the defect.
func repairIssue(rec *Recommendation, issue PrintIssue, margin float64) (SOGRepair, bool) {
	switch issue.Key {
	case "checkGhosting":
		// Ringing is an acceleration problem before it is a speed problem, and
		// acceleration is the cheaper of the two to give up: it costs almost
		// nothing on a long wall and everything on a corner, which is where the
		// echo comes from.
		target := issue.Limit * margin
		repair, ok := lowerProcess(rec, "outer_wall_acceleration", target, 0)
		// The bridge inherits the outer acceleration, so it has to come with it.
		lowerProcess(rec, "bridge_acceleration", target, 0)
		repair.Issue = issue.Key
		return repair, ok

	case "checkFlow":
		// The hotend cannot melt faster than this, and asking it to does not
		// speed anything up: the slicer quietly throttles, the estimate goes
		// wrong, and the extrusion bands where the flow stumbles. Scale the
		// wall speed by exactly the overshoot.
		if issue.Value <= 0 {
			return SOGRepair{}, false
		}
		current := processFloat(rec, "outer_wall_speed")
		target := current * (issue.Limit / issue.Value) * margin
		repair, ok := lowerProcess(rec, "outer_wall_speed", target, 0)
		repair.Issue = issue.Key
		return repair, ok

	case "checkBridge":
		target := issue.Limit * margin
		repair, ok := lowerProcess(rec, "bridge_speed", target, 0)
		// The steepest overhang bands are laid under the same conditions.
		lowerProcess(rec, "overhang_4_4_speed", target, 0)
		lowerProcess(rec, "overhang_3_4_speed", target+8, 0)
		repair.Issue = issue.Key
		return repair, ok

	case "checkCooling":
		// Too little time per layer for the last one to set. Slowing the wall
		// buys that time back directly.
		target := issue.Limit * margin
		repair, ok := lowerProcess(rec, "outer_wall_speed", target, 0)
		repair.Issue = issue.Key
		return repair, ok

	case "checkAdhesionRisk":
		if fmt.Sprint(rec.Process["brim_type"]) != "no_brim" {
			return SOGRepair{}, false
		}
		rec.Process["brim_type"] = "outer_only"
		rec.Process["brim_width"] = "5"
		return SOGRepair{Issue: issue.Key, Setting: "brim_type", From: 0, To: 5}, true

	case "checkTemp":
		// Over what the machine or the filament allows. There is one direction
		// this can move.
		if issue.Limit <= 0 {
			return SOGRepair{}, false
		}
		current := firstFloatFromAny(rec.Filament["nozzle_temperature"])
		if current <= issue.Limit {
			return SOGRepair{}, false
		}
		rec.Filament["nozzle_temperature"] = []string{strconv.FormatFloat(issue.Limit, 'f', 0, 64)}
		return SOGRepair{Issue: issue.Key, Setting: "nozzle_temperature", From: current, To: issue.Limit}, true
	}
	return SOGRepair{}, false
}

// lowerProcess moves a numeric process setting down to target, and refuses to
// move it up. The refusal is the one-way guarantee, expressed in the one place
// every repair goes through rather than restated in each of them.
func lowerProcess(rec *Recommendation, key string, target float64, digits int) (SOGRepair, bool) {
	current := processFloat(rec, key)
	if current <= 0 || target <= 0 || target >= current {
		return SOGRepair{Setting: key, From: current, To: current}, false
	}
	// Round down, so rounding can never land above the target.
	scale := math.Pow(10, float64(digits))
	rounded := math.Floor(target*scale) / scale
	if rounded <= 0 {
		return SOGRepair{Setting: key, From: current, To: current}, false
	}
	rec.Process[key] = strconv.FormatFloat(rounded, 'f', digits, 64)
	syncCriticalValue(rec, key, rounded)
	return SOGRepair{Setting: key, From: current, To: rounded}, true
}

// syncCriticalValue keeps the self-test's copy of a setting in step with the
// profile. CriticalValues is what verifies the written project actually carries
// what was decided, so leaving it on the pre-repair number would make the
// self-test check the wrong thing and report red on a correct project.
func syncCriticalValue(rec *Recommendation, key string, value float64) {
	if rec.CriticalValues == nil {
		return
	}
	alias := key
	if key == "outer_wall_acceleration" {
		alias = "outer_acceleration"
	}
	if _, tracked := rec.CriticalValues[alias]; tracked {
		rec.CriticalValues[alias] = value
	}
}

func processFloat(rec *Recommendation, key string) float64 {
	if rec == nil || rec.Process == nil {
		return 0
	}
	v, err := strconv.ParseFloat(fmt.Sprint(rec.Process[key]), 64)
	if err != nil {
		return 0
	}
	return v
}

// Summary is a one-line account of what S.O.G did, for the status row.
func (v SOGVerdict) Summary() string {
	switch {
	case len(v.Repairs) == 0 && v.Cleared:
		return "profilo già pulito"
	case !v.Cleared:
		return "non autorizzato"
	case len(v.Repairs) == 1:
		return "1 correzione applicata"
	default:
		return fmt.Sprintf("%d correzioni applicate", len(v.Repairs))
	}
}
