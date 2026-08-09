package shared

import (
	"archive/zip"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func writeCubeSTL(t *testing.T, path string) {
	t.Helper()
	v := [][3]float32{{0, 0, 0}, {20, 0, 0}, {20, 20, 0}, {0, 20, 0}, {0, 0, 20}, {20, 0, 20}, {20, 20, 20}, {0, 20, 20}}
	faces := [][3]int{{0, 2, 1}, {0, 3, 2}, {4, 5, 6}, {4, 6, 7}, {0, 1, 5}, {0, 5, 4}, {1, 2, 6}, {1, 6, 5}, {2, 3, 7}, {2, 7, 6}, {3, 0, 4}, {3, 4, 7}}
	f, e := os.Create(path)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	f.Write(make([]byte, 80))
	binary.Write(f, binary.LittleEndian, uint32(len(faces)))
	for _, fc := range faces {
		binary.Write(f, binary.LittleEndian, [3]float32{})
		for _, i := range fc {
			binary.Write(f, binary.LittleEndian, v[i])
		}
		binary.Write(f, binary.LittleEndian, uint16(0))
	}
}
func sampleFilament() Filament {
	return Filament{Brand: "Test", Product: "PLA", Material: "PLA", Variant: "", NozzleMin: 190, NozzleMax: 230, NozzleDefault: 215, BedMin: 45, BedMax: 65, BedDefault: 55, FanMin: 80, FanMax: 100, MaxVolumetricSpeed: 16, Density: 1.24, FlowRatio: 1}
}
func writeBaseProfiles(t *testing.T, dir string) (string, string, string) {
	t.Helper()
	machine := filepath.Join(dir, "machine.json")
	process := filepath.Join(dir, "process.json")
	fil := filepath.Join(dir, "filament.json")
	os.WriteFile(machine, []byte(`{"type":"machine","name":"Flashforge Adventurer 5M 0.4 Nozzle","nozzle_diameter":["0.4"]}`), 0600)
	os.WriteFile(process, []byte(`{"type":"process","name":"0.20 Standard @ Flashforge Adventurer 5M 0.4","compatible_printers":["Flashforge Adventurer 5M 0.4 Nozzle"],"machine_start_gcode":"DO_NOT_TOUCH"}`), 0600)
	os.WriteFile(fil, []byte(`{"type":"filament","name":"Generic PLA","filament_type":["PLA"],"filament_start_gcode":"DO_NOT_TOUCH"}`), 0600)
	return machine, process, fil
}
func writeValid3MF(t *testing.T, path string, markers []string, recs ...Recommendation) {
	t.Helper()
	f, e := os.Create(path)
	if e != nil {
		t.Fatal(e)
	}
	z := zip.NewWriter(f)
	w, _ := z.Create("3D/3dmodel.model")
	w.Write([]byte(`<?xml version="1.0"?><model unit="millimeter" xmlns="http://schemas.microsoft.com/3dmanufacturing/core/2015/02"><resources><object id="1" type="model"><mesh><vertices><vertex x="0" y="0" z="0"/><vertex x="20" y="0" z="0"/><vertex x="20" y="20" z="0"/><vertex x="0" y="20" z="0"/><vertex x="0" y="0" z="20"/><vertex x="20" y="0" z="20"/><vertex x="20" y="20" z="20"/><vertex x="0" y="20" z="20"/></vertices><triangles><triangle v1="0" v2="2" v3="1"/><triangle v1="0" v2="3" v3="2"/><triangle v1="4" v2="5" v3="6"/><triangle v1="4" v2="6" v3="7"/><triangle v1="0" v2="1" v3="5"/><triangle v1="0" v2="5" v3="4"/><triangle v1="1" v2="2" v3="6"/><triangle v1="1" v2="6" v3="5"/><triangle v1="2" v2="3" v3="7"/><triangle v1="2" v2="7" v3="6"/><triangle v1="3" v2="0" v3="4"/><triangle v1="3" v2="4" v3="7"/></triangles></mesh></object></resources><build><item objectid="1"/></build></model>`))
	cfg, _ := z.Create("Metadata/flashfit.config")
	config := strings.Join(markers, "\n") + "\nlayer_height=0.2\nouter_wall_speed=55\nouter_wall_acceleration=1500\nfilament_max_volumetric_speed=16\nnozzle_temperature=215"
	if len(recs) > 0 {
		r := recs[0]
		config = fmt.Sprintf("%s\nlayer_height=%s\nouter_wall_speed=%s\nouter_wall_acceleration=%s\nfilament_max_volumetric_speed=%s\nnozzle_temperature=%s", strings.Join(markers, "\n"), fmt2(r.CriticalValues["layer_height"]), fmt0(r.CriticalValues["outer_wall_speed"]), fmt0(r.CriticalValues["outer_acceleration"]), fmt2(r.CriticalValues["max_volumetric_speed"]), fmt0(r.CriticalValues["nozzle_temperature"]))
		for key, value := range r.CriticalSettings {
			config += "\n" + key + "=" + value
		}
	}
	cfg.Write([]byte(config))
	z.Close()
	f.Close()
}
func TestAnalyzeCube(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "cube.stl")
	writeCubeSTL(t, p)
	a, e := AnalyzeSTL(p)
	if e != nil {
		t.Fatal(e)
	}
	if !a.Watertight {
		t.Fatalf("cube should be watertight: %+v", a)
	}
	for _, x := range a.Extents {
		if math.Abs(x-20) > 0.001 {
			t.Fatalf("bad extent %v", a.Extents)
		}
	}
	if e = ValidateAnalysis(a); e != nil {
		t.Fatal(e)
	}
}
func TestRecommendationCapsFlow(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "cube.stl")
	writeCubeSTL(t, p)
	a, _ := AnalyzeSTL(p)
	f := sampleFilament()
	f.MaxVolumetricSpeed = 8
	r, e := Recommend(a, f, "low")
	if e != nil {
		t.Fatal(e)
	}
	outer := r.CriticalValues["outer_wall_speed"]
	if outer*r.CriticalValues["layer_height"]*0.42 > 8*0.75 {
		t.Fatalf("flow cap failed %.2f", outer)
	}
}
func TestPatchPreservesGcode(t *testing.T) {
	d := t.TempDir()
	_, bp, bf := writeBaseProfiles(t, d)
	p := filepath.Join(d, "cube.stl")
	writeCubeSTL(t, p)
	a, _ := AnalyzeSTL(p)
	f := sampleFilament()
	r, _ := Recommend(a, f, "balanced")
	pp, ff, _, e := PatchProfiles(bp, bf, r, f, filepath.Join(d, "out"))
	if e != nil {
		t.Fatal(e)
	}
	pm, _ := readJSONMap(pp)
	fm, _ := readJSONMap(ff)
	if mapText(pm, "machine_start_gcode") != "DO_NOT_TOUCH" || mapText(fm, "filament_start_gcode") != "DO_NOT_TOUCH" {
		t.Fatal("base gcode modified")
	}
}
func TestValidate3MF(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "x.3mf")
	m := []string{"FlashFit Bilanciata AD5M 0.4", "FlashFit Test PLA"}
	writeValid3MF(t, p, m)
	if e := Validate3MF(p, m, Recommendation{}); e != nil {
		t.Fatal(e)
	}
}
func TestBuildAndOpenAtomic(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("linux fake slicer test")
	}
	d := t.TempDir()
	machine, bp, bf := writeBaseProfiles(t, d)
	model := filepath.Join(d, "cube.stl")
	writeCubeSTL(t, model)
	a, _ := AnalyzeSTL(model)
	template := filepath.Join(d, "template.3mf")
	templateRec, _ := Recommend(a, sampleFilament(), "balanced")
	writeValid3MF(t, template, []string{"FlashFit Bilanciata AD5M 0.4", "FlashFit Test PLA"}, templateRec)
	script := filepath.Join(d, "orca-slicer")
	body := "#!/bin/sh\nout=''\nprev=''\nfor a in \"$@\"; do if [ \"$prev\" = '--export-3mf' ]; then out=\"$a\"; fi; prev=\"$a\"; done\nif [ -n \"$out\" ]; then cp '" + template + "' \"$out\"; fi\nexit 0\n"
	os.WriteFile(script, []byte(body), 0700)
	out := filepath.Join(d, "out")
	res, e := BuildAndOpen(ImportRequest{Model: a, Filament: sampleFilament(), Quality: "balanced", SlicerExe: script, Machine: machine, BaseProcess: bp, BaseFilament: bf, OutputDir: out})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(res.ProjectPath); e != nil {
		t.Fatal(e)
	}
	orig, _ := fileSHA256(model)
	if orig != a.SHA256 {
		t.Fatal("model changed")
	}
	b, _ := os.ReadFile(res.SummaryPath)
	var x any
	if json.Unmarshal(b, &x) != nil {
		t.Fatal("bad summary")
	}
}

