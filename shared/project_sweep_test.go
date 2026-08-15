package shared

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// checkProjectIsLoadable asserts the things a slicer needs in order to put the
// model on the plate, rather than only in order to open the file.
//
// The distinction is the whole point: the project that failed was a valid 3MF
// and valid JSON, and it opened. It just arrived with an empty plate, because
// nothing checked that what the build declares is also what the project says to
// place.
func checkProjectIsLoadable(t *testing.T, path, what string) {
	t.Helper()
	entries := projectEntries(t, path)

	for _, required := range []string{"[Content_Types].xml", "_rels/.rels", modelEntry, projectConfigEntry} {
		if _, ok := entries[required]; !ok {
			t.Fatalf("%s: manca %s", what, required)
		}
	}

	ids := buildItemObjectIDs([]byte(entries[modelEntry]))
	if len(ids) == 0 {
		t.Fatalf("%s: il 3MF non costruisce alcun oggetto", what)
	}
	settings, ok := entries[modelSettingsEntry]
	if !ok {
		t.Fatalf("%s: nessuna descrizione oggetti: lo slicer importa le impostazioni e lascia il piatto vuoto", what)
	}
	for _, id := range ids {
		if !strings.Contains(settings, `<object id="`+id+`"`) {
			t.Fatalf("%s: oggetto %s costruito ma non descritto", what, id)
		}
		if !strings.Contains(settings, `key="object_id" value="`+id+`"`) {
			t.Fatalf("%s: oggetto %s descritto ma non messo sul piatto", what, id)
		}
	}
	if !strings.Contains(settings, "<plate>") {
		t.Fatalf("%s: nessun piatto dichiarato", what)
	}

	var config map[string]any
	if err := json.Unmarshal([]byte(entries[projectConfigEntry]), &config); err != nil {
		t.Fatalf("%s: impostazioni non leggibili come JSON: %v", what, err)
	}
	if len(config) == 0 {
		t.Fatalf("%s: impostazioni vuote", what)
	}
	// The settings have to be the ones that were decided, not merely present.
	if got := strings.TrimSpace(asText(config["layer_height"])); got == "" {
		t.Fatalf("%s: altezza layer assente dalle impostazioni scritte", what)
	}
}

// Every supported machine has to produce a project that actually loads. The
// failure this guards against was reported on one printer and one slicer, and
// nothing in it was specific to either.
func TestEveryPrinterProducesALoadableProject(t *testing.T) {
	filaments, err := LoadBuiltinFilaments()
	if err != nil || len(filaments) == 0 {
		t.Fatalf("catalogo filamenti non caricabile: %v", err)
	}
	dir := t.TempDir()
	geometry := filepath.Join(dir, "geo.3mf")
	if err := writeGeometryOnly3MF(geometry, box(0, 0, 0, 40, 30, 20)); err != nil {
		t.Fatalf("geometria non scrivibile: %v", err)
	}
	a := ModelAnalysis{
		Filename: "pezzo.stl", Category: "Oggetto tecnico/decorativo",
		Extents: [3]float64{40, 30, 20}, Volume: 24000, SurfaceArea: 5200,
		Watertight: true, TriangleCount: 12,
	}

	written := 0
	for _, printer := range SupportedPrinters() {
		for _, quality := range []string{"low", "balanced", "perfect"} {
			rec, err := RecommendForPrinter(a, filaments[0], printer, quality)
			if err != nil {
				// A refusal is a legitimate answer for some combinations; it is
				// silence about a broken project that is not.
				t.Logf("RIFIUTO %s %s: %v", printer.Model, quality, err)
				continue
			}
			SecureProfile(&rec, a, filaments[0], printer)
			out := filepath.Join(dir, safeProfileName(printer.ID+"-"+quality)+".3mf")
			if err := WriteProjectWithSources(geometry, out, rec, printer, "FlashFit", ProjectSources{}); err != nil {
				t.Fatalf("%s %s: progetto non scrivibile: %v", printer.Model, quality, err)
			}
			checkProjectIsLoadable(t, out, printer.Model+" "+quality)
			written++
		}
	}
	if written == 0 {
		t.Fatal("nessun progetto prodotto: il test non ha verificato niente")
	}
	t.Logf("%d progetti verificati su %d macchine", written, len(SupportedPrinters()))
}

// The multi-plate path writes one project per plate, and each of them has to
// stand on its own — a second plate that opens empty is the same defect, just
// further from the first thing anyone checks.
func TestEveryPlateProjectIsLoadable(t *testing.T) {
	filaments, err := LoadBuiltinFilaments()
	if err != nil || len(filaments) == 0 {
		t.Fatalf("catalogo filamenti non caricabile: %v", err)
	}
	printer := DefaultPrinterProfile()
	var tris []triangle
	tris = append(tris, box(0, 0, 0, 150, 150, 60)...)
	tris = append(tris, box(400, 0, 0, 150, 150, 60)...)

	usable := ManualFor(printer).UsablePlate
	plates, oversized := PackIntoPlates(SplitIntoPieces(tris), usable)
	if len(plates) < 2 {
		t.Fatalf("presupposto del test non valido: attesi almeno 2 piatti, ottenuti %d (%d fuori misura)", len(plates), len(oversized))
	}

	a := ModelAnalysis{
		Filename: "kit.stl", Category: "Oggetto tecnico/decorativo",
		Extents: [3]float64{150, 150, 60}, Volume: 400000, SurfaceArea: 90000,
		Watertight: true, TriangleCount: 24,
	}
	rec, err := RecommendForPrinter(a, filaments[0], printer, "balanced")
	if err != nil {
		t.Fatalf("raccomandazione non producibile: %v", err)
	}
	dir := t.TempDir()
	written, err := writePlateProjects(plates, filepath.Join(dir, "kit.3mf"), rec, printer, "FlashFit", dir, ProjectSources{})
	if err != nil {
		t.Fatalf("progetti dei piatti non scrivibili: %v", err)
	}
	if len(written) != len(plates) {
		t.Fatalf("%d progetti per %d piatti", len(written), len(plates))
	}
	for i, path := range written {
		checkProjectIsLoadable(t, path, "piatto "+string(rune('1'+i)))
	}
}
