package shared

import "testing"

// The machine's build volume is what decides whether a part fits. The usable
// plate — a few millimetres off each axis so a part can be centred clear of the
// clips and the prime line — is the right basis for arranging something and the
// wrong basis for refusing it.
//
// Confusing the two rejected a 214 mm part on a 220 mm bed: a part the slicer
// would have taken without comment, refused with no way for the user to
// overrule it.
func TestFitIsJudgedAgainstTheBuildVolume(t *testing.T) {
	for _, printer := range SupportedPrinters() {
		usable := ManualFor(printer).UsablePlate
		// Between the usable plate and the advertised bed: tight, but the
		// machine's own specification says it fits.
		size := [3]float64{
			(usable[0] + printer.BuildVolume[0]) / 2,
			(usable[1] + printer.BuildVolume[1]) / 2,
			(usable[2] + printer.BuildVolume[2]) / 2,
		}
		a := ModelAnalysis{
			Filename: "grande.stl", Category: "Oggetto grande", Extents: size,
			Volume: size[0] * size[1] * size[2] * 0.3, SurfaceArea: 50000,
			Watertight: true, TriangleCount: 1000,
		}
		if err := ValidateModelForPrinter(a, printer); err != nil {
			t.Fatalf("%s: rifiutato un pezzo che il piano dichiarato accetta (%.1f su %.1f): %v",
				printer.Model, size[0], printer.BuildVolume[0], err)
		}
	}
}

// The limit still exists: past the advertised bed, the answer is no.
func TestPartsBeyondTheBuildVolumeAreStillRefused(t *testing.T) {
	printer := DefaultPrinterProfile()
	size := [3]float64{printer.BuildVolume[0] + 5, 50, 50}
	a := ModelAnalysis{
		Filename: "troppo.stl", Category: "Oggetto grande", Extents: size,
		Volume: 100000, SurfaceArea: 30000, Watertight: true, TriangleCount: 1000,
	}
	if err := ValidateModelForPrinter(a, printer); err == nil {
		t.Fatal("un pezzo oltre il volume di stampa è stato accettato")
	}
}