func TestAllBuiltinSafeProfilesAllQualities(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "cube.stl")
	writeCubeSTL(t, p)
	a, _ := AnalyzeSTL(p)
	fs, e := LoadBuiltinFilaments()
	if e != nil {
		t.Fatal(e)
	}
	count := 0
	for _, f := range fs {
		for _, q := range []string{"low", "balanced", "perfect"} {
			r, e := Recommend(a, f, q)
			if e != nil {
				t.Fatalf("%s %s: %v", f.Product, q, e)
			}
			c := r.CriticalValues
			if c["outer_wall_speed"]*c["layer_height"]*0.42 > f.MaxVolumetricSpeed*0.75 {
				t.Fatalf("MVS exceeded for %s", f.Product)
			}
			if c["nozzle_temperature"] > 280 || c["bed_temperature"] > 110 {
				t.Fatal("machine temp limit exceeded")
			}
			count++
		}
	}
	if count < 60 {
		t.Fatalf("coverage too low: %d", count)
	}
}

func TestPerfectPremiumTexturesWriteRealSlicerSettings(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "cube.stl")
	writeCubeSTL(t, p)
	a, _ := AnalyzeSTL(p)
	f := sampleFilament()
	want := map[string]struct {
		top, ironing, fuzzy string
	}{
		"satin":       {"monotonicline", "topmost", "none"},
		"prism":       {"octagramspiral", "no ironing", "none"},
		"carbon":      {"hilbertcurve", "no ironing", "external"},
		"topographic": {"archimedeanchords", "no ironing", "none"},
	}
	for id, expected := range want {
		r, err := RecommendWithTexture(a, f, "perfect", id)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		if r.Texture != id || fmt.Sprint(r.Process["top_surface_pattern"]) != expected.top || fmt.Sprint(r.Process["ironing_type"]) != expected.ironing || fmt.Sprint(r.Process["fuzzy_skin"]) != expected.fuzzy {
			t.Fatalf("texture %s non applicata: %+v", id, r.Process)
		}
		if r.Process["wall_generator"] != "arachne" || r.Process["wall_loops"] != "4" || r.Process["top_shell_thickness"] != "1" {
			t.Fatalf("texture %s ha indebolito il profilo", id)
		}
	}
}

