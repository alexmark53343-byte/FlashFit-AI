package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"flashfitai/shared"
)

func TestFilterFilaments(t *testing.T) {
	fs := []shared.Filament{{Brand: "SUNLU", Product: "PETG", Material: "PETG"}, {Brand: "eSUN", Product: "PLA+", Material: "PLA+"}}
	x := filterFilaments(fs, "sun petg")
	if len(x) != 1 || x[0] != 0 {
		t.Fatalf("bad filter: %v", x)
	}
}
func TestChooseProfiles(t *testing.T) {
	d := t.TempDir()
	low := filepath.Join(d, "0.28 Draft AD5M.json")
	bal := filepath.Join(d, "0.20 Standard AD5M.json")
	perf := filepath.Join(d, "0.12 Fine AD5M.json")
	for _, p := range []string{low, bal, perf} {
		os.WriteFile(p, []byte(`{"type":"process","name":"`+filepath.Base(p)+`"}`), 0600)
	}
	if chooseProcess([]string{bal, perf, low}, "perfect") != perf {
		t.Fatal("perfect profile selection")
	}
	if chooseProcess([]string{bal, perf, low}, "low") != low {
		t.Fatal("low profile selection")
	}
}

func TestChooseProcessMatchesSelectedBambuPrinter(t *testing.T) {
	d := t.TempDir()
	a1 := filepath.Join(d, "0.20 Standard BBL A1.json")
	p2s := filepath.Join(d, "0.20 Standard BBL P2S.json")
	os.WriteFile(a1, []byte(`{"type":"process","name":"0.20mm Standard @BBL A1","compatible_printers":["Bambu Lab A1 0.4 nozzle"]}`), 0600)
	os.WriteFile(p2s, []byte(`{"type":"process","name":"0.20mm Standard @BBL P2S","compatible_printers":["Bambu Lab P2S 0.4 nozzle"]}`), 0600)
	printer, _ := shared.PrinterByID("bambu-p2s")
	if got := chooseProcessForPrinter([]string{a1, p2s}, "balanced", printer); got != p2s {
		t.Fatalf("profilo P2S non selezionato: %s", got)
	}
}

func TestChooseProcessMatchesSelectedNozzle(t *testing.T) {
	d := t.TempDir()
	nozzle04 := filepath.Join(d, "0.20 Standard BBL A1 0.4.json")
	nozzle06 := filepath.Join(d, "0.20 Standard BBL A1 0.6.json")
	os.WriteFile(nozzle04, []byte(`{"type":"process","name":"0.20mm Standard @BBL A1","compatible_printers":["Bambu Lab A1 0.4 nozzle"]}`), 0600)
	os.WriteFile(nozzle06, []byte(`{"type":"process","name":"0.20mm Standard @BBL A1","compatible_printers":["Bambu Lab A1 0.6 nozzle"]}`), 0600)
	printer, _ := shared.PrinterByID("bambu-a1")
	printer.NozzleDiameter = 0.6
	if got := chooseProcessForPrinter([]string{nozzle04, nozzle06}, "balanced", printer); got != nozzle06 {
		t.Fatalf("profilo 0.6 non selezionato: %s", got)
	}
}

func TestChooseBaseFilamentMatchesMaterial(t *testing.T) {
	d := t.TempDir()
	pla := filepath.Join(d, "pla.json")
	petg := filepath.Join(d, "petg.json")
	os.WriteFile(pla, []byte(`{"type":"filament","name":"Generic PLA","filament_type":["PLA"]}`), 0600)
	os.WriteFile(petg, []byte(`{"type":"filament","name":"Generic PETG","filament_type":["PETG"]}`), 0600)
	f := shared.Filament{Brand: "X", Product: "PETG", Material: "PETG"}
	if chooseBaseFilament([]string{pla, petg}, f) != petg {
		t.Fatal("material mismatch")
	}
}
func TestMergeOfficialFirst(t *testing.T) {
	base := []shared.Filament{{Brand: "A", Product: "PLA", Material: "PLA", NozzleMin: 190, NozzleMax: 230, NozzleDefault: 210, BedMin: 40, BedMax: 70, BedDefault: 55, FanMin: 0, FanMax: 100, MaxVolumetricSpeed: 12, Density: 1.24, FlowRatio: 1}}
	off := append([]shared.Filament(nil), base...)
	off[0].OfficialProfile = true
	off[0].SourcePath = "x"
	got := mergeFilaments(base, off)
	if len(got) != 2 || !got[0].OfficialProfile {
		t.Fatalf("bad merge %+v", got)
	}
}

func TestVisibleFilamentMatchesCapsHugeCatalog(t *testing.T) {
	fs := make([]shared.Filament, 20000)
	for i := range fs {
		fs[i] = shared.Filament{Brand: "Brand", Product: fmt.Sprintf("PLA-%05d", i), Material: "PLA"}
	}
	got, total := visibleFilamentMatches(fs, "PLA")
	if total != 20000 || len(got) != maxVisibleFilaments {
		t.Fatalf("total=%d visible=%d", total, len(got))
	}
}
