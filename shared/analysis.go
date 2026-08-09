package shared

import (
	"bufio"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	MaxModelBytes = int64(512 * 1024 * 1024)
	MaxTriangles  = 1_000_000
)

type vec3 struct{ X, Y, Z float64 }
type triangle struct{ A, B, C vec3 }

func sub(a, b vec3) vec3 { return vec3{a.X - b.X, a.Y - b.Y, a.Z - b.Z} }
func cross(a, b vec3) vec3 {
	return vec3{a.Y*b.Z - a.Z*b.Y, a.Z*b.X - a.X*b.Z, a.X*b.Y - a.Y*b.X}
}
func dot(a, b vec3) float64 { return a.X*b.X + a.Y*b.Y + a.Z*b.Z }
func norm(a vec3) float64   { return math.Sqrt(dot(a, a)) }

func AnalyzeSTL(path string) (ModelAnalysis, error) {
	var out ModelAnalysis
	st, err := os.Stat(path)
	if err != nil {
		return out, fmt.Errorf("modello non leggibile: %w", err)
	}
	if st.Size() <= 84 || st.Size() > MaxModelBytes {
		return out, fmt.Errorf("dimensione STL non valida o superiore a 512 MB")
	}
	if strings.ToLower(filepath.Ext(path)) != ".stl" {
		return out, errors.New("la versione verde accetta soltanto file STL")
	}
	f, err := os.Open(path)
	if err != nil {
		return out, err
	}
	defer f.Close()
	first := make([]byte, 84)
	if _, err = io.ReadFull(f, first); err != nil {
		return out, err
	}
	count := int(binary.LittleEndian.Uint32(first[80:84]))
	binaryExpected := int64(84) + int64(count)*50
	if count > 0 && count <= MaxTriangles && binaryExpected == st.Size() {
		if _, err = f.Seek(84, io.SeekStart); err != nil {
			return out, err
		}
		out, err = analyzeBinarySTL(f, count)
	} else {
		if _, err = f.Seek(0, io.SeekStart); err != nil {
			return out, err
		}
		out, err = analyzeASCIISTL(f)
	}
	if err != nil {
		return ModelAnalysis{}, err
	}
	out.Filename = filepath.Base(path)
	out.SizeBytes = st.Size()
	out.StoredModelPath = path
	h, err := fileSHA256(path)
	if err != nil {
		return ModelAnalysis{}, err
	}
	out.SHA256 = h
	classifyAnalysis(&out)
	return out, nil
}

func analyzeBinarySTL(r io.Reader, count int) (ModelAnalysis, error) {
	if count <= 0 || count > MaxTriangles {
		return ModelAnalysis{}, fmt.Errorf("numero triangoli non sicuro: %d", count)
	}
	buf := make([]byte, 50)
	tris := make([]triangle, 0, min(count, 250000))
	for i := 0; i < count; i++ {
		if _, err := io.ReadFull(r, buf); err != nil {
			return ModelAnalysis{}, fmt.Errorf("STL troncato al triangolo %d", i+1)
		}
		t := triangle{
			A: vec3{float64(math.Float32frombits(binary.LittleEndian.Uint32(buf[12:16]))), float64(math.Float32frombits(binary.LittleEndian.Uint32(buf[16:20]))), float64(math.Float32frombits(binary.LittleEndian.Uint32(buf[20:24])))},
			B: vec3{float64(math.Float32frombits(binary.LittleEndian.Uint32(buf[24:28]))), float64(math.Float32frombits(binary.LittleEndian.Uint32(buf[28:32]))), float64(math.Float32frombits(binary.LittleEndian.Uint32(buf[32:36])))},
			C: vec3{float64(math.Float32frombits(binary.LittleEndian.Uint32(buf[36:40]))), float64(math.Float32frombits(binary.LittleEndian.Uint32(buf[40:44]))), float64(math.Float32frombits(binary.LittleEndian.Uint32(buf[44:48])))},
		}
		tris = append(tris, t)
	}
	return analyzeTriangles(tris)
}

