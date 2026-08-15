package shared

import (
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Regression: S.O.G lowering the nozzle temperature must leave the project a
// value the slicer can read, and keep the self-test's copy in step.
//
// checkTemp briefly stored the temperature as a []string, while every other
// part of the code stores it as a plain string. The project writer wraps a
// filament value in a []string itself, so a value already wrapped came out
// "[200]" — a temperature no slicer can parse — and the loop's own re-check
// read it as 0 and believed the profile fixed.
func TestSOGNozzleTempStaysReadable(t *testing.T) {
	rec, a, f, printer := sogFixture(t)
	// A filament whose maximum is below its working temperature makes checkTemp
	// fire, which is the only path that touches the nozzle temperature.
	f.NozzleMax = 200
	rec.Filament["nozzle_temperature"] = "250"
	rec.Filament["nozzle_temperature_initial_layer"] = "255"
	rec.CriticalValues["nozzle_temperature"] = 250

	SecureProfile(&rec, a, f, printer)

	temp := firstFloatFromAny(rec.Filament["nozzle_temperature"])
	if temp <= 0 || temp > 200 {
		t.Fatalf("temperatura ugello dopo la riparazione: %.0f (attesa ≤200 e leggibile)", temp)
	}
	// The value the project will carry must be a bare number, not a wrapped one.
	if s, ok := rec.Filament["nozzle_temperature"].(string); !ok {
		t.Fatalf("la temperatura non è una stringa semplice: %#v", rec.Filament["nozzle_temperature"])
	} else if strings.ContainsAny(s, "[]") {
		t.Fatalf("temperatura ugello corrotta nel profilo: %q", s)
	}
	// The initial-layer temperature must not end up hotter than the layers above.
	if initial := firstFloatFromAny(rec.Filament["nozzle_temperature_initial_layer"]); initial > temp {
		t.Fatalf("primo layer a %.0f °C, più caldo del resto a %.0f °C", initial, temp)
	}
	// The self-test verifies the project against CriticalValues; a stale copy
	// makes it check the wrong number.
	if tracked := rec.CriticalValues["nozzle_temperature"]; tracked != temp {
		t.Fatalf("desync: profilo %.0f °C, self-test controlla %.0f °C", temp, tracked)
	}
}

// Regression: a filament setting that arrives already wrapped in a []string
// must still read as the number it is, not as absent.
func TestFirstFloatReadsWrappedValues(t *testing.T) {
	if got := firstFloatFromAny([]string{"215"}); got != 215 {
		t.Fatalf("[]string{\"215\"} letto come %.0f invece di 215", got)
	}
	if got := firstFloatFromAny([]any{"60"}); got != 60 {
		t.Fatalf("[]any{\"60\"} letto come %.0f invece di 60", got)
	}
}

// Regression: a non-finite coordinate must never reach the 3MF. Writing "NaN"
// or "+Inf" makes the whole model unreadable — a corrupt file is worse than a
// refusal. Analysis rejects such meshes, so this is the last gate for anything
// a downstream transform might produce.
func TestGeometryWriterRejectsNonFinite(t *testing.T) {
	for name, bad := range map[string]float64{"NaN": math.NaN(), "+Inf": math.Inf(1), "-Inf": math.Inf(-1)} {
		tris := []triangle{
			{A: vec3{0, 0, 0}, B: vec3{10, 0, 0}, C: vec3{bad, 5, 0}},
			{A: vec3{0, 0, 0}, B: vec3{0, 10, 0}, C: vec3{5, 5, 5}},
		}
		if err := writeGeometryOnly3MF(t.TempDir()+"/bad.3mf", tris); err == nil {
			t.Fatalf("%s: un 3MF con una coordinata non finita è stato scritto invece di essere rifiutato", name)
		}
	}
	// A clean mesh still writes.
	good := box(0, 0, 0, 10, 10, 10)
	if err := writeGeometryOnly3MF(t.TempDir()+"/good.3mf", good); err != nil {
		t.Fatalf("una mesh valida è stata rifiutata: %v", err)
	}
}

// Regression: the model's finish and risks must reach S.O.G through the real
// path, not only when a test injects them directly.
//
// proposeWithModelDetailed copied the object, class and reason across from the
// vetted reply but dropped the finish and the risks, so deltasForClass's empty
// defaults reached the interface instead. S.O.G then always used its baseline
// margin and never applied a named-risk correction, however the model answered.
// Every test of those features injected LastAdvisorOutcome directly and so never
// exercised this gap.
func TestModelFinishAndRisksReachTheOutcome(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":` +
			jsonQuote(`{"object":"vaso","class":"decorative","finish":"showpiece","risks":["warping"],"reason":"x"}`) +
			`}}]}`))
	}))
	defer server.Close()

	cfg := AdvisorConfig{Enabled: true, Endpoint: server.URL, Model: "local"}
	// A shape the guard will not contradict for "decorative" (it has no
	// geometric refutation), so the recognition survives intact.
	a := ModelAnalysis{
		Filename: "vaso.stl", Category: "Oggetto tecnico/decorativo",
		Extents: [3]float64{80, 80, 120}, Volume: 80 * 80 * 120 * 0.3,
		SurfaceArea: 40000, Watertight: true, TriangleCount: 20000,
	}
	_, deltas, ok := proposeWithModelDetailed(cfg, a, "balanced", presets["balanced"])
	if !ok {
		t.Fatal("una risposta valida del modello è stata rifiutata")
	}
	if deltas.Finish != "showpiece" {
		t.Fatalf("finish perso lungo il percorso: %q invece di showpiece", deltas.Finish)
	}
	if len(deltas.Risks) != 1 || deltas.Risks[0] != "warping" {
		t.Fatalf("risks persi lungo il percorso: %v", deltas.Risks)
	}
}
