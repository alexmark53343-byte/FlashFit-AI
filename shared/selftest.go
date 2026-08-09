package shared

import (
	"archive/zip"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
)

func RunSelfTest(root string) error {
	if err := os.MkdirAll(root, 0700); err != nil {
		return err
	}
	fils, err := LoadBuiltinFilaments()
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	if len(fils) < 20 {
		return fmt.Errorf("database incompleto: %d profili", len(fils))
	}
	model := filepath.Join(root, "selftest_cube.stl")
	if err = writeSelfTestCube(model); err != nil {
		return err
	}
	a, err := AnalyzeModel(model)
	if err != nil {
		return fmt.Errorf("analisi STL: %w", err)
	}
	if err = ValidateAnalysis(a); err != nil {
		return err
	}
	if math.Abs(a.Extents[0]-20) > 0.01 || a.TriangleCount != 12 || a.InputFormat != "STL" {
		return fmt.Errorf("geometria self-test STL alterata")
	}
	obj := filepath.Join(root, "selftest_cube.obj")
	if err = writeSelfTestOBJ(obj); err != nil {
		return err
	}
	oa, err := AnalyzeModel(obj)
	if err != nil || ValidateAnalysis(oa) != nil || oa.InputFormat != "OBJ" || oa.Sanitized || oa.TriangleCount != 12 {
		return fmt.Errorf("analisi OBJ self-test fallita: %v", err)
	}
	threeMF := filepath.Join(root, "selftest_input.3mf")
	if err = writeSelfTestGeometry3MF(threeMF); err != nil {
		return err
	}
	ma, err := AnalyzeModel(threeMF)
	if err != nil || ValidateAnalysis(ma) != nil || ma.InputFormat != "3MF" || !ma.Sanitized || ma.TriangleCount != 12 {
		return fmt.Errorf("analisi 3MF self-test fallita: %v", err)
	}
	production3MF := filepath.Join(root, "selftest_production.3mf")
	if err = writeSelfTestProduction3MF(production3MF); err != nil {
		return err
	}
	pa, err := AnalyzeModel(production3MF)
	if err != nil || ValidateAnalysis(pa) != nil || pa.InputFormat != "3MF" || pa.TriangleCount != 12 || math.Abs(pa.BoundsMin[0]-10) > 0.01 {
		return fmt.Errorf("analisi 3MF Production Extension self-test fallita: %v", err)
	}
	tested := 0
	for _, f := range fils {
		if ValidateFilament(f) != nil {
			continue
		}
		for _, q := range []string{"low", "balanced", "perfect"} {
			r, e := Recommend(a, f, q)
			if e != nil {
				return fmt.Errorf("profilo %s %s: %w", f.Product, q, e)
			}
			layer := r.CriticalValues["layer_height"]
			speed := r.CriticalValues["outer_wall_speed"]
			if speed*layer*0.42 > f.MaxVolumetricSpeed*0.73 {
				return fmt.Errorf("limite volumetrico non rispettato")
			}
			tested++
		}
	}
	if tested < 60 {
		return fmt.Errorf("copertura profili insufficiente: %d", tested)
	}
	machine := filepath.Join(root, "machine.json")
	bp := filepath.Join(root, "process.json")
	bf := filepath.Join(root, "filament.json")
	os.WriteFile(machine, []byte(`{"type":"machine","name":"Flashforge Adventurer 5M 0.4 Nozzle","nozzle_diameter":["0.4"]}`), 0600)
	os.WriteFile(bp, []byte(`{"type":"process","name":"Base @ Flashforge Adventurer 5M 0.4","compatible_printers":["Flashforge Adventurer 5M 0.4 Nozzle"],"machine_start_gcode":"UNCHANGED"}`), 0600)
	os.WriteFile(bf, []byte(`{"type":"filament","name":"Generic PLA","filament_type":["PLA"],"filament_start_gcode":"UNCHANGED"}`), 0600)
	var pla Filament
	for _, f := range fils {
		if strings.HasPrefix(strings.ToUpper(f.Material), "PLA") {
			pla = f
			break
		}
	}
	rec, err := Recommend(a, pla, "balanced")
	if err != nil {
		return err
	}
	pp, ff, _, err := PatchProfiles(bp, bf, rec, pla, filepath.Join(root, "profiles"))
	if err != nil {
		return fmt.Errorf("patch profili: %w", err)
	}
	pm, _ := readJSONMap(pp)
	fm, _ := readJSONMap(ff)
	if mapText(pm, "machine_start_gcode") != "UNCHANGED" || mapText(fm, "filament_start_gcode") != "UNCHANGED" {
		return fmt.Errorf("G-code base modificato")
	}
	project := filepath.Join(root, "selftest.3mf")
	markers := []string{safeProfileName("FlashFit " + rec.QualityLabel + " AD5M 0.4"), safeProfileName("FlashFit " + pla.Brand + " " + pla.Product)}
	if err = writeSelfTest3MF(project, markers, rec); err != nil {
		return err
	}
	if err = Validate3MF(project, markers, rec); err != nil {
		return fmt.Errorf("validazione 3MF: %w", err)
	}
	return nil
}

