package shared

import "testing"

// The advisor's guarantee is structural, so it is tested structurally: whatever
// the proposer suggests, for every category and every tier, what survives the
// veto must stay inside the envelope and inside the tier's time budget.
func TestAdvisorNeverMakesThingsWorse(t *testing.T) {
	categories := []string{
		"Miniatura dettagliata",
		"Forma alta o sottile",
		"Geometria con molti sbalzi",
		"Oggetto grande",
		"Superficie complessa",
		"Oggetto tecnico/decorativo",
	}
	printer := DefaultPrinterProfile()
	for _, quality := range []string{"low", "balanced", "perfect"} {
		base := presets[quality]
		for _, category := range categories {
			a := ModelAnalysis{Category: category}
			got := applyAdvisor(base, a, quality, printer)

			if got.Layer != base.Layer {
				t.Fatalf("%s/%s: l'advisor ha cambiato l'altezza layer %.3f -> %.3f", quality, category, base.Layer, got.Layer)
			}
			if got.Outer > base.Outer || got.Inner > base.Inner || got.Bridge > base.Bridge {
				t.Fatalf("%s/%s: l'advisor ha aumentato una velocità", quality, category)
			}
			if got.OuterAccel > base.OuterAccel || got.InnerAccel > base.InnerAccel {
				t.Fatalf("%s/%s: l'advisor ha aumentato un'accelerazione", quality, category)
			}
			if got.Walls < 2 || got.Walls > 6 || got.InfillPct < 8 || got.InfillPct > 40 {
				t.Fatalf("%s/%s: fuori envelope: walls=%d infill=%d", quality, category, got.Walls, got.InfillPct)
			}
			ratio := presetWorkIndex(got) / presetWorkIndex(base)
			if budget := advisorTimeBudget(quality); ratio > budget+0.0001 {
				t.Fatalf("%s/%s: costo %.3f oltre il budget %.3f", quality, category, ratio, budget)
			}
		}
	}
}

// A proposal that breaches the envelope must be discarded whole, not clamped:
// half-applied advice is its own kind of wrong.
func TestAdvisorRejectsOutOfEnvelopeProposal(t *testing.T) {
	base := presets["balanced"]
	for _, bad := range []qualityPreset{
		func() qualityPreset { p := base; p.Layer += 0.06; return p }(),
		func() qualityPreset { p := base; p.Walls = 9; return p }(),
		func() qualityPreset { p := base; p.InfillPct = 75; return p }(),
		func() qualityPreset { p := base; p.Outer = base.Outer + 30; return p }(),
		func() qualityPreset { p := base; p.OuterAccel = base.OuterAccel + 900; return p }(),
	} {
		if advisorProposalAllowed(base, bad, "balanced") {
			t.Fatalf("proposta fuori envelope accettata: %+v", bad)
		}
	}
}

// The tiers order by how much extra work they may buy: fast the least.
func TestAdvisorBudgetOrdering(t *testing.T) {
	if !(advisorTimeBudget("low") < advisorTimeBudget("balanced") && advisorTimeBudget("balanced") < advisorTimeBudget("perfect")) {
		t.Fatalf("budget non ordinati: %.2f %.2f %.2f", advisorTimeBudget("low"), advisorTimeBudget("balanced"), advisorTimeBudget("perfect"))
	}
}
