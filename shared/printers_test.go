package shared

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func samplePrinterAnalysis() ModelAnalysis {
	return ModelAnalysis{Filename: "part.stl", InputFormat: "STL", TriangleCount: 12, Extents: [3]float64{20, 20, 20}, SurfaceArea: 2400, Volume: 8000, Watertight: true, BedContactRatio: .16, Category: "Oggetto tecnico/decorativo"}
}

func TestSupportedPrinterCatalogIsCompleteAndUnique(t *testing.T) {
	printers := SupportedPrinters()
	if len(printers) != 20 {
		t.Fatalf("catalogo stampanti inatteso: %d", len(printers))
	}
	seen := map[string]bool{}
	brands := map[string]int{}
	for _, printer := range printers {
		if seen[printer.ID] {
			t.Fatalf("id duplicato: %s", printer.ID)
		}
		seen[printer.ID] = true
		brands[printer.Brand]++
		if printer.NozzleDiameter != .4 || printer.BuildVolume[0] <= 0 || printer.BuildVolume[1] <= 0 || printer.BuildVolume[2] <= 0 {
			t.Fatalf("profilo incompleto: %+v", printer)
		}
		if !strings.HasPrefix(printer.OfficialTechnicalSource, "https://") {
			t.Fatalf("fonte tecnica mancante: %s", printer.ID)
		}
	}
	if brands["Flashforge"] != 6 || brands["Bambu Lab"] != 14 {
		t.Fatalf("ripartizione catalogo inattesa: %#v", brands)
	}
}

func TestPrinterAliasDisambiguation(t *testing.T) {
	tests := map[string]string{
		"Bambu Lab A1 mini 0.4 nozzle":        "bambu-a1-mini",
		"Bambu Lab A1 0.4 nozzle":             "bambu-a1",
		"Bambu Lab H2D Pro 0.4 nozzle":        "bambu-h2d-pro",
		"Bambu Lab H2D 0.4 nozzle":            "bambu-h2d",
		"Flashforge Creator 5 Pro 0.4 nozzle": "flashforge-creator-5-pro",
		"Flashforge Creator 5 0.4 nozzle":     "flashforge-creator-5",
	}
	for text, want := range tests {
		got, ok := MatchPrinterText(text)
		if !ok || got.ID != want {
			t.Fatalf("%q: got=%q ok=%t want=%q", text, got.ID, ok, want)
		}
	}
}