func TestEveryBuiltinFilamentSupportsEveryPremiumTexture(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "cube.stl")
	writeCubeSTL(t, p)
	a, _ := AnalyzeSTL(p)
	filaments, err := LoadBuiltinFilaments()
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, filament := range filaments {
		for _, texture := range []string{"satin", "prism", "carbon", "topographic"} {
			r, recErr := RecommendWithTexture(a, filament, "perfect", texture)
			if recErr != nil {
				t.Fatalf("%s / %s: %v", filament.Product, texture, recErr)
			}
			if r.CriticalSettings["top_surface_pattern"] == "" || r.CriticalSettings["wall_loops"] != "4" {
				t.Fatalf("profilo incompleto per %s / %s", filament.Product, texture)
			}
			count++
		}
	}
	if count < 80 {
		t.Fatalf("copertura texture troppo bassa: %d", count)
	}
}

func TestNonPerfectCannotAccidentallyEnableTexture(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "cube.stl")
	writeCubeSTL(t, p)
	a, _ := AnalyzeSTL(p)
	r, err := RecommendWithTexture(a, sampleFilament(), "balanced", "carbon")
	if err != nil {
		t.Fatal(err)
	}
	if r.Texture != "none" || r.Process["fuzzy_skin"] != "none" || r.Process["ironing_type"] != "no ironing" {
		t.Fatalf("texture leaked into balanced: %+v", r)
	}
}

