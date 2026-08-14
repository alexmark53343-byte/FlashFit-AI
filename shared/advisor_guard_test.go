package shared

import (
	"strings"
	"testing"
)

// The failure this guard exists for: the veto only ever refused advice for
// asking too much, so a misclassification that made the print *worse* went
// through untouched. "hollow" on a solid part takes infill out and slows it
// down — cheaper than the base profile on every axis the veto measures, and
// wrong.
func TestClassContradictedByTheMeshIsWithdrawn(t *testing.T) {
	solid := ModelAnalysis{
		Filename: "porsche911.stl", Watertight: true,
		Extents: [3]float64{200, 90, 60}, Volume: 200 * 90 * 60 * 0.62,
		SurfaceArea: 40000,
	}
	got, mismatch, ok := vetReply(advisorDeltas{Object: "porsche 911", Class: "hollow"}, solid)
	if !ok {
		t.Fatal("una risposta nel vocabolario deve restare utilizzabile")
	}
	if mismatch == "" {
		t.Fatal("solidità 0.62 dichiarata guscio: la mesh smentisce la classe e va segnalato")
	}
	if got.Class != "unknown" {
		t.Fatalf("la classe smentita doveva essere ritirata, invece è %q", got.Class)
	}
	// The name is the half the model is good at, and it survives.
	if got.Object != "porsche 911" {
		t.Fatalf("il riconoscimento andava conservato, invece è %q", got.Object)
	}
	if deltasForClass(got.Class, solid, "balanced").hasEffect() {
		t.Fatal("una classe ritirata non deve muovere nessuna impostazione")
	}
}

// The mirror case: when the mesh agrees, the guard stays out of the way.
func TestClassSupportedByTheMeshSurvives(t *testing.T) {
	vase := ModelAnalysis{
		Filename: "vase.stl", Watertight: true,
		Extents: [3]float64{80, 80, 200}, Volume: 80 * 80 * 200 * 0.09,
		SurfaceArea: 60000,
	}
	got, mismatch, ok := vetReply(advisorDeltas{Object: "vase", Class: "hollow"}, vase)
	if !ok || mismatch != "" || got.Class != "hollow" {
		t.Fatalf("un vaso a solidità 0.09 è un guscio: %q, smentita %q", got.Class, mismatch)
	}
}

// A mesh with holes has no enclosed volume, so solidity is an artefact. The
// guard must not refute a class using a number that does not mean anything —
// that would turn every broken STL into an infinitely thin shell.
func TestUntrustworthyVolumeNeverRefutesAClass(t *testing.T) {
	broken := ModelAnalysis{
		Filename: "scan.stl", Watertight: false,
		Extents: [3]float64{100, 100, 100}, Volume: 10, SurfaceArea: 5000,
	}
	for _, class := range []string{"hollow", "mechanical"} {
		if _, mismatch, _ := vetReply(advisorDeltas{Object: "x", Class: class}, broken); mismatch != "" {
			t.Fatalf("%s: smentita basata su un volume inaffidabile: %s", class, mismatch)
		}
	}
}

// A word outside the vocabulary is not a quiet "no change". It is an answer
// that ignored the contract, and nothing else in it is evidence of anything.
func TestOffContractClassIsRejectedOutright(t *testing.T) {
	a := ModelAnalysis{Filename: "x.stl", Extents: [3]float64{50, 50, 50}}
	for _, class := range []string{"", "cylindrical", "HOLLOW-ish", "vase"} {
		if _, _, ok := vetReply(advisorDeltas{Object: "qualcosa", Class: class}, a); ok {
			t.Fatalf("classe fuori vocabolario accettata: %q", class)
		}
	}
	// Case and padding are formatting, not a different answer.
	if got, _, ok := vetReply(advisorDeltas{Object: "x", Class: "  Mechanical "}, a); !ok || got.Class != "mechanical" {
		t.Fatal("maiuscole e spazi non sono una violazione del contratto")
	}
}

// Model output is untrusted input, and it ends up in a label.
func TestModelTextIsMadeSafeForTheInterface(t *testing.T) {
	a := ModelAnalysis{Filename: "x.stl", Extents: [3]float64{50, 50, 50}}
	got, _, ok := vetReply(advisorDeltas{
		Object: "staffa\r\n{\"class\":\"x\"}\x07",
		Class:  "mechanical",
		Reason: strings.Repeat("molto lungo ", 60),
	}, a)
	if !ok {
		t.Fatal("il testo sporco non deve invalidare una risposta valida")
	}
	if strings.ContainsAny(got.Object, "\r\n{}\"\x07") {
		t.Fatalf("caratteri di controllo o JSON arrivati alla UI: %q", got.Object)
	}
	if len([]rune(got.Object)) > advisorObjectMaxRunes {
		t.Fatalf("nome oltre il limite: %d caratteri", len([]rune(got.Object)))
	}
	if len([]rune(got.Reason)) > advisorReasonMaxRunes+1 {
		t.Fatalf("motivazione oltre il limite: %d caratteri", len([]rune(got.Reason)))
	}
}

// An honest "unknown" used to be worse than never asking: the deltas came out
// zero, the resulting profile equalled the base, and the veto accepted it —
// which replaced the deterministic category tuning with nothing at all.
func TestAbstentionFallsBackToTheRulesInsteadOfBlankingThem(t *testing.T) {
	a := ModelAnalysis{
		Filename: "ignoto.stl", Category: "Miniatura dettagliata",
		Extents: [3]float64{30, 30, 45}, Volume: 12000, SurfaceArea: 7000, Watertight: true,
	}
	base := presets["balanced"]

	withRules := base
	tuneForCategory(&withRules, a, "balanced")
	if withRules == base {
		t.Skip("questa categoria non muove nulla: il test non distinguerebbe i due percorsi")
	}

	deltas := deltasForClass("unknown", a, "balanced")
	if deltas.hasEffect() {
		t.Fatal("unknown non deve chiedere nulla")
	}
	// This is the property: an abstention must not be admitted as a no-op
	// proposal, because admitting it returns the base preset untuned.
	if _, _, accepted := admitAdvice(base, deltas, "balanced"); !accepted {
		t.Skip("il veto già rifiuta il no-op: la protezione è altrove")
	}
	if got := applyAdvisor(base, a, "balanced", DefaultPrinterProfile()); got == base && withRules != base {
		t.Fatal("l'astensione del modello ha azzerato la taratura per categoria invece di lasciarle il posto")
	}
}

// The finish word is treated more leniently than the class, and deliberately:
// an unknown finish has a safe default, an unknown class does not.
func TestUnknownFinishDoesNotThrowAwayTheIdentification(t *testing.T) {
	a := ModelAnalysis{Filename: "staffa.stl", Extents: [3]float64{50, 50, 50}}
	got, _, ok := vetReply(advisorDeltas{Object: "staffa", Class: "mechanical", Finish: "lucido"}, a)
	if !ok {
		t.Fatal("una parola di finitura sbagliata non deve invalidare la classe")
	}
	if got.Finish != "unknown" {
		t.Fatalf("la finitura fuori vocabolario doveva diventare unknown, invece è %q", got.Finish)
	}
	if sogMargin(got.Finish) != 1.0 {
		t.Fatal("il fallback della finitura deve essere il margine di base")
	}
}
