package shared

import "testing"

// Advice that is merely too expensive should be toned down until it fits, not
// thrown away. Discarding it meant the user saw "advice rejected" and got
// nothing, when a gentler version of the very same advice was acceptable.
func TestAdviceTooExpensiveIsScaledNotDiscarded(t *testing.T) {
	base := presets["balanced"]
	bold := advisorDeltas{Walls: 2, TopLayers: 2, BotLayers: 2, Infill: 8, SpeedScale: 1}

	full := applyAdvisorDeltas(base, bold)
	if advisorWithinBudget(base, full, "balanced") {
		t.Fatal("presupposto del test non valido: la proposta piena deve sforare il budget")
	}
	if !advisorProposalSafe(base, full) {
		t.Fatal("presupposto del test non valido: la proposta deve essere sicura")
	}

	got, scaled, accepted := admitAdvice(base, bold, "balanced")
	if !accepted {
		t.Fatal("una proposta sicura ma costosa va ridotta, non scartata")
	}
	if !scaled {
		t.Fatal("la proposta doveva risultare ridotta")
	}
	if !advisorWithinBudget(base, got, "balanced") {
		t.Fatal("la versione ridotta deve rientrare nel budget")
	}
	// It must still point the same way, just less far.
	if got.Walls < base.Walls || got.Walls > full.Walls {
		t.Fatalf("la direzione del consiglio è stata alterata: base %d, ridotto %d, pieno %d", base.Walls, got.Walls, full.Walls)
	}
}

// Safety failures are a different matter: those are discarded whole, at any
// strength, because a half-applied unsafe suggestion is its own kind of wrong.
func TestUnsafeAdviceIsNeverScaledIn(t *testing.T) {
	base := presets["balanced"]
	for name, bad := range map[string]advisorDeltas{
		"accelera": {SpeedScale: 1.4},
		"pareti":   {Walls: 20},
		"infill":   {Infill: 60},
	} {
		got, _, accepted := admitAdvice(base, bad, "balanced")
		if accepted && !advisorProposalSafe(base, got) {
			t.Fatalf("%s: è stata ammessa una proposta non sicura", name)
		}
		if name == "accelera" && got.Outer > base.Outer {
			t.Fatal("il modello non deve poter accelerare la macchina nemmeno ridotto")
		}
	}
}

// Whatever survives, the guarantee holds: never faster, never a different tier,
// never over budget.
func TestAdmittedAdviceAlwaysRespectsGuarantees(t *testing.T) {
	for _, quality := range []string{"low", "balanced", "perfect"} {
		base := presets[quality]
		for _, d := range []advisorDeltas{
			{Walls: 2, TopLayers: 2, BotLayers: 2, Infill: 8, SpeedScale: 1},
			{Walls: -2, Infill: -8, SpeedScale: 0.8},
			{Walls: 1, Infill: 4, SpeedScale: 0.9},
		} {
			got, _, accepted := admitAdvice(base, d, quality)
			if !accepted {
				continue
			}
			if got.Layer != base.Layer {
				t.Fatalf("%s: altezza layer alterata", quality)
			}
			if got.Outer > base.Outer || got.OuterAccel > base.OuterAccel {
				t.Fatalf("%s: velocità o accelerazione aumentate", quality)
			}
			if !advisorWithinBudget(base, got, quality) {
				t.Fatalf("%s: oltre il budget tempo", quality)
			}
		}
	}
}
