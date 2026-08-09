package shared

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

var cubeVerts = [][3]float64{{0, 0, 0}, {20, 0, 0}, {20, 20, 0}, {0, 20, 0}, {0, 0, 20}, {20, 0, 20}, {20, 20, 20}, {0, 20, 20}}
var cubeFaces = [][3]int{{0, 2, 1}, {0, 3, 2}, {4, 5, 6}, {4, 6, 7}, {0, 1, 5}, {0, 5, 4}, {1, 2, 6}, {1, 6, 5}, {2, 3, 7}, {2, 7, 6}, {3, 0, 4}, {3, 4, 7}}

func TestAnalyzeOBJAndSanitize(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "cube.obj")
	var b strings.Builder
	b.WriteString("o cube\n")
	for _, v := range cubeVerts {
		fmt.Fprintf(&b, "v %g %g %g\n", v[0], v[1], v[2])
	}
	for _, f := range cubeFaces {
		fmt.Fprintf(&b, "f %d %d %d\n", f[0]+1, f[1]+1, f[2]+1)
	}
	if err := os.WriteFile(p, []byte(b.String()), 0600); err != nil {
		t.Fatal(err)
	}
	a, err := AnalyzeModel(p)
	if err != nil {
		t.Fatal(err)
	}
	if err = ValidateAnalysis(a); err != nil {
		t.Fatal(err)
	}
	if a.InputFormat != "OBJ" || a.Sanitized || a.TriangleCount != 12 || a.ObjectCount != 1 {
		t.Fatalf("bad analysis: %+v", a)
	}
	if a.StoredModelPath == p {
		t.Fatal("OBJ was not staged into the stable cache")
	}
	orig, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	staged, err := os.ReadFile(a.StoredModelPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(orig, staged) {
		t.Fatal("staged OBJ is not an exact byte-for-byte copy")
	}
}

func TestAnalyze3MFPreservesTransformsAndStripsMetadata(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "cube.3mf")
	if err := writeTest3MFModel(p, "1 0 0 0 1 0 0 0 1 10 20 30", true); err != nil {
		t.Fatal(err)
	}
	a, err := AnalyzeModel(p)
	if err != nil {
		t.Fatal(err)
	}
	if err = ValidateAnalysis(a); err != nil {
		t.Fatal(err)
	}
	if a.InputFormat != "3MF" || !a.Sanitized || a.TriangleCount != 12 {
		t.Fatalf("bad analysis: %+v", a)
	}
	if !closeExtent(a.BoundsMin[0], 10) || !closeExtent(a.BoundsMin[1], 20) || !closeExtent(a.BoundsMin[2], 30) {
		t.Fatalf("transform lost: min=%v", a.BoundsMin)
	}
	zr, err := zip.OpenReader(a.StoredModelPath)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if strings.Contains(strings.ToLower(f.Name), "evil") || strings.Contains(strings.ToLower(f.Name), "config") {
			t.Fatalf("hidden metadata copied: %s", f.Name)
		}
		r, _ := f.Open()
		data, _ := io.ReadAll(r)
		r.Close()
		if strings.Contains(string(data), "speed=9999") {
			t.Fatal("hidden setting survived sanitization")
		}
	}
}