func analyzeASCIISTL(r io.Reader) (ModelAnalysis, error) {
	s := bufio.NewScanner(r)
	buf := make([]byte, 64*1024)
	s.Buffer(buf, 4*1024*1024)
	verts := make([]vec3, 0, 3000)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if !strings.HasPrefix(strings.ToLower(line), "vertex ") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) != 4 {
			return ModelAnalysis{}, fmt.Errorf("riga vertex STL non valida")
		}
		x, e1 := strconv.ParseFloat(parts[1], 64)
		y, e2 := strconv.ParseFloat(parts[2], 64)
		z, e3 := strconv.ParseFloat(parts[3], 64)
		if e1 != nil || e2 != nil || e3 != nil {
			return ModelAnalysis{}, fmt.Errorf("coordinate STL non valide")
		}
		verts = append(verts, vec3{x, y, z})
		if len(verts)/3 > MaxTriangles {
			return ModelAnalysis{}, fmt.Errorf("STL oltre %d triangoli", MaxTriangles)
		}
	}
	if err := s.Err(); err != nil {
		return ModelAnalysis{}, err
	}
	if len(verts) < 3 || len(verts)%3 != 0 {
		return ModelAnalysis{}, fmt.Errorf("STL ASCII incompleto")
	}
	tris := make([]triangle, 0, len(verts)/3)
	for i := 0; i < len(verts); i += 3 {
		tris = append(tris, triangle{verts[i], verts[i+1], verts[i+2]})
	}
	return analyzeTriangles(tris)
}

type qv struct{ X, Y, Z int64 }
type edge struct{ A, B qv }

func quant(v vec3) qv {
	const q = 100000.0
	return qv{int64(math.Round(v.X * q)), int64(math.Round(v.Y * q)), int64(math.Round(v.Z * q))}
}
func lessQ(a, b qv) bool {
	if a.X != b.X {
		return a.X < b.X
	}
	if a.Y != b.Y {
		return a.Y < b.Y
	}
	return a.Z < b.Z
}
func edgeKey(a, b vec3) edge {
	qa, qb := quant(a), quant(b)
	if lessQ(qb, qa) {
		qa, qb = qb, qa
	}
	return edge{qa, qb}
}

