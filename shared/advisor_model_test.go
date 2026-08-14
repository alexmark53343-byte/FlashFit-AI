package shared

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// The property that matters most: with the model switched off — which is the
// default, and what every user without one has — results are byte-identical to
// the deterministic path.
func TestAdvisorDisabledChangesNothing(t *testing.T) {
	t.Setenv("FLASHFIT_ADVISOR", "")
	printer := DefaultPrinterProfile()
	for _, quality := range []string{"low", "balanced", "perfect"} {
		base := presets[quality]
		a := ModelAnalysis{Category: "Oggetto grande", Extents: [3]float64{180, 160, 90}}

		withModel := applyAdvisor(base, a, quality, printer)
		rules := base
		tuneForCategory(&rules, a, quality)
		if !advisorProposalAllowed(base, rules, quality) {
			rules = base
		}
		if withModel != rules {
			t.Fatalf("%s: con advisor disattivo il risultato differisce dal percorso deterministico", quality)
		}
	}
}

// An unreachable endpoint must be indistinguishable from having no model.
func TestAdvisorUnreachableEndpointFallsBack(t *testing.T) {
	cfg := AdvisorConfig{Enabled: true, Endpoint: "http://127.0.0.1:1/v1/chat/completions", Model: "local"}
	base := presets["balanced"]
	if _, ok := proposeWithModel(cfg, ModelAnalysis{Category: "Oggetto grande"}, "balanced", base); ok {
		t.Fatal("un endpoint irraggiungibile non deve produrre una proposta")
	}
}

// Garbage in, deterministic profile out.
func TestAdvisorRejectsUnusableReplies(t *testing.T) {
	for _, reply := range []string{
		"non lo so",
		"{",
		`{"walls_delta": "molte"}`,
		"",
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":` + jsonQuote(reply) + `}}]}`))
		}))
		cfg := AdvisorConfig{Enabled: true, Endpoint: server.URL, Model: "local"}
		_, ok := proposeWithModel(cfg, ModelAnalysis{Category: "Oggetto grande"}, "balanced", presets["balanced"])
		server.Close()
		if ok {
			t.Fatalf("risposta inutilizzabile accettata: %q", reply)
		}
	}
}

// The model names the part; the numbers are derived here. So a reply that tries
// to dictate settings must have no effect on them: only the class it reports is
// allowed to move anything.
func TestModelCannotDictateSettings(t *testing.T) {
	server := httptest.NewServer(func() http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			// Absurd settings smuggled alongside a legitimate identification.
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"object\":\"vaso\",\"class\":\"hollow\",\"walls_delta\":9,\"infill_delta\":60,\"speed_scale\":3.0,\"reason\":\"x\"}"}}]}`))
		}
	}())
	defer server.Close()

	cfg := AdvisorConfig{Enabled: true, Endpoint: server.URL, Model: "local"}
	base := presets["balanced"]
	a := ModelAnalysis{Category: "Oggetto grande", Extents: [3]float64{80, 80, 200}}

	got, deltas, ok := proposeWithModelDetailed(cfg, a, "balanced", base)
	if !ok {
		t.Fatal("un'identificazione valida deve essere accettata")
	}
	if deltas.Object != "vaso" || deltas.Class != "hollow" {
		t.Fatalf("identificazione persa: %+v", deltas)
	}
	// A hollow part gets less infill, not the +60 the reply asked for.
	if got.InfillPct >= base.InfillPct {
		t.Fatalf("la classe hollow deve ridurre l'infill: %d -> %d", base.InfillPct, got.InfillPct)
	}
	if got.Walls > base.Walls+advisorMaxDelta {
		t.Fatalf("pareti fuori limite: %d", got.Walls)
	}
	if got.Outer > base.Outer || got.OuterAccel > base.OuterAccel {
		t.Fatal("nessuna risposta deve poter accelerare la macchina")
	}
}

// The classes have to actually differ, or splitting the work bought nothing.
func TestClassesProduceDifferentSettings(t *testing.T) {
	a := ModelAnalysis{Extents: [3]float64{80, 60, 40}}
	hollow := deltasForClass("hollow", a, "balanced")
	mechanical := deltasForClass("mechanical", a, "balanced")
	unknown := deltasForClass("unknown", a, "balanced")

	if hollow.Infill >= 0 {
		t.Fatalf("un guscio cavo non deve guadagnare infill: %d", hollow.Infill)
	}
	if mechanical.Infill <= 0 {
		t.Fatalf("un pezzo meccanico deve guadagnare infill: %d", mechanical.Infill)
	}
	if unknown.hasEffect() {
		t.Fatalf("una classe sconosciuta non deve cambiare nulla: %+v", unknown)
	}
}

func jsonQuote(s string) string {
	out := []byte{'"'}
	for _, r := range []byte(s) {
		switch r {
		case '"':
			out = append(out, '\\', '"')
		case '\\':
			out = append(out, '\\', '\\')
		case '\n':
			out = append(out, '\\', 'n')
		default:
			out = append(out, r)
		}
	}
	return string(append(out, '"'))
}