func TestAnalyze3MFComponentsPreserveComposedTransform(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "components.3mf")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	w, _ := z.Create("3D/3dmodel.model")
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><model unit="millimeter" xmlns="http://schemas.microsoft.com/3dmanufacturing/core/2015/02"><resources><object id="1" type="model"><mesh><vertices>`)
	for _, v := range cubeVerts {
		fmt.Fprintf(&b, `<vertex x="%g" y="%g" z="%g"/>`, v[0], v[1], v[2])
	}
	b.WriteString(`</vertices><triangles>`)
	for _, fc := range cubeFaces {
		fmt.Fprintf(&b, `<triangle v1="%d" v2="%d" v3="%d"/>`, fc[0], fc[1], fc[2])
	}
	b.WriteString(`</triangles></mesh></object><object id="2" type="model"><components><component objectid="1" transform="1 0 0 0 1 0 0 0 1 5 0 0"/></components></object></resources><build><item objectid="2" transform="1 0 0 0 1 0 0 0 1 10 0 0"/></build></model>`)
	w.Write([]byte(b.String()))
	z.Close()
	f.Close()
	a, err := AnalyzeModel(p)
	if err != nil {
		t.Fatal(err)
	}
	if !closeExtent(a.BoundsMin[0], 15) || !closeExtent(a.BoundsMax[0], 35) {
		t.Fatalf("composed transform wrong: %v %v", a.BoundsMin, a.BoundsMax)
	}
}

func TestAnalyzeUnsupportedFormat(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.step")
	os.WriteFile(p, []byte("dummy"), 0600)
	if _, err := AnalyzeModel(p); err == nil {
		t.Fatal("unsupported format accepted")
	}
}

func writeTest3MFModel(path, transform string, evil bool) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	z := zip.NewWriter(f)
	w, err := z.Create("3D/3dmodel.model")
	if err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><model unit="millimeter" xmlns="http://schemas.microsoft.com/3dmanufacturing/core/2015/02"><resources><object id="1" type="model"><mesh><vertices>`)
	for _, v := range cubeVerts {
		fmt.Fprintf(&b, `<vertex x="%g" y="%g" z="%g"/>`, v[0], v[1], v[2])
	}
	b.WriteString(`</vertices><triangles>`)
	for _, fc := range cubeFaces {
		fmt.Fprintf(&b, `<triangle v1="%d" v2="%d" v3="%d"/>`, fc[0], fc[1], fc[2])
	}
	fmt.Fprintf(&b, `</triangles></mesh></object></resources><build><item objectid="1" transform="%s"/></build></model>`, transform)
	if _, err = w.Write([]byte(b.String())); err != nil {
		return err
	}
	if evil {
		e, _ := z.Create("Metadata/evil.config")
		e.Write([]byte("speed=9999"))
	}
	if err = z.Close(); err != nil {
		return err
	}
	return f.Close()
}

func TestRunSlicerReturnsWhenProjectStableEvenIfGUIStaysOpen(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell test")
	}
	d := t.TempDir()
	candidate := filepath.Join(d, "candidate.3mf")
	script := filepath.Join(d, "fake-slicer")
	body := "#!/bin/sh\nprintf 'this-is-a-stable-project-file-with-enough-bytes..............................................................................................................................................................................' > '" + candidate + "'\nsleep 10\n"
	if err := os.WriteFile(script, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	cmd := exec.CommandContext(ctx, script)
	start := time.Now()
	err := runSlicerUntilProject(ctx, cmd, candidate)
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("waited for GUI exit: %v", elapsed)
	}
}

func TestRunSlicerCanBeCancelled(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell test")
	}
	d := t.TempDir()
	script := filepath.Join(d, "fake-slicer")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 10\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, script)
	go func() { time.Sleep(150 * time.Millisecond); cancel() }()
	start := time.Now()
	err := runSlicerUntilProject(ctx, cmd, filepath.Join(d, "missing.3mf"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel not propagated: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("cancel too slow: %v", elapsed)
	}
}

func TestSanitized3MFSourceChangeBlocksImport(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("linux fake slicer test")
	}
	d := t.TempDir()
	source := filepath.Join(d, "source.3mf")
	if err := writeTest3MFModel(source, "", false); err != nil {
		t.Fatal(err)
	}
	a, err := AnalyzeModel(source)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(source, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.Write([]byte("changed"))
	_ = f.Close()
	machine, bp, bf := writeBaseProfiles(t, d)
	script := filepath.Join(d, "orca-slicer")
	if err = os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	_, err = BuildAndOpen(ImportRequest{Model: a, Filament: sampleFilament(), Quality: "balanced", SlicerExe: script, Machine: machine, BaseProcess: bp, BaseFilament: bf, OutputDir: filepath.Join(d, "out")})
	if err == nil || !strings.Contains(err.Error(), "originale è cambiato") {
		t.Fatalf("changed source not blocked: %v", err)
	}
}

