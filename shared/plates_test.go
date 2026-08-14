package shared

import (
	"os"
	"testing"
)

// The real file that prompted this: a Porsche bundled with a garage, 455 x 437
// mm as one lump and refused by a 220 mm machine. Split into its actual pieces,
// the parts should be printable across several plates at full size.
func TestSplitRealPorscheAcrossPlates(t *testing.T) {
	const path = `C:\Users\alexm\Downloads\Porsche+911+GT3+RS+with+Garage.3mf`
	if _, err := os.Stat(path); err != nil {
		t.Skip("modello di riferimento non presente")
	}
	tris, _, _, err := read3MFGeometry(path, newAnalysisDeadline())
	if err != nil {
		t.Fatalf("lettura 3MF fallita: %v", err)
	}
	t.Logf("triangoli totali: %d", len(tris))

	pieces := SplitIntoPieces(tris)
	if len(pieces) < 2 {
		t.Fatalf("atteso più di un pezzo, trovati %d", len(pieces))
	}
	for i, p := range pieces {
		if i < 8 {
			t.Logf("pezzo %d: %.0f x %.0f x %.0f mm, %d triangoli",
				i+1, p.Extents[0], p.Extents[1], p.Extents[2], len(p.Triangles))
		}
	}

	volume := [3]float64{220, 220, 220}
	plates, oversized := PackIntoPlates(pieces, volume)
	t.Logf("risultato: %s", DescribePlates(plates, oversized))
	for i, plate := range plates {
		t.Logf("piatto %d: %d pezzi, ingombro max %.0f x %.0f x %.0f mm",
			i+1, len(plate.Pieces), plate.Extents[0], plate.Extents[1], plate.Extents[2])
	}
	for _, p := range oversized {
		t.Logf("troppo grande: %.0f x %.0f x %.0f mm", p.Extents[0], p.Extents[1], p.Extents[2])
	}

	if len(plates) == 0 {
		t.Fatal("nessun piatto prodotto: il modello sarebbe ancora inutilizzabile")
	}
	// Every piece placed on a plate must genuinely fit the machine.
	for i, plate := range plates {
		for _, piece := range plate.Pieces {
			if !PieceFits(piece, volume) {
				t.Fatalf("piatto %d contiene un pezzo fuori volume: %.0f x %.0f x %.0f",
					i+1, piece.Extents[0], piece.Extents[1], piece.Extents[2])
			}
		}
	}
}

// A single solid part must stay one piece: splitting must not invent seams.
func TestSplitKeepsConnectedMeshWhole(t *testing.T) {
	pieces := SplitIntoPieces(selfTestCubeTriangles())
	if len(pieces) != 1 {
		t.Fatalf("un cubo connesso è stato diviso in %d pezzi", len(pieces))
	}
}