func TestDurationAdaptiveModesAvoidPointlessSpeedups(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "cube.stl")
	writeCubeSTL(t, p)
	short, _ := AnalyzeSTL(p)
	filament := sampleFilament()

	check := func(name string, analysis ModelAnalysis, wantClass string, fastMin, fastMax, perfectMax float64) {
		t.Helper()
		fast, err := RecommendWithTexture(analysis, filament, "low", "")
		if err != nil {
			t.Fatalf("%s fast: %v", name, err)
		}
		balanced, _ := RecommendWithTexture(analysis, filament, "balanced", "")
		perfect, _ := RecommendWithTexture(analysis, filament, "perfect", "satin")
		if fast.DurationClass != wantClass || balanced.DurationClass != wantClass || perfect.DurationClass != wantClass {
			t.Fatalf("%s classi errate: %s/%s/%s", name, fast.DurationClass, balanced.DurationClass, perfect.DurationClass)
		}
		if fast.EstimatedRelativeTime < fastMin || fast.EstimatedRelativeTime > fastMax {
			t.Fatalf("%s fast ratio eccessivo: %.2f", name, fast.EstimatedRelativeTime)
		}
		if perfect.EstimatedRelativeTime <= 1 || perfect.EstimatedRelativeTime > perfectMax {
			t.Fatalf("%s perfect ratio eccessivo: %.2f", name, perfect.EstimatedRelativeTime)
		}
		if !(fast.EstimatedModeMinutes < balanced.EstimatedModeMinutes && balanced.EstimatedModeMinutes < perfect.EstimatedModeMinutes) {
			t.Fatalf("%s ordine tempi errato: %.1f / %.1f / %.1f", name, fast.EstimatedModeMinutes, balanced.EstimatedModeMinutes, perfect.EstimatedModeMinutes)
		}
	}

	check("short", short, "short", 0.94, 0.98, 1.55)
	if r, _ := Recommend(short, filament, "low"); r.CriticalValues["layer_height"] != 0.20 {
		t.Fatalf("stampa breve degradata inutilmente: layer %.2f", r.CriticalValues["layer_height"])
	}

	medium := short
	medium.Extents, medium.SurfaceArea, medium.Volume = [3]float64{35, 35, 35}, 7350, 42875
	medium.Category, medium.ThinOrTall, medium.SupportSuggested, medium.BrimSuggested = "Oggetto tecnico/decorativo", false, false, false
	check("medium", medium, "medium", 0.82, 0.86, 1.65)

	long := short
	long.Extents, long.SurfaceArea, long.Volume = [3]float64{60, 60, 60}, 21600, 216000
	long.Category, long.ThinOrTall, long.SupportSuggested, long.BrimSuggested = "Oggetto grande", false, false, false
	check("long", long, "long", 0.70, 0.74, 1.58)
}

