package shared

import (
	"strings"
	"testing"
)

// A Porsche was once identified as a vase, because the model only ever saw the
// bounding box: at equal size a car and a vase are the same numbers. The prompt
// must carry the two things that actually separate them — the file name and the
// solidity of the shape.
func TestAdvisorPromptCarriesIdentifyingSignals(t *testing.T) {
	car := ModelAnalysis{
		Filename:      "porsche_911.stl",
		Extents:       [3]float64{180, 80, 52},
		Volume:        284_000, // mm3, a fairly solid body
		SurfaceArea:   62_000,
		TriangleCount: 210_000,
		Category:      "Oggetto grande",
	}
	prompt := advisorUserPrompt(car, "balanced", presets["balanced"])

	if !strings.Contains(prompt, "porsche_911.stl") {
		t.Fatal("il nome del file non arriva al modello: è il segnale più forte per riconoscere l'oggetto")
	}
	if !strings.Contains(prompt, "solidity") {
		t.Fatal("la solidità non arriva al modello: senza, un'auto e un vaso sono indistinguibili")
	}
}

// Solidity has to actually separate the two cases, otherwise carrying it is
// pointless: a hollow vase must read far lower than a solid body of equal size.
func TestSolidityDistinguishesHollowFromSolid(t *testing.T) {
	size := [3]float64{180, 80, 52}
	box := size[0] * size[1] * size[2]

	solid := ModelAnalysis{Extents: size, Volume: box * 0.38, SurfaceArea: 62_000}
	hollow := ModelAnalysis{Extents: size, Volume: box * 0.09, SurfaceArea: 96_000}

	solidText := shapeDescriptors(solid)
	hollowText := shapeDescriptors(hollow)

	if !strings.Contains(solidText, "solidity 0.38") {
		t.Fatalf("solidità del pieno non calcolata: %s", solidText)
	}
	if !strings.Contains(hollowText, "solidity 0.09") {
		t.Fatalf("solidità del cavo non calcolata: %s", hollowText)
	}
	if solidText == hollowText {
		t.Fatal("i due descrittori sono identici: non distinguono nulla")
	}
}

// An honest "unknown" is better than a confident wrong label, so the contract
// has to offer that escape.
func TestAdvisorPromptAllowsUnknown(t *testing.T) {
	if !strings.Contains(advisorSystemPrompt, "unknown") {
		t.Fatal("il modello non ha modo di dire che non sa: sarà costretto a inventare")
	}
}
