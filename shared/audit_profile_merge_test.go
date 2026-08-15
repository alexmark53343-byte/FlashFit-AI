package shared

import (
	"os"
	"path/filepath"
	"testing"
)

// PROBE: a cyclic "inherits" chain in installed profiles must not loop forever.
func TestAudit_CyclicInheritance(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.json")
	b := filepath.Join(dir, "b.json")
	os.WriteFile(a, []byte(`{"inherits":"b","layer_height":"0.2"}`), 0o600)
	os.WriteFile(b, []byte(`{"inherits":"a","wall_loops":"3"}`), 0o600)
	done := make(chan bool, 1)
	go func() {
		defer func() { recover(); done <- true }()
		_ = resolveProfileChain(a, 0)
	}()
	select {
	case <-done:
	case <-timeoutProbe():
		t.Fatal("resolveProfileChain in loop su inherits ciclico")
	}
}

// PROBE: malformed installed profile (not JSON, empty, truncated) must degrade
// gracefully, not crash the whole recommendation.
func TestAudit_MalformedInstalledProfile(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.json")
	for _, content := range []string{"", "{not json", `{"inherits":"nonexistent"}`, "null", "[]"} {
		os.WriteFile(bad, []byte(content), 0o600)
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("panic con profilo %q: %v", content, r)
				}
			}()
			m := mergedBaseSettings(bad, "", "")
			_ = m
		}()
	}
}

// PROBE: project generation with EMPTY source profile paths (no installed
// profiles found). Must still produce a usable project.
func TestAudit_NoInstalledProfiles(t *testing.T) {
	geometry := filepath.Join(t.TempDir(), "geo.3mf")
	if err := writeGeometryOnly3MF(geometry, box(0, 0, 0, 40, 30, 20)); err != nil {
		t.Fatal(err)
	}
	printer := DefaultPrinterProfile()
	filaments, _ := LoadBuiltinFilaments()
	a := ModelAnalysis{Filename: "x.stl", Category: "Oggetto tecnico/decorativo",
		Extents: [3]float64{40, 30, 20}, Volume: 24000, SurfaceArea: 5200, Watertight: true, TriangleCount: 12}
	rec, _ := RecommendForPrinter(a, filaments[0], printer, "balanced")
	out := filepath.Join(t.TempDir(), "p.3mf")
	// Empty sources: no machine/process/filament base profiles.
	if err := WriteProjectWithSources(geometry, out, rec, printer, "FlashFit", ProjectSources{}); err != nil {
		t.Fatalf("progetto non generato senza profili installati: %v", err)
	}
	entries := projectEntries(t, out)
	if _, ok := entries[projectConfigEntry]; !ok {
		t.Fatal("nessuna configurazione nel progetto")
	}
	// Even without installed profiles the config must carry the decided layer.
	var cfg map[string]any
	if jsonUnmarshalReal([]byte(entries[projectConfigEntry]), &cfg) != nil {
		t.Fatal("configurazione non è JSON valido")
	}
	if _, ok := cfg["layer_height"]; !ok {
		t.Error("altezza layer assente dal progetto senza profili installati")
	}
}
