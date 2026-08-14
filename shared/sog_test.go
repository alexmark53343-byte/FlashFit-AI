package shared

import (
	"fmt"
	"strconv"
	"testing"
)

func sogFixture(t *testing.T) (Recommendation, ModelAnalysis, Filament, PrinterProfile) {
	t.Helper()
	printer := DefaultPrinterProfile()
	filaments, err := LoadBuiltinFilaments()
	if err != nil || len(filaments) == 0 {
		t.Fatalf("catalogo filamenti non caricabile: %v", err)
	}
	a := ModelAnalysis{
		Filename: "pezzo.stl", Category: "Oggetto tecnico/decorativo",
		Extents: [3]float64{120, 90, 180}, Volume: 400000, SurfaceArea: 90000,
		TriangleCount: 40000, Watertight: true, OverhangRatio: 0.2, ThinOrTall: true,
	}
	rec, err := RecommendForPrinter(a, filaments[0], printer, "balanced")
	if err != nil {
		t.Fatalf("raccomandazione non producibile: %v", err)
	}
	return rec, a, filaments[0], printer
}

// The property the whole layer rests on: S.O.G may only ever slow the print
// down. If a repair could raise a speed, an acceleration or a temperature, then
// letting it run on every import — and letting a model influence it — would be
// handing away the guarantee the quality tier already made.
func TestSOGOnlyEverSlowsDown(t *testing.T) {
	watched := []string{
		"outer_wall_speed", "inner_wall_speed", "bridge_speed", "top_surface_speed",
		"outer_wall_acceleration", "inner_wall_acceleration", "bridge_acceleration",
		"overhang_3_4_speed", "overhang_4_4_speed",
	}
	for _, finish := range []string{"showpiece", "visible", "functional", "hidden", "unknown", "sciocchezza"} {
		rec, a, f, printer := sogFixture(t)
		before := map[string]float64{}
		for _, key := range watched {
			before[key] = processFloat(&rec, key)
		}
		nozzleBefore := firstFloatFromAny(rec.Filament["nozzle_temperature"])

		LastAdvisorOutcome = AdvisorOutcome{Used: true, Finish: finish}
		SecureProfile(&rec, a, f, printer)

		for _, key := range watched {
			if after := processFloat(&rec, key); after > before[key] {
				t.Fatalf("finitura %q: %s è salito da %.0f a %.0f", finish, key, before[key], after)
			}
		}
		if after := firstFloatFromAny(rec.Filament["nozzle_temperature"]); after > nozzleBefore {
			t.Fatalf("finitura %q: temperatura ugello salita da %.0f a %.0f", finish, nozzleBefore, after)
		}
	}
	LastAdvisorOutcome = AdvisorOutcome{}
}

// The model's influence is bounded in one direction. Whatever it answers, the
// clearance below a limit can only widen — so a wrong answer costs time and
// never safety.
func TestModelCanOnlyMakeSOGMoreCareful(t *testing.T) {
	baseline := sogMargin("unknown")
	for _, finish := range []string{"showpiece", "visible", "functional", "hidden", "unknown", "", "qualunque"} {
		if got := sogMargin(finish); got > baseline {
			t.Fatalf("la finitura %q allarga il limite invece di stringerlo: %.2f > %.2f", finish, got, baseline)
		}
	}
	if sogMargin("showpiece") >= sogMargin("functional") {
		t.Fatal("un pezzo da esposizione deve stare più lontano dal limite di uno funzionale")
	}
}

// A defect that was corrected must not still be predicted afterwards. This is
// the go-ahead: the profile is cleared on the strength of a check that ran
// after its last change, not before it.
func TestSOGClearsOnlyAfterTheFinalCheck(t *testing.T) {
	rec, a, f, printer := sogFixture(t)
	// Force a defect the checker will certainly report: an acceleration far
	// above what the frame can absorb on a tall part.
	rec.Process["outer_wall_acceleration"] = "99000"
	before := CheckPrintReadiness(rec, a, f, printer)
	if len(before.Issues) == 0 {
		t.Fatal("presupposto del test non valido: il profilo doveva risultare difettoso")
	}

	verdict := SecureProfile(&rec, a, f, printer)
	after := CheckPrintReadiness(rec, a, f, printer)
	if after.Blocked {
		t.Fatalf("autorizzato con un difetto bloccante ancora presente: %+v", after.Issues)
	}
	if verdict.Cleared != !after.Blocked {
		t.Fatal("il via libera non corrisponde al controllo finale")
	}
	if len(verdict.Repairs) == 0 {
		t.Fatal("un difetto reale doveva produrre almeno una correzione")
	}
	if got := processFloat(&rec, "outer_wall_acceleration"); got >= 99000 {
		t.Fatalf("l'accelerazione non è stata corretta: %.0f", got)
	}
}

// Slowing the print down and leaving the old estimate on screen is the kind of
// quiet inaccuracy that made the time reading untrustworthy to begin with.
func TestSOGKeepsTheTimeEstimateHonest(t *testing.T) {
	rec, a, f, printer := sogFixture(t)
	rec.Process["outer_wall_speed"] = "400" // far beyond any melt rate
	beforeMinutes := rec.EstimatedModeMinutes
	beforeSpeed := processFloat(&rec, "outer_wall_speed")

	verdict := SecureProfile(&rec, a, f, printer)
	afterSpeed := processFloat(&rec, "outer_wall_speed")
	if afterSpeed >= beforeSpeed {
		t.Fatal("presupposto del test non valido: la velocità doveva essere ridotta")
	}
	if rec.EstimatedModeMinutes <= beforeMinutes {
		t.Fatal("la stampa è stata rallentata ma il tempo stimato non è cresciuto")
	}
	// The estimate is built as inversely proportional to the outer wall speed,
	// so the scaling has to follow that same relationship.
	wantFactor := beforeSpeed / afterSpeed
	if got := verdict.SpeedFactor; got < wantFactor*0.999 || got > wantFactor*1.001 {
		t.Fatalf("fattore tempo %.4f invece di %.4f", got, wantFactor)
	}
}