func TestAllGeneratedProfilesRespectOfficialAD5MLimits(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "cube.stl")
	writeCubeSTL(t, p)
	a, _ := AnalyzeSTL(p)
	filaments, err := LoadBuiltinFilaments()
	if err != nil {
		t.Fatal(err)
	}
	parse := func(value any) float64 {
		t.Helper()
		n, parseErr := strconv.ParseFloat(strings.TrimSuffix(fmt.Sprint(value), "%"), 64)
		if parseErr != nil {
			t.Fatalf("valore non numerico: %v", value)
		}
		return n
	}
	printSpeeds := []string{"outer_wall_speed", "inner_wall_speed", "sparse_infill_speed", "internal_solid_infill_speed", "top_surface_speed", "small_perimeter_speed", "gap_infill_speed", "bridge_speed", "support_speed", "support_interface_speed", "initial_layer_speed", "initial_layer_infill_speed", "ironing_speed"}
	accelerations := []string{"outer_wall_acceleration", "inner_wall_acceleration", "sparse_infill_acceleration", "internal_solid_infill_acceleration", "top_surface_acceleration", "bridge_acceleration", "initial_layer_acceleration", "travel_acceleration"}
	medium, long := a, a
	medium.Extents, medium.SurfaceArea, medium.Volume = [3]float64{35, 35, 35}, 7350, 42875
	medium.Category, medium.ThinOrTall, medium.SupportSuggested, medium.BrimSuggested = "Oggetto tecnico/decorativo", false, false, false
	long.Extents, long.SurfaceArea, long.Volume = [3]float64{60, 60, 60}, 21600, 216000
	long.Category, long.ThinOrTall, long.SupportSuggested, long.BrimSuggested = "Oggetto grande", false, false, false
	for _, analysis := range []ModelAnalysis{a, medium, long} {
		for _, filament := range filaments {
			for _, quality := range []string{"low", "balanced", "perfect"} {
				r, recErr := Recommend(analysis, filament, quality)
				if recErr != nil {
					t.Fatalf("%s/%s: %v", filament.Product, quality, recErr)
				}
				layer := parse(r.Process["layer_height"])
				if layer < 0.10 || layer > 0.40 {
					t.Fatalf("layer fuori manuale AD5M: %.2f", layer)
				}
				for _, key := range printSpeeds {
					if speed := parse(r.Process[key]); speed < 0 || speed > 300 {
						t.Fatalf("%s %s velocità fuori limite: %s=%.0f", filament.Product, quality, key, speed)
					}
				}
				if travel := parse(r.Process["travel_speed"]); travel > 600 {
					t.Fatalf("travel fuori limite: %.0f", travel)
				}
				for _, key := range accelerations {
					if accel := parse(r.Process[key]); accel < 0 || accel > 20000 {
						t.Fatalf("%s accelerazione fuori limite: %.0f", key, accel)
					}
				}
			}
		}
	}
}
func TestOpenMeshRejected(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "open.stl")
	writeCubeSTL(t, p)
	b, _ := os.ReadFile(p)
	count := binary.LittleEndian.Uint32(b[80:84])
	binary.LittleEndian.PutUint32(b[80:84], count-1)
	b = b[:len(b)-50]
	os.WriteFile(p, b, 0600)
	a, e := AnalyzeSTL(p)
	if e != nil {
		t.Fatal(e)
	}
	if a.Watertight {
		t.Fatal("open mesh accepted")
	}
	if ValidateAnalysis(a) == nil {
		t.Fatal("open mesh not blocked")
	}
}
func TestOversizedModelRejected(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "big.stl")
	writeCubeSTL(t, p)
	b, _ := os.ReadFile(p)
	for off := 84; off < len(b); off += 50 {
		for v := 0; v < 3; v++ {
			pos := off + 12 + v*12
			bits := binary.LittleEndian.Uint32(b[pos : pos+4])
			x := math.Float32frombits(bits) * 20
			binary.LittleEndian.PutUint32(b[pos:pos+4], math.Float32bits(x))
		}
	}
	os.WriteFile(p, b, 0600)
	a, e := AnalyzeSTL(p)
	if e != nil {
		t.Fatal(e)
	}
	if ValidateAnalysis(a) == nil {
		t.Fatal("oversized model accepted")
	}
}
func TestBaseFilamentMismatchBlocked(t *testing.T) {
	d := t.TempDir()
	_, bp, bf := writeBaseProfiles(t, d)
	os.WriteFile(bf, []byte(`{"type":"filament","name":"Generic PETG","filament_type":["PETG"]}`), 0600)
	p := filepath.Join(d, "cube.stl")
	writeCubeSTL(t, p)
	a, _ := AnalyzeSTL(p)
	f := sampleFilament()
	r, _ := Recommend(a, f, "balanced")
	if _, _, _, e := PatchProfiles(bp, bf, r, f, filepath.Join(d, "out")); e == nil {
		t.Fatal("material mismatch accepted")
	}
}
func Test3MFMissingCriticalRejected(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "x.3mf")
	f, e := os.Create(p)
	if e != nil {
		t.Fatal(e)
	}
	z := zip.NewWriter(f)
	w, _ := z.Create("3D/3dmodel.model")
	w.Write([]byte(`<?xml version="1.0"?><model><resources><object id="1"><mesh><vertices><vertex x="0" y="0" z="0"/><vertex x="1" y="0" z="0"/><vertex x="0" y="1" z="0"/></vertices><triangles><triangle v1="0" v2="1" v3="2"/></triangles></mesh></object></resources></model>`))
	c, _ := z.Create("Metadata/a.config")
	c.Write([]byte("FlashFit Bilanciata AD5M 0.4\nFlashFit Test PLA"))
	z.Close()
	f.Close()
	if Validate3MF(p, []string{"FlashFit Bilanciata AD5M 0.4", "FlashFit Test PLA"}, Recommendation{}) == nil {
		t.Fatal("missing critical settings accepted")
	}
}
func TestCorrupt3MFRejected(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.3mf")
	os.WriteFile(p, []byte("not-a-zip"), 0600)
	if Validate3MF(p, nil, Recommendation{}) == nil {
		t.Fatal("corrupt 3mf accepted")
	}
}
func TestFingerprintChangeBlocksImport(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	d := t.TempDir()
	machine, bp, bf := writeBaseProfiles(t, d)
	model := filepath.Join(d, "cube.stl")
	writeCubeSTL(t, model)
	a, _ := AnalyzeSTL(model)
	f, _ := os.OpenFile(model, os.O_APPEND|os.O_WRONLY, 0600)
	f.Write([]byte("x"))
	f.Close()
	script := filepath.Join(d, "orca-slicer")
	os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0700)
	_, e := BuildAndOpen(ImportRequest{Model: a, Filament: sampleFilament(), Quality: "balanced", SlicerExe: script, Machine: machine, BaseProcess: bp, BaseFilament: bf, OutputDir: filepath.Join(d, "out")})
	if e == nil || !strings.Contains(e.Error(), "cambiato") {
		t.Fatalf("fingerprint not blocked: %v", e)
	}
}

