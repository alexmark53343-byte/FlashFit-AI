package shared

import (
	"archive/zip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// The settings were being written to Metadata/Slic3r_PE.config as key = value
// lines. That is PrusaSlicer's convention; Orca, Bambu Studio and Flash Studio
// read Metadata/project_settings.config and expect JSON. The file travelled
// inside every project and was ignored by all of them, so a profile asking for
// a 0.14 mm layer opened at the slicer's own 0.20 mm default — and the time and
// weight estimates disagreed with the sliced result for the same reason.
//
// This pins the entry name, the format, and that the values actually arrive.
func TestProjectCarriesSettingsWhereTheSlicerReadsThem(t *testing.T) {
	dir := t.TempDir()
	geometry := filepath.Join(dir, "geometry.3mf")
	if err := writeGeometryOnly3MF(geometry, selfTestCubeTriangles()); err != nil {
		t.Fatalf("geometria di prova non scrivibile: %v", err)
	}

	rec := Recommendation{
		Process: map[string]any{
			"layer_height":     "0.14",
			"wall_loops":       "4",
			"outer_wall_speed": "34",
		},
		Filament: map[string]any{
			"nozzle_temperature": "215",
			"filament_density":   "1.24",
		},
	}
	printer := DefaultPrinterProfile()
	project := filepath.Join(dir, "project.3mf")
	if err := WriteProjectWithSettings(geometry, project, rec, printer, "FlashFit Perfetta"); err != nil {
		t.Fatalf("progetto non scrivibile: %v", err)
	}

	reader, err := zip.OpenReader(project)
	if err != nil {
		t.Fatalf("progetto non è uno zip valido: %v", err)
	}
	defer reader.Close()

	var raw []byte
	for _, entry := range reader.File {
		if entry.Name != "Metadata/project_settings.config" {
			continue
		}
		f, err := entry.Open()
		if err != nil {
			t.Fatalf("voce non apribile: %v", err)
		}
		raw, err = io.ReadAll(f)
		f.Close()
		if err != nil {
			t.Fatalf("voce non leggibile: %v", err)
		}
	}
	if len(raw) == 0 {
		t.Fatal("il progetto non contiene Metadata/project_settings.config: lo slicer userà i propri default")
	}

	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatalf("la configurazione non è JSON valido, lo slicer la scarterà: %v", err)
	}

	// The value that made the bug visible: perfect quality asked for 0.14 and
	// the slicer showed 0.20.
	if config["layer_height"] != "0.14" {
		t.Fatalf("altezza layer non arrivata allo slicer: %v", config["layer_height"])
	}
	if config["wall_loops"] != "4" {
		t.Fatalf("numero pareti non arrivato: %v", config["wall_loops"])
	}
	// Per-extruder settings must be arrays, or the slicer rejects the file.
	nozzle, ok := config["nozzle_temperature"].([]any)
	if !ok || len(nozzle) != 1 || nozzle[0] != "215" {
		t.Fatalf("temperatura ugello non nel formato per estrusore: %#v", config["nozzle_temperature"])
	}
	if _, ok := config["print_settings_id"]; !ok {
		t.Fatal("il profilo non è identificato nel progetto")
	}
}

// Two runs of the same recommendation must produce the same bytes, so a project
// stays verifiable.
func TestProjectConfigIsReproducible(t *testing.T) {
	rec := Recommendation{
		Process:  map[string]any{"layer_height": "0.14", "wall_loops": "4"},
		Filament: map[string]any{"nozzle_temperature": "215"},
	}
	printer := DefaultPrinterProfile()
	first := projectConfigText(rec, printer, "P")
	second := projectConfigText(rec, printer, "P")
	if first != second {
		t.Fatal("la configurazione cambia fra due esecuzioni identiche")
	}
	if _, err := os.Stat(t.TempDir()); err != nil {
		t.Fatal(err)
	}
}
