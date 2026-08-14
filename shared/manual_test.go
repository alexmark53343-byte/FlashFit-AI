package shared

import (
	"strings"
	"testing"
)

// A bed loses a strip to clips, the prime line and the edge the nozzle cannot
// reach squarely. Checking a part against the advertised volume passes a 220 mm
// part on a 220 mm bed, which does not print.
func TestUsablePlateIsSmallerThanTheAdvertisedVolume(t *testing.T) {
	for _, printer := range SupportedPrinters() {
		m := ManualFor(printer)
		for axis := 0; axis < 3; axis++ {
			if m.UsablePlate[axis] <= 0 {
				t.Fatalf("%s: piatto utile nullo sull'asse %d", printer.Model, axis)
			}
			if m.UsablePlate[axis] >= printer.BuildVolume[axis] {
				t.Fatalf("%s: l'asse %d non lascia alcun margine (%.1f di %.1f)",
					printer.Model, axis, m.UsablePlate[axis], printer.BuildVolume[axis])
			}
		}
	}
}

// The fitted nozzle decides which layer heights exist. A 0.8 mm nozzle cannot
// lay a 0.12 mm layer: the melt has nowhere to go.
func TestLayerRangeFollowsTheNozzle(t *testing.T) {
	cases := []struct {
		nozzle   float64
		min, max float64
	}{
		{0.4, 0.10, 0.30},
		{0.6, 0.15, 0.45},
		{0.8, 0.20, 0.60},
	}
	for _, c := range cases {
		printer := DefaultPrinterProfile()
		printer.NozzleDiameter = c.nozzle
		m := ManualFor(printer)
		if m.MinLayer != c.min || m.MaxLayer != c.max {
			t.Fatalf("ugello %.1f: intervallo %.2f–%.2f invece di %.2f–%.2f",
				c.nozzle, m.MinLayer, m.MaxLayer, c.min, c.max)
		}
	}
}

// The finest tier on a wide nozzle has to mean something the machine can do —
// and the change has to be said out loud. Quietly serving a different layer
// height than the tier implies is what made the quality setting untrustworthy
// once already.
func TestFineTierOnAWideNozzleIsClampedAndDeclared(t *testing.T) {
	printer := DefaultPrinterProfile()
	printer.NozzleDiameter = 0.8
	filaments, err := LoadBuiltinFilaments()
	if err != nil || len(filaments) == 0 {
		t.Fatalf("catalogo filamenti non caricabile: %v", err)
	}
	a := ModelAnalysis{
		Filename: "pezzo.stl", Category: "Oggetto tecnico/decorativo",
		Extents: [3]float64{80, 60, 40}, Volume: 42000, SurfaceArea: 18000,
		TriangleCount: 12000, Watertight: true,
	}
	rec, err := RecommendForPrinter(a, filaments[0], printer, "perfect")
	if err != nil {
		t.Fatalf("raccomandazione non producibile: %v", err)
	}

	manual := ManualFor(printer)
	layer := rec.CriticalValues["layer_height"]
	if layer < manual.MinLayer-0.001 {
		t.Fatalf("layer %.2f sotto il minimo fisico %.2f dell'ugello", layer, manual.MinLayer)
	}
	declared := false
	for _, reason := range rec.Reasons {
		if strings.Contains(reason, "ugello") && strings.Contains(strings.ToLower(reason), "layer") {
			declared = true
			break
		}
	}
	if !declared {
		t.Fatal("l'altezza layer è stata cambiata per l'ugello senza dirlo nelle motivazioni")
	}
}

// The readiness check reports against the ceiling and S.O.G repairs down to it.
// When each carried its own arithmetic the two could disagree about where the
// line was, and a repair could land on the wrong side of the check that asked
// for it.
func TestGhostingCeilingHasOneDefinition(t *testing.T) {
	rec, a, f, printer := sogFixture(t)
	rec.Process["outer_wall_acceleration"] = "99000"

	readiness := CheckPrintReadiness(rec, a, f, printer)
	var reported float64
	for _, issue := range readiness.Issues {
		if issue.Key == "checkGhosting" {
			reported = issue.Limit
		}
	}
	if reported <= 0 {
		t.Fatal("il ghosting doveva essere segnalato")
	}
	if want := ManualFor(printer).AccelerationCeiling(a.Extents[2], a.ThinOrTall); reported != want {
		t.Fatalf("il controllo usa %.0f mentre il manuale dice %.0f", reported, want)
	}

	SecureProfile(&rec, a, f, printer)
	if got := processFloat(&rec, "outer_wall_acceleration"); got > reported {
		t.Fatalf("la riparazione si è fermata a %.0f, sopra il limite %.0f che l'aveva richiesta", got, reported)
	}
}
