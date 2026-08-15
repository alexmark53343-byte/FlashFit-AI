package shared

import "sync"

// The interface's view of the last recommendation, published safely.
//
// These three values are what the checks panel draws: what the model
// recognised, what the readiness check predicted, and what S.O.G did. They are
// written while a recommendation is computed and read while the window paints,
// and those two happen on different threads — the import runs in its own
// goroutine while the UI keeps repainting.
//
// Read as plain package variables, that was a data race. Each value is a struct
// carrying slices (the repairs, the predicted issues, the named risks), so a
// paint that read one mid-write could see a slice header whose pointer and
// length came from different assignments — an out-of-bounds waiting to happen,
// not merely a stale number.
//
// The fix is publish-and-snapshot. A writer builds the whole value and stores
// it under the lock in one step; it never mutates a slice already published. A
// reader takes the lock just long enough to copy the struct out. Because the
// published slices are never changed after the fact, the copy the reader walks
// away with is a consistent snapshot even after the writer moves on.
var (
	uiStateMu          sync.RWMutex
	lastAdvisorOutcome AdvisorOutcome
	lastPrintReadiness PrintReadiness
	lastSOGVerdict     SOGVerdict
)

// LastAdvisorOutcome returns a snapshot of the model's last contribution.
func LastAdvisorOutcome() AdvisorOutcome {
	uiStateMu.RLock()
	defer uiStateMu.RUnlock()
	return lastAdvisorOutcome
}

// LastPrintReadiness returns a snapshot of the last readiness verdict.
func LastPrintReadiness() PrintReadiness {
	uiStateMu.RLock()
	defer uiStateMu.RUnlock()
	return lastPrintReadiness
}

// LastSOGVerdict returns a snapshot of what S.O.G last did.
func LastSOGVerdict() SOGVerdict {
	uiStateMu.RLock()
	defer uiStateMu.RUnlock()
	return lastSOGVerdict
}

func publishAdvisorOutcome(o AdvisorOutcome) {
	uiStateMu.Lock()
	lastAdvisorOutcome = o
	uiStateMu.Unlock()
}

func publishPrintReadiness(r PrintReadiness) {
	uiStateMu.Lock()
	lastPrintReadiness = r
	uiStateMu.Unlock()
}

func publishSOGVerdict(v SOGVerdict) {
	uiStateMu.Lock()
	lastSOGVerdict = v
	uiStateMu.Unlock()
}