func writeSelfTestCube(path string) error {
	v := [][3]float32{{0, 0, 0}, {20, 0, 0}, {20, 20, 0}, {0, 20, 0}, {0, 0, 20}, {20, 0, 20}, {20, 20, 20}, {0, 20, 20}}
	faces := [][3]int{{0, 2, 1}, {0, 3, 2}, {4, 5, 6}, {4, 6, 7}, {0, 1, 5}, {0, 5, 4}, {1, 2, 6}, {1, 6, 5}, {2, 3, 7}, {2, 7, 6}, {3, 0, 4}, {3, 4, 7}}
	f, e := os.Create(path)
	if e != nil {
		return e
	}
	defer f.Close()
	if _, e = f.Write(make([]byte, 80)); e != nil {
		return e
	}
	if e = binary.Write(f, binary.LittleEndian, uint32(len(faces))); e != nil {
		return e
	}
	for _, fc := range faces {
		if e = binary.Write(f, binary.LittleEndian, [3]float32{}); e != nil {
			return e
		}
		for _, i := range fc {
			if e = binary.Write(f, binary.LittleEndian, v[i]); e != nil {
				return e
			}
		}
		if e = binary.Write(f, binary.LittleEndian, uint16(0)); e != nil {
			return e
		}
	}
	return nil
}
func selfTestCubeTriangles() []triangle {
	v := []vec3{{0, 0, 0}, {20, 0, 0}, {20, 20, 0}, {0, 20, 0}, {0, 0, 20}, {20, 0, 20}, {20, 20, 20}, {0, 20, 20}}
	faces := [][3]int{{0, 2, 1}, {0, 3, 2}, {4, 5, 6}, {4, 6, 7}, {0, 1, 5}, {0, 5, 4}, {1, 2, 6}, {1, 6, 5}, {2, 3, 7}, {2, 7, 6}, {3, 0, 4}, {3, 4, 7}}
	out := make([]triangle, 0, len(faces))
	for _, f := range faces {
		out = append(out, triangle{v[f[0]], v[f[1]], v[f[2]]})
	}
	return out
}
func writeSelfTestOBJ(path string) error {
	v := [][3]int{{0, 0, 0}, {20, 0, 0}, {20, 20, 0}, {0, 20, 0}, {0, 0, 20}, {20, 0, 20}, {20, 20, 20}, {0, 20, 20}}
	faces := [][3]int{{1, 3, 2}, {1, 4, 3}, {5, 6, 7}, {5, 7, 8}, {1, 2, 6}, {1, 6, 5}, {2, 3, 7}, {2, 7, 6}, {3, 4, 8}, {3, 8, 7}, {4, 1, 5}, {4, 5, 8}}
	var b strings.Builder
	b.WriteString("o selftest\n")
	for _, p := range v {
		fmt.Fprintf(&b, "v %d %d %d\n", p[0], p[1], p[2])
	}
	for _, f := range faces {
		fmt.Fprintf(&b, "f %d %d %d\n", f[0], f[1], f[2])
	}
	return os.WriteFile(path, []byte(b.String()), 0600)
}