func analyzeTriangles(tris []triangle) (ModelAnalysis, error) {
	deadline := time.Now().Add(2 * time.Minute)
	if len(tris) == 0 {
		return ModelAnalysis{}, errors.New("STL senza triangoli")
	}
	minv := vec3{math.Inf(1), math.Inf(1), math.Inf(1)}
	maxv := vec3{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	edges := make(map[edge]uint8, len(tris)*2)
	area, signedVol := 0.0, 0.0
	degenerate, downward := 0, 0
	minZVertices := 0
	for triIndex, t := range tris {
		if triIndex%8192 == 0 && time.Now().After(deadline) {
			return ModelAnalysis{}, errors.New("analisi topologica oltre 2 minuti: mesh troppo complessa")
		}
		vs := []vec3{t.A, t.B, t.C}
		for _, v := range vs {
			if math.IsNaN(v.X) || math.IsNaN(v.Y) || math.IsNaN(v.Z) || math.IsInf(v.X, 0) || math.IsInf(v.Y, 0) || math.IsInf(v.Z, 0) {
				return ModelAnalysis{}, errors.New("coordinate non finite nello STL")
			}
			if math.Abs(v.X) > 100000 || math.Abs(v.Y) > 100000 || math.Abs(v.Z) > 100000 {
				return ModelAnalysis{}, errors.New("coordinate STL fuori scala plausibile")
			}
			minv.X = math.Min(minv.X, v.X)
			minv.Y = math.Min(minv.Y, v.Y)
			minv.Z = math.Min(minv.Z, v.Z)
			maxv.X = math.Max(maxv.X, v.X)
			maxv.Y = math.Max(maxv.Y, v.Y)
			maxv.Z = math.Max(maxv.Z, v.Z)
		}
		n := cross(sub(t.B, t.A), sub(t.C, t.A))
		nlen := norm(n)
		if nlen < 1e-10 {
			degenerate++
			continue
		}
		triArea := nlen / 2
		area += triArea
		if n.Z/nlen < -0.70710678 {
			downward++
		}
		signedVol += dot(t.A, cross(t.B, t.C)) / 6.0
		for _, e := range []edge{edgeKey(t.A, t.B), edgeKey(t.B, t.C), edgeKey(t.C, t.A)} {
			if edges[e] < 3 {
				edges[e]++
			}
		}
	}
	if area <= 0 {
		return ModelAnalysis{}, errors.New("superficie STL nulla")
	}
	openEdges, nonManifold := 0, 0
	for _, c := range edges {
		if c == 1 {
			openEdges++
		}
		if c > 2 {
			nonManifold++
		}
	}
	ext := [3]float64{maxv.X - minv.X, maxv.Y - minv.Y, maxv.Z - minv.Z}
	if ext[0] <= 0 || ext[1] <= 0 || ext[2] <= 0 {
		return ModelAnalysis{}, errors.New("dimensioni STL nulle")
	}
	// Stima contatto piano: vertici entro 0,05 mm dal minimo Z.
	for triIndex, t := range tris {
		if triIndex%8192 == 0 && time.Now().After(deadline) {
			return ModelAnalysis{}, errors.New("analisi del piano oltre 2 minuti: mesh troppo complessa")
		}
		for _, v := range []vec3{t.A, t.B, t.C} {
			if math.Abs(v.Z-minv.Z) <= 0.05 {
				minZVertices++
			}
		}
	}
	contact := float64(minZVertices) / float64(len(tris)*3)
	return ModelAnalysis{
		TriangleCount: len(tris), BoundsMin: [3]float64{minv.X, minv.Y, minv.Z}, BoundsMax: [3]float64{maxv.X, maxv.Y, maxv.Z}, Extents: ext,
		SurfaceArea: area, Volume: math.Abs(signedVol), Watertight: openEdges == 0 && nonManifold == 0 && degenerate == 0,
		DegenerateFaces: degenerate, OverhangRatio: float64(downward) / float64(len(tris)), BedContactRatio: contact,
	}, nil
}

func classifyAnalysis(a *ModelAnalysis) {
	x, y, z := a.Extents[0], a.Extents[1], a.Extents[2]
	foot := x * y
	a.ThinOrTall = z > 100 && (math.Min(x, y) < 25 || foot < 1200)
	a.BrimSuggested = a.ThinOrTall || a.BedContactRatio < 0.015
	a.SupportSuggested = a.OverhangRatio > 0.045
	switch {
	case z < 35 && math.Max(x, y) < 80 && a.TriangleCount > 50000:
		a.Category = "Miniatura dettagliata"
	case a.ThinOrTall:
		a.Category = "Forma alta o sottile"
	case a.OverhangRatio > 0.10:
		a.Category = "Geometria con molti sbalzi"
	case math.Max(x, math.Max(y, z)) > 160:
		a.Category = "Oggetto grande"
	case a.TriangleCount > 150000:
		a.Category = "Superficie complessa"
	default:
		a.Category = "Oggetto tecnico/decorativo"
	}
	if !a.Watertight {
		a.Warnings = append(a.Warnings, "Mesh non chiusa o non manifold: importazione automatica bloccata.")
	}
	if a.DegenerateFaces > 0 {
		a.Warnings = append(a.Warnings, fmt.Sprintf("Rilevate %d facce degeneri: il modello non viene modificato e l'importazione è bloccata.", a.DegenerateFaces))
	}
	if x > 220 || y > 220 || z > 220 {
		a.Warnings = append(a.Warnings, "Il modello, nell'orientamento attuale, supera il volume 220×220×220 mm.")
	}
	if a.SupportSuggested {
		a.Warnings = append(a.Warnings, "Sono presenti sbalzi marcati: controllare i supporti nell'anteprima layer.")
	}
	if a.BrimSuggested {
		a.Warnings = append(a.Warnings, "Impronta ridotta o modello alto: brim prudente consigliato.")
	}
}

func ValidateAnalysis(a ModelAnalysis) error {
	if !a.Watertight {
		return errors.New("mesh non chiusa/non manifold: correggila prima di importare")
	}
	if a.DegenerateFaces != 0 {
		return errors.New("mesh con facce degeneri: FlashFit non la ripara automaticamente")
	}
	if a.Extents[0] > 220 || a.Extents[1] > 220 || a.Extents[2] > 220 {
		return errors.New("modello fuori dal volume AD5M 220×220×220 mm nell'orientamento attuale")
	}
	if a.TriangleCount <= 0 || a.TriangleCount > MaxTriangles {
		return errors.New("numero triangoli fuori limite")
	}
	return nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func StableFilamentSort(fs []Filament) {
	sort.SliceStable(fs, func(i, j int) bool {
		a := strings.ToLower(fs[i].Brand + " " + fs[i].Product + " " + fs[i].Variant)
		b := strings.ToLower(fs[j].Brand + " " + fs[j].Product + " " + fs[j].Variant)
		return a < b
	})
}