func writeMachineProfile(t *testing.T, name, nozzle string, extra map[string]any) string {
	t.Helper()
	m := map[string]any{"type": "machine", "name": name, "printer_model": strings.TrimSuffix(name, " 0.4 nozzle"), "nozzle_diameter": []string{nozzle}}
	for key, value := range extra {
		m[key] = value
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "machine.json")
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestResolveBambuMachineUsesInstalledLowerLimits(t *testing.T) {
	path := writeMachineProfile(t, "Bambu Lab P2S 0.4 nozzle", "0.4", map[string]any{
		"machine_max_speed_x":        []string{"450"},
		"machine_max_speed_y":        []string{"440"},
		"machine_max_acceleration_x": []string{"9000"},
		"machine_max_acceleration_y": []string{"8500"},
		"printable_height":           "250",
	})
	printer, err := ResolvePrinterProfile(path)
	if err != nil {
		t.Fatal(err)
	}
	if printer.ID != "bambu-p2s" || printer.MaxTravelSpeed != 440 || printer.MaxAcceleration != 8500 || printer.BuildVolume[2] != 250 {
		t.Fatalf("limiti locali non applicati: %+v", printer)
	}
}

func TestResolvePrinterKeepsInstalledNozzle(t *testing.T) {
	path := writeMachineProfile(t, "Bambu Lab A1 mini 0.6 nozzle", "0.6", nil)
	printer, err := ResolvePrinterProfile(path)
	if err != nil {
		t.Fatal(err)
	}
	if printer.ID != "bambu-a1-mini" || printer.NozzleDiameter != 0.6 {
		t.Fatalf("ugello installato non mantenuto: %+v", printer)
	}
}

func TestPrinterSpecificBuildVolume(t *testing.T) {
	a := samplePrinterAnalysis()
	a.Extents = [3]float64{200, 190, 190}
	mini, _ := PrinterByID("bambu-a1-mini")
	h2d, _ := PrinterByID("bambu-h2d")
	if ValidateModelForPrinter(a, mini) == nil {
		t.Fatal("A1 mini ha accettato un modello fuori volume")
	}
	if err := ValidateModelForPrinter(a, h2d); err != nil {
		t.Fatalf("H2D ha rifiutato un modello valido: %v", err)
	}
}

func TestAllPrintersProduceBoundedQualityModes(t *testing.T) {
	a := samplePrinterAnalysis()
	a.Extents = [3]float64{40, 40, 80}
	filament := sampleFilament()
	for _, printer := range SupportedPrinters() {
		fast, err := RecommendForPrinter(a, filament, printer, "low")
		if err != nil {
			t.Fatalf("%s veloce: %v", printer.ID, err)
		}
		balanced, err := RecommendForPrinter(a, filament, printer, "balanced")
		if err != nil {
			t.Fatalf("%s bilanciata: %v", printer.ID, err)
		}
		perfect, err := RecommendForPrinterWithTexture(a, filament, printer, "perfect", "satin")
		if err != nil {
			t.Fatalf("%s perfetta: %v", printer.ID, err)
		}
		if fast.EstimatedModeMinutes > balanced.EstimatedModeMinutes+0.01 {
			t.Fatalf("%s: Veloce più lenta di Bilanciata", printer.ID)
		}
		// The upper bound was 1.7 while the tier ratios were hand-written
		// constants. Two real prints of the same part - Fast 3 h 31 m, Perfect
		// 13 h 14 m - put the true Fast-to-Perfect spread at 3.8, which places
		// Perfect around 2.3x Balanced. The bound follows the measurement.
		if perfect.EstimatedModeMinutes <= balanced.EstimatedModeMinutes || perfect.EstimatedModeMinutes > balanced.EstimatedModeMinutes*2.8 {
			t.Fatalf("%s: tempo Perfetta fuori fascia %.2f/%.2f", printer.ID, perfect.EstimatedModeMinutes, balanced.EstimatedModeMinutes)
		}
		for _, rec := range []Recommendation{fast, balanced, perfect} {
			if rec.CriticalValues["nozzle_temperature"] > printer.MaxNozzleTemperature || rec.CriticalValues["bed_temperature"] > printer.MaxBedTemperature {
				t.Fatalf("%s: temperatura macchina superata", printer.ID)
			}
			if rec.CriticalValues["outer_acceleration"] > printer.MaxAcceleration {
				t.Fatalf("%s: accelerazione macchina superata", printer.ID)
			}
		}
	}
}

func TestTallBedSlingerUsesAntiRingingGuardrail(t *testing.T) {
	a := samplePrinterAnalysis()
	a.Extents = [3]float64{18, 18, 150}
	a.ThinOrTall = true
	printer, _ := PrinterByID("bambu-a1")
	rec, err := RecommendForPrinter(a, sampleFilament(), printer, "low")
	if err != nil {
		t.Fatal(err)
	}
	if rec.CriticalValues["outer_acceleration"] > 750 || rec.CriticalValues["outer_wall_speed"] > 42 {
		t.Fatalf("guardrail bed-slinger assente: %+v", rec.CriticalValues)
	}
}

func TestPatchBambuProfilePreservesVendorGCode(t *testing.T) {
	dir := t.TempDir()
	process := filepath.Join(dir, "process.json")
	filamentPath := filepath.Join(dir, "filament.json")
	if err := os.WriteFile(process, []byte(`{"type":"process","name":"0.20mm Standard @BBL P2S","compatible_printers":["Bambu Lab P2S 0.4 nozzle"],"machine_start_gcode":"DO_NOT_TOUCH","change_filament_gcode":"DO_NOT_TOUCH"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filamentPath, []byte(`{"type":"filament","name":"Generic PLA","filament_type":["PLA"],"filament_start_gcode":"DO_NOT_TOUCH","enable_pressure_advance":["1"],"pressure_advance":["0.027"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	printer, _ := PrinterByID("bambu-p2s")
	rec, err := RecommendForPrinter(samplePrinterAnalysis(), sampleFilament(), printer, "balanced")
	if err != nil {
		t.Fatal(err)
	}
	patchedProcess, patchedFilament, _, err := PatchProfilesForPrinter(process, filamentPath, rec, sampleFilament(), printer, filepath.Join(dir, "out"))
	if err != nil {
		t.Fatal(err)
	}
	pm, _ := readJSONMap(patchedProcess)
	fm, _ := readJSONMap(patchedFilament)
	if mapText(pm, "machine_start_gcode") != "DO_NOT_TOUCH" || mapText(pm, "change_filament_gcode") != "DO_NOT_TOUCH" || mapText(fm, "filament_start_gcode") != "DO_NOT_TOUCH" {
		t.Fatal("G-code vendor modificato")
	}
	if mapText(fm, "enable_pressure_advance") != "1" || mapText(fm, "pressure_advance") != "0.027" {
		t.Fatal("Flow Dynamics/Pressure Advance vendor modificato senza calibrazione misurata")
	}
}
