package shared

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Regression test for a freeze: the model was consulted inline, so computing a
// recommendation blocked the UI thread for as long as the request took — up to
// the full timeout. A recommendation must return immediately no matter how slow
// the model is.
func TestAdvisorNeverBlocksCaller(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Far longer than any acceptable UI stall.
		time.Sleep(3 * time.Second)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"object\":\"test\",\"class\":\"mechanical\",\"reason\":\"x\"}"}}]}`))
	}))
	defer slow.Close()

	ResetAdvisorCache()
	ActiveAdvisorEndpoint = slow.URL
	defer func() { ActiveAdvisorEndpoint = "" }()

	a := ModelAnalysis{Category: "Oggetto grande", Extents: [3]float64{120, 100, 60}}
	printer := DefaultPrinterProfile()

	start := time.Now()
	got := applyAdvisor(presets["balanced"], a, "balanced", printer)
	elapsed := time.Since(start)

	if elapsed > 250*time.Millisecond {
		t.Fatalf("applyAdvisor ha bloccato il chiamante per %v: il modello va consultato in background", elapsed)
	}
	// While waiting it must behave exactly like the deterministic path.
	want := presets["balanced"]
	tuneForCategory(&want, a, "balanced")
	if !advisorProposalAllowed(presets["balanced"], want, "balanced") {
		want = presets["balanced"]
	}
	if got != want {
		t.Fatalf("in attesa della risposta il risultato deve essere quello deterministico")
	}
}

// Once an answer has arrived it is reused rather than re-requested, so a repaint
// does not start a fresh inference every time.
func TestAdvisorCachesAnswer(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"object\":\"staffa\",\"class\":\"mechanical\",\"reason\":\"staffa che porta carico\"}"}}]}`))
	}))
	defer server.Close()

	ResetAdvisorCache()
	ActiveAdvisorEndpoint = server.URL
	defer func() { ActiveAdvisorEndpoint = "" }()

	a := ModelAnalysis{Category: "Oggetto tecnico/decorativo", Extents: [3]float64{60, 40, 30}}
	printer := DefaultPrinterProfile()
	base := presets["balanced"]

	applyAdvisor(base, a, "balanced", printer) // starts the request
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) && AdvisorIsThinking() {
		time.Sleep(20 * time.Millisecond)
	}
	if AdvisorIsThinking() {
		t.Fatal("la richiesta non si è conclusa")
	}

	for i := 0; i < 5; i++ {
		applyAdvisor(base, a, "balanced", printer)
	}
	if calls != 1 {
		t.Fatalf("il modello è stato interrogato %d volte invece di una: la cache non funziona", calls)
	}
	if outcome := LastAdvisorOutcome; !outcome.Used || outcome.Object != "staffa" {
		t.Fatalf("l'oggetto riconosciuto non è arrivato alla UI: %+v", outcome)
	}
}
