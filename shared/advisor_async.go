package shared

import (
	"fmt"
	"sync"
)

// Asking the model must never block the caller.
//
// The recommendation is computed on the UI thread, and a local 1.5B model needs
// seconds to answer. Calling it inline froze the window for as long as the
// request took — up to the whole timeout when the server was busy. So the model
// is consulted off to one side: a recommendation always returns immediately
// using the deterministic rules, and when an answer arrives it is cached and
// the host is told to recompute, at which point the advice is applied.
//
// The cache is keyed by everything the answer depends on, so the same part at
// the same settings is asked about once, not once per repaint.

type advisorCacheKey struct {
	Category string
	Extents  [3]int // rounded to mm; sub-millimetre changes are not worth a re-ask
	Quality  string
	Priority string
	Walls    int
	Infill   int
}

type advisorCacheEntry struct {
	deltas advisorDeltas
	valid  bool
}

var (
	advisorMu       sync.Mutex
	advisorAnswers  = map[advisorCacheKey]advisorCacheEntry{}
	advisorInFlight = map[advisorCacheKey]bool{}
	advisorThinking bool

	// AdvisorNotify is called when an answer arrives, so the host can recompute
	// and show it. The host sets this; it is never called on the UI thread.
	AdvisorNotify func()
)

func advisorKeyFor(a ModelAnalysis, quality string, p qualityPreset) advisorCacheKey {
	return advisorCacheKey{
		Category: a.Category,
		Extents:  [3]int{int(a.Extents[0]), int(a.Extents[1]), int(a.Extents[2])},
		Quality:  quality,
		Priority: AdvisorPriority,
		Walls:    p.Walls,
		Infill:   p.InfillPct,
	}
}

// AdvisorIsThinking reports whether a request is currently outstanding, for the
// status indicator.
func AdvisorIsThinking() bool {
	advisorMu.Lock()
	defer advisorMu.Unlock()
	return advisorThinking
}

// lookupOrRequestAdvice returns a cached answer if there is one. Otherwise it
// starts a background request (unless one is already running for this key) and
// reports that nothing is available yet.
func lookupOrRequestAdvice(cfg AdvisorConfig, a ModelAnalysis, quality string, base qualityPreset) (advisorDeltas, bool) {
	if !cfg.Enabled {
		return advisorDeltas{}, false
	}
	key := advisorKeyFor(a, quality, base)

	advisorMu.Lock()
	if entry, ok := advisorAnswers[key]; ok {
		advisorMu.Unlock()
		return entry.deltas, entry.valid
	}
	if advisorInFlight[key] {
		advisorMu.Unlock()
		return advisorDeltas{}, false
	}
	advisorInFlight[key] = true
	advisorThinking = true
	advisorMu.Unlock()

	go func() {
		_, deltas, ok := proposeWithModelDetailed(cfg, a, quality, base)

		advisorMu.Lock()
		advisorAnswers[key] = advisorCacheEntry{deltas: deltas, valid: ok}
		delete(advisorInFlight, key)
		advisorThinking = len(advisorInFlight) > 0
		notify := AdvisorNotify
		advisorMu.Unlock()

		if notify != nil {
			notify()
		}
	}()
	return advisorDeltas{}, false
}

// ResetAdvisorCache drops every stored answer. Used when the model or the
// user's priority changes and old advice no longer applies.
func ResetAdvisorCache() {
	advisorMu.Lock()
	advisorAnswers = map[advisorCacheKey]advisorCacheEntry{}
	advisorMu.Unlock()
}

// AdvisorCacheStats is used by the tests and the diagnostics line.
func AdvisorCacheStats() string {
	advisorMu.Lock()
	defer advisorMu.Unlock()
	return fmt.Sprintf("%d risposte, %d in corso", len(advisorAnswers), len(advisorInFlight))
}