// The repair loop has to finish. Each correction lands at or below the limit
// that flagged it, so no defect can flag twice — that is why it terminates
// rather than merely usually terminating.
func TestSOGAlwaysTerminates(t *testing.T) {
	rec, a, f, printer := sogFixture(t)
	for _, key := range []string{"outer_wall_speed", "bridge_speed", "outer_wall_acceleration"} {
		rec.Process[key] = "99000"
	}
	rec.Filament["nozzle_temperature"] = []string{"400"}

	verdict := SecureProfile(&rec, a, f, printer)
	if verdict.Passes >= 8 {
		t.Fatalf("il ciclo ha esaurito i passaggi invece di convergere: %d", verdict.Passes)
	}
	if !verdict.Cleared {
		t.Fatalf("difetti correggibili non risolti: %+v", verdict.Remaining)
	}
}

// A repair changes what the project must carry, and CriticalValues is what the
// self-test compares the written project against. Leaving it on the pre-repair
// number would make the self-test check the wrong thing and report red on a
// perfectly correct project.
func TestSOGKeepsTheSelfTestInStep(t *testing.T) {
	rec, a, f, printer := sogFixture(t)
	rec.Process["outer_wall_acceleration"] = "99000"
	rec.CriticalValues["outer_acceleration"] = 99000

	SecureProfile(&rec, a, f, printer)

	written, err := strconv.ParseFloat(fmt.Sprint(rec.Process["outer_wall_acceleration"]), 64)
	if err != nil {
		t.Fatalf("accelerazione non leggibile: %v", err)
	}
	if got := rec.CriticalValues["outer_acceleration"]; got != written {
		t.Fatalf("il self-test controllerebbe %.0f mentre il profilo porta %.0f", got, written)
	}
}

// The model names risks the arithmetic cannot see, because they follow from
// what the part is for rather than from any number in the profile. S.O.G still
// owns every number: a named risk buys a bounded, one-way correction and
// nothing else.
func TestNamedRisksOnlyEverTightenTheProfile(t *testing.T) {
	watched := []string{
		"outer_wall_acceleration", "inner_wall_acceleration", "sparse_infill_acceleration",
		"overhang_2_4_speed", "overhang_3_4_speed", "overhang_4_4_speed",
		"travel_speed", "initial_layer_speed",
	}
	for _, risk := range sogRiskOrder {
		rec, a, f, printer := sogFixture(t)
		before := map[string]float64{}
		for _, key := range watched {
			before[key] = processFloat(&rec, key)
		}

		LastAdvisorOutcome = AdvisorOutcome{Used: true, Risks: []string{risk}}
		verdict := SecureProfile(&rec, a, f, printer)

		for _, key := range watched {
			if after := processFloat(&rec, key); after > before[key] {
				t.Fatalf("rischio %q: %s è salito da %.0f a %.0f", risk, key, before[key], after)
			}
		}
		if len(verdict.Risks) != 1 || verdict.Risks[0] != risk {
			t.Fatalf("rischio %q non riportato nel verdetto: %v", risk, verdict.Risks)
		}
	}
	LastAdvisorOutcome = AdvisorOutcome{}
}

// Each named risk has to actually do something, or naming it is theatre.
func TestEachNamedRiskChangesTheProfile(t *testing.T) {
	for _, risk := range sogRiskOrder {
		rec, _, _, _ := sogFixture(t)
		// A brim already present means the warping fix has only its second half
		// to apply, which is the case worth checking.
		rec.Process["brim_type"] = "no_brim"
		repairs := applyNamedRisks(&rec, []string{risk}, 1.0)
		if len(repairs) == 0 {
			t.Fatalf("il rischio %q non produce alcuna correzione", risk)
		}
	}
}

// A reply listing every risk in the vocabulary is not a diagnosis of this part,
// and words S.O.G has no correction for buy nothing by being kept.
func TestRiskListIsFilteredByTheGuard(t *testing.T) {
	if got := keepKnownRisks([]string{"warping", "spaghetti", "WARPING", " layer_shift "}); len(got) != 2 {
		t.Fatalf("filtro rischi errato: %v", got)
	}
	if got := keepKnownRisks(sogRiskOrder); got != nil {
		t.Fatalf("l'elenco completo del vocabolario non è una diagnosi: %v", got)
	}
	if got := keepKnownRisks([]string{"qualcosa", "altro"}); got != nil {
		t.Fatalf("parole sconosciute accettate: %v", got)
	}
	// Order is fixed, so the same reply always produces the same repairs.
	first := keepKnownRisks([]string{"layer_shift", "warping"})
	second := keepKnownRisks([]string{"warping", "layer_shift"})
	if len(first) != 2 || first[0] != second[0] || first[1] != second[1] {
		t.Fatalf("l'ordine dipende da come ha risposto il modello: %v contro %v", first, second)
	}
}