func writeSelfTestProduction3MF(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	z := zip.NewWriter(f)
	write := func(name, body string) error {
		w, e := z.Create(name)
		if e != nil {
			return e
		}
		_, e = w.Write([]byte(body))
		return e
	}
	if err = write("_rels/.rels", `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Target="/3D/build.model" Id="rel0" Type="http://schemas.microsoft.com/3dmanufacturing/2013/01/3dmodel"/></Relationships>`); err != nil {
		return err
	}
	if err = write("3D/build.model", `<?xml version="1.0"?><model unit="millimeter" xmlns="http://schemas.microsoft.com/3dmanufacturing/core/2015/02" xmlns:p="http://schemas.microsoft.com/3dmanufacturing/production/2015/06"><resources/><build><item objectid="1" p:path="/3D/Objects/object_1.model" transform="1 0 0 0 1 0 0 0 1 10 0 0"/></build></model>`); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><model unit="millimeter" xmlns="http://schemas.microsoft.com/3dmanufacturing/core/2015/02"><resources><object id="1" type="model"><mesh><vertices>`)
	for _, t := range selfTestCubeTriangles() {
		for _, v := range []vec3{t.A, t.B, t.C} {
			fmt.Fprintf(&b, `<vertex x="%g" y="%g" z="%g"/>`, v.X, v.Y, v.Z)
		}
	}
	b.WriteString(`</vertices><triangles>`)
	for i := 0; i < len(selfTestCubeTriangles()); i++ {
		fmt.Fprintf(&b, `<triangle v1="%d" v2="%d" v3="%d"/>`, i*3, i*3+1, i*3+2)
	}
	b.WriteString(`</triangles></mesh></object></resources><build/></model>`)
	if err = write("3D/Objects/object_1.model", b.String()); err != nil {
		return err
	}
	if err = z.Close(); err != nil {
		return err
	}
	return f.Close()
}

func writeSelfTestGeometry3MF(path string) error {
	return writeGeometryOnly3MF(path, selfTestCubeTriangles())
}

func writeSelfTest3MF(path string, markers []string, rec Recommendation) error {
	f, e := os.Create(path)
	if e != nil {
		return e
	}
	z := zip.NewWriter(f)
	w, e := z.Create("3D/3dmodel.model")
	if e != nil {
		return e
	}
	_, e = w.Write([]byte(`<?xml version="1.0"?><model xmlns="http://schemas.microsoft.com/3dmanufacturing/core/2015/02"><resources><object id="1" type="model"><mesh><vertices><vertex x="0" y="0" z="0"/><vertex x="1" y="0" z="0"/><vertex x="0" y="1" z="0"/></vertices><triangles><triangle v1="0" v2="1" v3="2"/></triangles></mesh></object></resources><build><item objectid="1"/></build></model>`))
	if e != nil {
		return e
	}
	c, e := z.Create("Metadata/flashfit.config")
	if e != nil {
		return e
	}
	config := fmt.Sprintf("%s\nlayer_height=%s\nouter_wall_speed=%s\nouter_wall_acceleration=%s\nfilament_max_volumetric_speed=%s\nnozzle_temperature=%s", strings.Join(markers, "\n"), fmt2(rec.CriticalValues["layer_height"]), fmt0(rec.CriticalValues["outer_wall_speed"]), fmt0(rec.CriticalValues["outer_acceleration"]), fmt2(rec.CriticalValues["max_volumetric_speed"]), fmt0(rec.CriticalValues["nozzle_temperature"]))
	_, e = c.Write([]byte(config))
	if e != nil {
		return e
	}
	if e = z.Close(); e != nil {
		return e
	}
	return f.Close()
}