func Test3MFWrongCriticalValueRejected(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "wrong.3mf")
	r := Recommendation{CriticalValues: map[string]float64{"layer_height": 0.2, "outer_wall_speed": 55, "outer_acceleration": 1500, "max_volumetric_speed": 11.52, "nozzle_temperature": 215}}
	writeValid3MF(t, p, []string{"FlashFit Bilanciata AD5M 0.4", "FlashFit Test PLA"})
	if Validate3MF(p, []string{"FlashFit Bilanciata AD5M 0.4", "FlashFit Test PLA"}, r) == nil {
		t.Fatal("wrong MVS value accepted")
	}
}

func Test3MFPathTraversalRejected(t *testing.T) {
	p := filepath.Join(t.TempDir(), "traversal.3mf")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	w, _ := z.Create("3D/3dmodel.model")
	w.Write([]byte(`<?xml version="1.0"?><model><resources><object id="1"><mesh><vertices><vertex x="0" y="0" z="0"/><vertex x="1" y="0" z="0"/><vertex x="0" y="1" z="0"/></vertices><triangles><triangle v1="0" v2="1" v3="2"/></triangles></mesh></object></resources></model>`))
	bad, _ := z.Create("../outside.txt")
	bad.Write([]byte("x"))
	cfg, _ := z.Create("Metadata/a.config")
	cfg.Write([]byte("layer_height=0.2\nouter_wall_speed=55\nouter_wall_acceleration=1500\nfilament_max_volumetric_speed=16\nnozzle_temperature=215"))
	z.Close()
	f.Close()
	err = Validate3MF(p, nil, Recommendation{})
	if err == nil || !strings.Contains(err.Error(), "percorso") {
		t.Fatalf("path traversal accepted: %v", err)
	}
}

func Test3MFGeometryMismatchRejected(t *testing.T) {
	d := t.TempDir()
	model := filepath.Join(d, "cube.stl")
	writeCubeSTL(t, model)
	a, err := AnalyzeSTL(model)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(d, "scaled.3mf")
	writeValid3MF(t, p, []string{"FlashFit Bilanciata AD5M 0.4", "FlashFit Test PLA"})
	// The helper writes a 20 mm cube, so alter the expected dimensions to emulate a different original.
	a.Extents = [3]float64{40, 40, 40}
	if validate3MF(p, []string{"FlashFit Bilanciata AD5M 0.4", "FlashFit Test PLA"}, Recommendation{}, &a) == nil {
		t.Fatal("scaled geometry accepted")
	}
}
