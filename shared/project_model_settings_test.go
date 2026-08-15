package shared

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func projectEntries(t *testing.T, path string) map[string]string {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("progetto non leggibile: %v", err)
	}
	defer zr.Close()
	out := map[string]string{}
	for _, entry := range zr.File {
		r, err := entry.Open()
		if err != nil {
			t.Fatalf("voce %s non leggibile: %v", entry.Name, err)
		}
		body, _ := io.ReadAll(r)
		r.Close()
		out[entry.Name] = string(body)
	}
	return out
}

func writeTestProject(t *testing.T, geometry string) string {
	t.Helper()
	printer := DefaultPrinterProfile()
	filaments, err := LoadBuiltinFilaments()
	if err != nil || len(filaments) == 0 {
		t.Fatalf("catalogo filamenti non caricabile: %v", err)
	}
	a := ModelAnalysis{
		Filename: "pezzo.stl", Category: "Oggetto tecnico/decorativo",
		Extents: [3]float64{40, 30, 20}, Volume: 24000, SurfaceArea: 5200,
		Watertight: true, TriangleCount: 12,
	}
	rec, err := RecommendForPrinter(a, filaments[0], printer, "balanced")
	if err != nil {
		t.Fatalf("raccomandazione non producibile: %v", err)
	}
	out := filepath.Join(t.TempDir(), "progetto.3mf")
	if err := WriteProjectWithSources(geometry, out, rec, printer, "FlashFit", ProjectSources{}); err != nil {
		t.Fatalf("progetto non scrivibile: %v", err)
	}
	return out
}

// The reported failure, exactly: Bambu Studio imported the settings and left the
// plate empty.
//
// A 3MF with a valid build is loaded object-by-object when it is opened as a
// model. Attaching project_settings.config changes what the file is — the
// slicer recognises a project, and a project lists its objects and plates in
// model_settings.config. Ours listed none, so nothing was placed.
func TestProjectDescribesItsObjectsAndPlate(t *testing.T) {
	geometry := filepath.Join(t.TempDir(), "geo.3mf")
	if err := writeGeometryOnly3MF(geometry, box(0, 0, 0, 40, 30, 20)); err != nil {
		t.Fatalf("geometria non scrivibile: %v", err)
	}
	entries := projectEntries(t, writeTestProject(t, geometry))

	settings, ok := entries[modelSettingsEntry]
	if !ok {
		t.Fatal("il progetto non descrive alcun oggetto: lo slicer importa le impostazioni e lascia il piatto vuoto")
	}
	// Every object the build declares has to be described and placed, or the
	// instance points at nothing and the plate is empty again.
	for _, id := range buildItemObjectIDs([]byte(entries[modelEntry])) {
		if !strings.Contains(settings, `<object id="`+id+`"`) {
			t.Fatalf("oggetto %s costruito ma non descritto", id)
		}
		if !strings.Contains(settings, `key="object_id" value="`+id+`"`) {
			t.Fatalf("oggetto %s descritto ma non messo sul piatto", id)
		}
	}
	if !strings.Contains(settings, "<plate>") {
		t.Fatal("nessun piatto dichiarato")
	}
}

// A 3MF that came from a slicer already describes its own objects, parts and
// placements. Replacing that with a synthesised description would throw away
// real positions and put every part back at the origin.
func TestExistingModelSettingsAreLeftAlone(t *testing.T) {
	geometry := filepath.Join(t.TempDir(), "geo.3mf")
	if err := writeGeometryOnly3MF(geometry, box(0, 0, 0, 40, 30, 20)); err != nil {
		t.Fatalf("geometria non scrivibile: %v", err)
	}
	const theirs = `<?xml version="1.0"?><config><object id="7"><metadata key="name" value="loro"/></object></config>`
	withSettings := filepath.Join(t.TempDir(), "conSettings.3mf")
	if err := copyZipAdding(geometry, withSettings, modelSettingsEntry, theirs); err != nil {
		t.Fatalf("preparazione non riuscita: %v", err)
	}

	entries := projectEntries(t, writeTestProject(t, withSettings))
	if entries[modelSettingsEntry] != theirs {
		t.Fatalf("la descrizione originale è stata sovrascritta:\n%s", entries[modelSettingsEntry])
	}
}

// A build that places nothing must not gain a plate claiming to be empty on
// purpose: writing no description is the honest answer there.
func TestNoObjectsMeansNoInventedPlate(t *testing.T) {
	if got := modelSettingsXML(nil, "x"); got != "" && strings.Contains(got, "<model_instance>") {
		t.Fatal("piatto con istanze inventate a partire da nessun oggetto")
	}
	if ids := buildItemObjectIDs([]byte(`<model><resources/><build></build></model>`)); len(ids) != 0 {
		t.Fatalf("oggetti trovati dove non ce ne sono: %v", ids)
	}
}

// copyZipAdding rewrites an archive with one extra entry, for building fixtures.
func copyZipAdding(src, dst, name, body string) error {
	zr, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer zr.Close()
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for _, entry := range zr.File {
		r, err := entry.Open()
		if err != nil {
			return err
		}
		w, err := zw.Create(entry.Name)
		if err != nil {
			r.Close()
			return err
		}
		_, err = io.Copy(w, r)
		r.Close()
		if err != nil {
			return err
		}
	}
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	if _, err = io.WriteString(w, body); err != nil {
		return err
	}
	return zw.Close()
}
