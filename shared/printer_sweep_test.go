package shared

import "testing"

// Every supported printer must survive a recommendation. The user reported the
// app closing when they picked any machine other than their own Adventurer 5M,
// which is the shape of a panic in the engine rather than a refusal from it.
func TestEveryPrinterProducesARecommendation(t *testing.T) {
	a := ModelAnalysis{
		Filename: "pezzo.stl", Category: "Oggetto tecnico/decorativo",
		Extents: [3]float64{80, 60, 40}, Volume: 42000, SurfaceArea: 18000,
		TriangleCount: 12000, Watertight: true, BedContactRatio: 0.2,
	}
	for _, printer := range SupportedPrinters() {
		for _, nozzle := range []float64{0.2, 0.25, 0.4, 0.6, 0.8, 1.0} {
			printer := printer
			printer.NozzleDiameter = nozzle
			for _, quality := range []string{"low", "balanced", "perfect"} {
				for _, f := range builtinFilamentsForTest(t) {
					func() {
						defer func() {
							if r := recover(); r != nil {
								t.Fatalf("panic con %s %s / %s / %s: %v", printer.Brand, printer.Model, quality, f.Product, r)
							}
						}()
						// A refusal is a legitimate answer — an A1 mini really cannot
						// run ABS. Only a panic is a defect.
						_, _ = RecommendForPrinter(a, f, printer, quality)
					}()
				}
			}
		}
	}
}

func builtinFilamentsForTest(t *testing.T) []Filament {
	t.Helper()
	all, err := LoadBuiltinFilaments()
	if err != nil {
		t.Fatalf("catalogo filamenti non caricabile: %v", err)
	}
	return all
}