func productionCubeModelXML(unit string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<?xml version="1.0"?><model unit="%s" xmlns="http://schemas.microsoft.com/3dmanufacturing/core/2015/02"><resources><object id="1" type="model"><mesh><vertices>`, unit)
	for _, v := range cubeVerts {
		fmt.Fprintf(&b, `<vertex x="%g" y="%g" z="%g"/>`, v[0], v[1], v[2])
	}
	b.WriteString(`</vertices><triangles>`)
	for _, fc := range cubeFaces {
		fmt.Fprintf(&b, `<triangle v1="%d" v2="%d" v3="%d"/>`, fc[0], fc[1], fc[2])
	}
	b.WriteString(`</triangles></mesh></object></resources><build/></model>`)
	return b.String()
}

func writeProduction3MF(t *testing.T, rootXML string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "production.3mf")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	entries := map[string]string{
		"_rels/.rels":               `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Target="/3D/build.model" Id="rel0" Type="http://schemas.microsoft.com/3dmanufacturing/2013/01/3dmodel"/></Relationships>`,
		"3D/build.model":            rootXML,
		"3D/Objects/object_1.model": productionCubeModelXML("millimeter"),
	}
	for name, body := range entries {
		w, e := z.Create(name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = w.Write([]byte(body)); e != nil {
			t.Fatal(e)
		}
	}
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAnalyze3MFProductionBuildItemPath(t *testing.T) {
	root := `<?xml version="1.0"?><model unit="millimeter" xmlns="http://schemas.microsoft.com/3dmanufacturing/core/2015/02" xmlns:p="http://schemas.microsoft.com/3dmanufacturing/production/2015/06"><resources/><build><item objectid="1" p:path="/3D/Objects/object_1.model" transform="1 0 0 0 1 0 0 0 1 10 20 30"/></build></model>`
	p := writeProduction3MF(t, root)
	a, err := AnalyzeModel(p)
	if err != nil {
		t.Fatal(err)
	}
	if a.TriangleCount != 12 || a.ObjectCount != 1 {
		t.Fatalf("unexpected analysis: %+v", a)
	}
	if !closeExtent(a.BoundsMin[0], 10) || !closeExtent(a.BoundsMin[1], 20) || !closeExtent(a.BoundsMin[2], 30) {
		t.Fatalf("production item transform lost: min=%v", a.BoundsMin)
	}
	if !strings.Contains(strings.Join(a.Warnings, " "), "Production Extension") {
		t.Fatalf("missing production warning: %v", a.Warnings)
	}
}

func TestAnalyze3MFProductionComponentPath(t *testing.T) {
	root := `<?xml version="1.0"?><model unit="millimeter" xmlns="http://schemas.microsoft.com/3dmanufacturing/core/2015/02" xmlns:p="http://schemas.microsoft.com/3dmanufacturing/production/2015/06"><resources><object id="7" type="model"><components><component objectid="1" p:path="/3D/Objects/object_1.model" transform="1 0 0 0 1 0 0 0 1 5 0 0"/></components></object></resources><build><item objectid="7" transform="1 0 0 0 1 0 0 0 1 10 0 0"/></build></model>`
	p := writeProduction3MF(t, root)
	a, err := AnalyzeModel(p)
	if err != nil {
		t.Fatal(err)
	}
	if !closeExtent(a.BoundsMin[0], 15) || !closeExtent(a.BoundsMax[0], 35) {
		t.Fatalf("production component transform lost: %v %v", a.BoundsMin, a.BoundsMax)
	}
}
