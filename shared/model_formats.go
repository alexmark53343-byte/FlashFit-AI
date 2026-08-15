package shared

import (
	"archive/zip"
	"bufio"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	pathpkg "path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	modelAnalysisTimeout = 2 * time.Minute
	// The same mesh must be judged the same way whatever container it arrives
	// in. This used to sit at half of MaxTriangles, so a 750k-triangle car was
	// accepted as an STL and refused as a 3MF — the geometry being identical.
	//
	// The limit exists to bound memory, and the numbers support the higher one:
	// a triangle is 72 bytes, so a million of them is ~72 MB of mesh plus the
	// vertex-dedup index, and all of it is built inside the analysis worker
	// process rather than the one drawing the window.
	MaxSanitizedTriangles = MaxTriangles
	max3MFModelXML        = 192 * 1024 * 1024
)

type analysisDeadline struct{ end time.Time }

func newAnalysisDeadline() analysisDeadline {
	return analysisDeadline{end: time.Now().Add(modelAnalysisTimeout)}
}
func (d analysisDeadline) check() error {
	if time.Now().After(d.end) {
		return errors.New("analisi interrotta dopo 2 minuti: modello troppo complesso o danneggiato")
	}
	return nil
}

// AnalyzeModel accepts STL, OBJ and 3MF. OBJ/3MF are converted into a geometry-only
// temporary 3MF so hidden slicer settings cannot override FlashFit profiles.
func AnalyzeModel(path string) (ModelAnalysis, error) {
	path = filepath.Clean(path)
	st, err := os.Stat(path)
	if err != nil {
		return ModelAnalysis{}, fmt.Errorf("modello non leggibile: %w", err)
	}
	if st.IsDir() || st.Size() <= 0 || st.Size() > MaxModelBytes {
		return ModelAnalysis{}, errors.New("file modello vuoto, non valido o superiore a 512 MB")
	}
	ext := strings.ToLower(filepath.Ext(path))
	sourceHash, err := fileSHA256(path)
	if err != nil {
		return ModelAnalysis{}, err
	}
	switch ext {
	case ".stl":
		a, err := AnalyzeSTL(path)
		if err != nil {
			return ModelAnalysis{}, err
		}
		staged, stagedHash, err := stageExactModel(path, sourceHash)
		if err != nil {
			return ModelAnalysis{}, err
		}
		a.InputFormat = "STL"
		a.SourcePath = path
		a.SourceSHA256 = sourceHash
		a.StoredModelPath = staged
		a.SHA256 = stagedHash
		a.ObjectCount = 1
		a.Warnings = append(a.Warnings, "STL copiato byte-per-byte in una cache stabile prima dell'importazione; il file originale non viene modificato.")
		return a, nil
	case ".obj":
		tris, objects, warnings, err := readOBJGeometry(path, newAnalysisDeadline())
		if err != nil {
			return ModelAnalysis{}, err
		}
		a, err := analyzeTriangles(tris)
		if err != nil {
			return ModelAnalysis{}, err
		}
		a.Filename = filepath.Base(path)
		a.InputFormat = "OBJ"
		a.SourcePath = path
		a.SourceSHA256 = sourceHash
		staged, stagedHash, err := stageExactModel(path, sourceHash)
		if err != nil {
			return ModelAnalysis{}, err
		}
		a.StoredModelPath = staged
		a.SHA256 = stagedHash
		a.SizeBytes = st.Size()
		a.ObjectCount = objects
		classifyAnalysis(&a)
		a.Warnings = append(a.Warnings, "OBJ copiato byte-per-byte in una cache stabile; unità interpretata in millimetri.")
		a.Warnings = append(a.Warnings, warnings...)
		return a, nil
	case ".3mf":
		tris, objects, warnings, err := read3MFGeometry(path, newAnalysisDeadline())
		if err != nil {
			return ModelAnalysis{}, err
		}
		return analyzeAndSanitize(path, sourceHash, "3MF", tris, objects, warnings)
	default:
		return ModelAnalysis{}, errors.New("formato non supportato: usa STL, OBJ oppure 3MF")
	}
}

func analyzeAndSanitize(sourcePath, sourceHash, format string, tris []triangle, objectCount int, warnings []string) (ModelAnalysis, error) {
	if len(tris) == 0 {
		return ModelAnalysis{}, errors.New("modello senza triangoli")
	}
	a, err := analyzeTriangles(tris)
	if err != nil {
		return ModelAnalysis{}, err
	}
	cache, err := modelCacheDir()
	if err != nil {
		return ModelAnalysis{}, err
	}
	cleanupModelCache(cache)
	f, err := os.CreateTemp(cache, "flashfit-geometry-*.3mf")
	if err != nil {
		return ModelAnalysis{}, fmt.Errorf("cache geometria non creabile: %w", err)
	}
	tmp := f.Name()
	if err = f.Close(); err != nil {
		os.Remove(tmp)
		return ModelAnalysis{}, err
	}
	if err = writeGeometryOnly3MF(tmp, tris); err != nil {
		os.Remove(tmp)
		return ModelAnalysis{}, fmt.Errorf("3MF geometrico temporaneo non creabile: %w", err)
	}
	storedHash, err := fileSHA256(tmp)
	if err != nil {
		os.Remove(tmp)
		return ModelAnalysis{}, err
	}
	st, err := os.Stat(sourcePath)
	if err != nil {
		os.Remove(tmp)
		return ModelAnalysis{}, err
	}
	a.Filename = filepath.Base(sourcePath)
	a.InputFormat = format
	a.SourcePath = sourcePath
	a.SourceSHA256 = sourceHash
	a.StoredModelPath = tmp
	a.SHA256 = storedHash
	a.SizeBytes = st.Size()
	a.Sanitized = true
	if objectCount < 1 {
		objectCount = 1
	}
	a.ObjectCount = objectCount
	classifyAnalysis(&a)
	if format == "3MF" {
		a.Warnings = append(a.Warnings, "Impostazioni e metadati del 3MF originale rimossi: viene conservata soltanto la geometria finale e le trasformazioni.")
	}
	a.Warnings = append(a.Warnings, warnings...)
	return a, nil
}

func stageExactModel(sourcePath, sourceHash string) (string, string, error) {
	if len(sourceHash) < 16 {
		return "", "", errors.New("hash del modello non valido")
	}
	cache, err := modelCacheDir()
	if err != nil {
		return "", "", err
	}
	cleanupModelCache(cache)
	ext := strings.ToLower(filepath.Ext(sourcePath))
	if ext != ".stl" && ext != ".obj" {
		return "", "", errors.New("formato non copiabile nella cache")
	}
	dst := filepath.Join(cache, "model-"+sourceHash[:16]+ext)
	if st, statErr := os.Stat(dst); statErr == nil && !st.IsDir() {
		if h, hashErr := fileSHA256(dst); hashErr == nil && h == sourceHash {
			return dst, h, nil
		}
		_ = os.Remove(dst)
	}
	in, err := os.Open(sourcePath)
	if err != nil {
		return "", "", fmt.Errorf("modello non apribile per la cache: %w", err)
	}
	defer in.Close()
	tmp, err := os.CreateTemp(cache, "model-copy-*.tmp")
	if err != nil {
		return "", "", fmt.Errorf("cache modello non creabile: %w", err)
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		_ = tmp.Close()
		if !ok {
			_ = os.Remove(tmpName)
		}
	}()
	written, err := io.Copy(tmp, io.LimitReader(in, MaxModelBytes+1))
	if err != nil {
		return "", "", fmt.Errorf("copia modello in cache fallita: %w", err)
	}
	if written <= 0 || written > MaxModelBytes {
		return "", "", errors.New("dimensione modello non valida durante la copia")
	}
	if err = tmp.Sync(); err != nil {
		return "", "", fmt.Errorf("cache modello non sincronizzata: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return "", "", fmt.Errorf("cache modello non chiudibile: %w", err)
	}
	copiedHash, err := fileSHA256(tmpName)
	if err != nil || copiedHash != sourceHash {
		return "", "", errors.New("la copia byte-per-byte del modello non supera il controllo SHA-256")
	}
	if err = os.Rename(tmpName, dst); err != nil {
		// Un altro processo può avere creato la stessa cache nel frattempo.
		if h, hashErr := fileSHA256(dst); hashErr == nil && h == sourceHash {
			ok = true
			_ = os.Remove(tmpName)
			return dst, h, nil
		}
		return "", "", fmt.Errorf("cache modello non finalizzabile: %w", err)
	}
	ok = true
	return dst, copiedHash, nil
}

func modelCacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil || strings.TrimSpace(base) == "" {
		base = os.TempDir()
	}
	d := filepath.Join(base, "FlashFitAI", "model-cache")
	if err := os.MkdirAll(d, 0700); err != nil {
		return "", err
	}
	return d, nil
}

func cleanupModelCache(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	for _, e := range entries {
		name := strings.ToLower(e.Name())
		if e.IsDir() || (!strings.HasPrefix(name, "flashfit-geometry-") && !strings.HasPrefix(name, "model-")) {
			continue
		}
		if st, err := e.Info(); err == nil && st.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

func readOBJGeometry(path string, deadline analysisDeadline) ([]triangle, int, []string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, nil, err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 64*1024), 16*1024*1024)
	vertices := make([]vec3, 0, 10000)
	tris := make([]triangle, 0, 20000)
	objects := 0
	polygons := 0
	lineNo := 0
	for s.Scan() {
		lineNo++
		if lineNo%8192 == 0 {
			if err := deadline.check(); err != nil {
				return nil, 0, nil, err
			}
		}
		line := strings.TrimSpace(s.Text())
		if cut := strings.Index(line, "#"); cut >= 0 {
			line = strings.TrimSpace(line[:cut])
		}
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch strings.ToLower(fields[0]) {
		case "o", "g":
			objects++
		case "v":
			if len(fields) < 4 {
				return nil, 0, nil, fmt.Errorf("OBJ: vertice non valido alla riga %d", lineNo)
			}
			x, e1 := strconv.ParseFloat(fields[1], 64)
			y, e2 := strconv.ParseFloat(fields[2], 64)
			z, e3 := strconv.ParseFloat(fields[3], 64)
			if e1 != nil || e2 != nil || e3 != nil || !finitePlausible(x) || !finitePlausible(y) || !finitePlausible(z) {
				return nil, 0, nil, fmt.Errorf("OBJ: coordinate non valide alla riga %d", lineNo)
			}
			vertices = append(vertices, vec3{x, y, z})
		case "f":
			if len(fields) < 4 {
				return nil, 0, nil, fmt.Errorf("OBJ: faccia con meno di 3 vertici alla riga %d", lineNo)
			}
			idx := make([]int, 0, len(fields)-1)
			for _, token := range fields[1:] {
				first := strings.SplitN(token, "/", 2)[0]
				v, e := strconv.Atoi(first)
				if e != nil || v == 0 {
					return nil, 0, nil, fmt.Errorf("OBJ: indice faccia non valido alla riga %d", lineNo)
				}
				if v < 0 {
					v = len(vertices) + v
				} else {
					v--
				}
				if v < 0 || v >= len(vertices) {
					return nil, 0, nil, fmt.Errorf("OBJ: indice fuori intervallo alla riga %d", lineNo)
				}
				idx = append(idx, v)
			}
			if len(idx) > 3 {
				polygons++
			}
			for i := 1; i+1 < len(idx); i++ {
				tris = append(tris, triangle{vertices[idx[0]], vertices[idx[i]], vertices[idx[i+1]]})
				if len(tris) > MaxTriangles {
					return nil, 0, nil, fmt.Errorf("OBJ oltre %d triangoli", MaxTriangles)
				}
			}
		}
	}
	if err := s.Err(); err != nil {
		return nil, 0, nil, fmt.Errorf("OBJ non leggibile: %w", err)
	}
	if len(vertices) == 0 || len(tris) == 0 {
		return nil, 0, nil, errors.New("OBJ senza vertici o facce")
	}
	if objects == 0 {
		objects = 1
	}
	var warnings []string
	if polygons > 0 {
		warnings = append(warnings, fmt.Sprintf("%d facce OBJ con più di 3 vertici triangolate senza spostare i vertici.", polygons))
	}
	return tris, objects, warnings, nil
}

func finitePlausible(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && math.Abs(v) <= 100000
}

type model3MF struct {
	Unit      string `xml:"unit,attr"`
	Resources struct {
		Objects []object3MF `xml:"object"`
	} `xml:"resources"`
	Build struct {
		Items []item3MF `xml:"item"`
	} `xml:"build"`
}
type object3MF struct {
	ID         int     `xml:"id,attr"`
	Type       string  `xml:"type,attr"`
	Mesh       mesh3MF `xml:"mesh"`
	Components struct {
		Items []component3MF `xml:"component"`
	} `xml:"components"`
}
type mesh3MF struct {
	Vertices  []vertex3MF   `xml:"vertices>vertex"`
	Triangles []triangle3MF `xml:"triangles>triangle"`
}
type vertex3MF struct {
	X, Y, Z float64 `xml:"-"`
}

func (v *vertex3MF) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	for _, a := range start.Attr {
		n, err := strconv.ParseFloat(a.Value, 64)
		if err != nil {
			return err
		}
		switch strings.ToLower(a.Name.Local) {
		case "x":
			v.X = n
		case "y":
			v.Y = n
		case "z":
			v.Z = n
		}
	}
	return d.Skip()
}

type triangle3MF struct{ V1, V2, V3 int }

func (t *triangle3MF) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	for _, a := range start.Attr {
		n, err := strconv.Atoi(a.Value)
		if err != nil {
			return err
		}
		switch strings.ToLower(a.Name.Local) {
		case "v1":
			t.V1 = n
		case "v2":
			t.V2 = n
		case "v3":
			t.V3 = n
		}
	}
	return d.Skip()
}

type component3MF struct {
	ObjectID  int    `xml:"objectid,attr"`
	Transform string `xml:"transform,attr"`
	Path      string `xml:"path,attr"`
}
type item3MF struct {
	ObjectID  int    `xml:"objectid,attr"`
	Transform string `xml:"transform,attr"`
	Path      string `xml:"path,attr"`
}

type mat4 [16]float64

func identity4() mat4 { return mat4{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1} }
func parse3MFTransform(s string) (mat4, error) {
	if strings.TrimSpace(s) == "" {
		return identity4(), nil
	}
	f := strings.Fields(s)
	if len(f) != 12 {
		return mat4{}, errors.New("trasformazione 3MF non valida")
	}
	v := make([]float64, 12)
	for i := range f {
		n, e := strconv.ParseFloat(f[i], 64)
		if e != nil || !finitePlausible(n) {
			return mat4{}, errors.New("trasformazione 3MF non numerica")
		}
		v[i] = n
	}
	// 3MF serializes a row-vector 3x4 matrix. Convert it to a conventional
	// column-vector matrix used by apply4/mul4.
	return mat4{v[0], v[3], v[6], v[9], v[1], v[4], v[7], v[10], v[2], v[5], v[8], v[11], 0, 0, 0, 1}, nil
}
func mul4(a, b mat4) mat4 {
	var r mat4
	for row := 0; row < 4; row++ {
		for col := 0; col < 4; col++ {
			for k := 0; k < 4; k++ {
				r[row*4+col] += a[row*4+k] * b[k*4+col]
			}
		}
	}
	return r
}
func apply4(m mat4, v vec3) vec3 {
	return vec3{m[0]*v.X + m[1]*v.Y + m[2]*v.Z + m[3], m[4]*v.X + m[5]*v.Y + m[6]*v.Z + m[7], m[8]*v.X + m[9]*v.Y + m[10]*v.Z + m[11]}
}
func det3(m mat4) float64 {
	return m[0]*(m[5]*m[10]-m[6]*m[9]) - m[1]*(m[4]*m[10]-m[6]*m[8]) + m[2]*(m[4]*m[9]-m[5]*m[8])
}

type relationship3MF struct {
	Target string `xml:"Target,attr"`
	Type   string `xml:"Type,attr"`
}

type relationships3MF struct {
	Items []relationship3MF `xml:"Relationship"`
}

type modelPart3MF struct {
	Path    string
	Doc     model3MF
	Objects map[int]object3MF
	Factor  float64
}

func scaleMatrix4(f float64) mat4 {
	m := identity4()
	m[0], m[5], m[10] = f, f, f
	return m
}

func normalize3MFPartPath(raw string) (string, error) {
	s := strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	if s == "" {
		return "", errors.New("percorso 3MF vuoto")
	}
	unescaped, err := url.PathUnescape(s)
	if err != nil {
		return "", errors.New("percorso 3MF con codifica non valida")
	}
	s = strings.TrimPrefix(unescaped, "/")
	clean := pathpkg.Clean(s)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, ":") || strings.ContainsRune(clean, 0) {
		return "", errors.New("percorso 3MF interno non sicuro")
	}
	return clean, nil
}

func resolve3MFModelPath(raw, current string, modelFiles map[string]*zip.File) (string, error) {
	rootCandidate, err := normalize3MFPartPath(raw)
	if err != nil {
		return "", err
	}
	if f, ok := modelFiles[strings.ToLower(rootCandidate)]; ok {
		return f.Name, nil
	}
	// The Production Extension defines p:path as package-absolute, but a few
	// exporters emit a path relative to the current model part. Accept that
	// harmless variant without ever allowing traversal outside the package.
	rawSlash := strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	if !strings.HasPrefix(rawSlash, "/") && current != "" {
		rel, e := normalize3MFPartPath(pathpkg.Join(pathpkg.Dir(current), rootCandidate))
		if e == nil {
			if f, ok := modelFiles[strings.ToLower(rel)]; ok {
				return f.Name, nil
			}
		}
	}
	return "", fmt.Errorf("3MF riferisce file modello inesistente: %s", raw)
}

func readZipEntryLimited(f *zip.File, limit uint64) ([]byte, error) {
	if f == nil || f.UncompressedSize64 > limit {
		return nil, errors.New("parte 3MF troppo grande")
	}
	r, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	b, err := io.ReadAll(io.LimitReader(r, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if uint64(len(b)) > limit {
		return nil, errors.New("parte 3MF oltre il limite")
	}
	return b, nil
}

func findRoot3MFModel(modelFiles map[string]*zip.File, allFiles map[string]*zip.File) (*zip.File, error) {
	if rels := allFiles[strings.ToLower("_rels/.rels")]; rels != nil {
		b, err := readZipEntryLimited(rels, 2*1024*1024)
		if err == nil {
			var rs relationships3MF
			if xml.Unmarshal(b, &rs) == nil {
				for _, rel := range rs.Items {
					t := strings.ToLower(strings.TrimSpace(rel.Type))
					isModelRel := strings.HasSuffix(t, "/3dmodel") || (strings.Contains(t, "3dmanufacturing") && strings.Contains(t, "3dmodel"))
					if isModelRel {
						p, e := normalize3MFPartPath(rel.Target)
						if e == nil {
							if f := modelFiles[strings.ToLower(p)]; f != nil {
								return f, nil
							}
						}
					}
				}
			}
		}
	}
	if f := modelFiles[strings.ToLower("3D/3dmodel.model")]; f != nil {
		return f, nil
	}
	// Avoid choosing a Production Extension child object as the package root.
	var fallback *zip.File
	for _, f := range modelFiles {
		lower := strings.ToLower(strings.ReplaceAll(f.Name, "\\", "/"))
		if fallback == nil {
			fallback = f
		}
		if !strings.Contains(lower, "/objects/") {
			return f, nil
		}
	}
	if fallback != nil {
		return fallback, nil
	}
	return nil, errors.New("3MF senza file .model")
}

func read3MFGeometry(filePath string, deadline analysisDeadline) ([]triangle, int, []string, error) {
	zr, err := zip.OpenReader(filePath)
	if err != nil {
		return nil, 0, nil, errors.New("3MF non è un archivio ZIP valido")
	}
	defer zr.Close()

	allFiles := make(map[string]*zip.File, len(zr.File))
	modelFiles := make(map[string]*zip.File)
	var total uint64
	for _, f := range zr.File {
		clean, e := normalize3MFPartPath(f.Name)
		if e != nil || f.Mode()&os.ModeSymlink != 0 {
			return nil, 0, nil, errors.New("3MF contiene percorsi interni non sicuri")
		}
		total += f.UncompressedSize64
		if total > uint64(MaxModelBytes) {
			return nil, 0, nil, errors.New("3MF decompresso oltre 512 MB")
		}
		key := strings.ToLower(clean)
		if _, exists := allFiles[key]; exists {
			return nil, 0, nil, errors.New("3MF con nomi di parti duplicati")
		}
		allFiles[key] = f
		if strings.HasSuffix(key, ".model") {
			modelFiles[key] = f
		}
	}

	rootFile, err := findRoot3MFModel(modelFiles, allFiles)
	if err != nil {
		return nil, 0, nil, err
	}
	rootPath, err := normalize3MFPartPath(rootFile.Name)
	if err != nil {
		return nil, 0, nil, err
	}

	parts := make(map[string]*modelPart3MF)
	var loadPart func(string) (*modelPart3MF, error)
	loadPart = func(partPath string) (*modelPart3MF, error) {
		normalized, e := normalize3MFPartPath(partPath)
		if e != nil {
			return nil, e
		}
		key := strings.ToLower(normalized)
		if p := parts[key]; p != nil {
			return p, nil
		}
		f := modelFiles[key]
		if f == nil {
			return nil, fmt.Errorf("3MF riferisce file modello inesistente: %s", partPath)
		}
		b, e := readZipEntryLimited(f, max3MFModelXML)
		if e != nil {
			return nil, e
		}
		var doc model3MF
		dec := xml.NewDecoder(strings.NewReader(string(b)))
		if e = dec.Decode(&doc); e != nil {
			return nil, fmt.Errorf("XML 3MF non valido in %s: %w", normalized, e)
		}
		objects := make(map[int]object3MF, len(doc.Resources.Objects))
		for _, o := range doc.Resources.Objects {
			if o.ID <= 0 {
				return nil, fmt.Errorf("3MF con object ID non valido in %s", normalized)
			}
			if _, exists := objects[o.ID]; exists {
				return nil, fmt.Errorf("3MF con object ID duplicato in %s", normalized)
			}
			objects[o.ID] = o
		}
		p := &modelPart3MF{Path: normalized, Doc: doc, Objects: objects, Factor: modelUnitFactor(doc.Unit)}
		parts[key] = p
		return p, nil
	}

	root, err := loadPart(rootPath)
	if err != nil {
		return nil, 0, nil, err
	}
	items := append([]item3MF(nil), root.Doc.Build.Items...)
	if len(items) == 0 {
		for _, o := range root.Doc.Resources.Objects {
			if len(o.Mesh.Triangles) > 0 {
				items = append(items, item3MF{ObjectID: o.ID})
			}
		}
	}
	if len(items) == 0 {
		return nil, 0, nil, errors.New("3MF senza elementi nel build")
	}

	tris := make([]triangle, 0, 10000)
	instances := 0
	usedExternal := false
	var emitObject func(string, int, mat4, map[string]bool, int) error
	emitObject = func(partPath string, id int, parent mat4, stack map[string]bool, depth int) error {
		if depth > 32 {
			return errors.New("3MF con componenti annidati oltre il limite")
		}
		part, e := loadPart(partPath)
		if e != nil {
			return e
		}
		stackKey := strings.ToLower(part.Path) + "#" + strconv.Itoa(id)
		if stack[stackKey] {
			return errors.New("3MF con ciclo nei componenti")
		}
		o, ok := part.Objects[id]
		if !ok {
			return fmt.Errorf("3MF riferisce oggetto inesistente: %d nel file %s", id, part.Path)
		}
		stack[stackKey] = true
		defer delete(stack, stackKey)

		if len(o.Mesh.Triangles) > 0 {
			for i, t := range o.Mesh.Triangles {
				if i%8192 == 0 {
					if e = deadline.check(); e != nil {
						return e
					}
				}
				if t.V1 < 0 || t.V2 < 0 || t.V3 < 0 || t.V1 >= len(o.Mesh.Vertices) || t.V2 >= len(o.Mesh.Vertices) || t.V3 >= len(o.Mesh.Vertices) {
					return errors.New("3MF con indice triangolo fuori intervallo")
				}
				conv := func(v vertex3MF) (vec3, error) {
					p := apply4(parent, vec3{v.X, v.Y, v.Z})
					if !finitePlausible(p.X) || !finitePlausible(p.Y) || !finitePlausible(p.Z) {
						return vec3{}, errors.New("3MF con coordinate trasformate non valide")
					}
					return p, nil
				}
				a, e := conv(o.Mesh.Vertices[t.V1])
				if e != nil {
					return e
				}
				b, e := conv(o.Mesh.Vertices[t.V2])
				if e != nil {
					return e
				}
				c, e := conv(o.Mesh.Vertices[t.V3])
				if e != nil {
					return e
				}
				if det3(parent) < 0 {
					b, c = c, b
				}
				tris = append(tris, triangle{a, b, c})
				if len(tris) > MaxSanitizedTriangles {
					// Say how far over it is: "too many" alone gives the user no
					// idea whether to decimate slightly or split the model.
					return fmt.Errorf("3MF con oltre %d triangoli (limite %d): riduci o suddividi la mesh prima dell’importazione sicura", len(tris), MaxSanitizedTriangles)
				}
			}
		}

		for _, c := range o.Components.Items {
			cm, e := parse3MFTransform(c.Transform)
			if e != nil {
				return e
			}
			targetPath := part.Path
			ratio := 1.0
			if strings.TrimSpace(c.Path) != "" {
				targetPath, e = resolve3MFModelPath(c.Path, part.Path, modelFiles)
				if e != nil {
					return e
				}
				targetPart, e := loadPart(targetPath)
				if e != nil {
					return e
				}
				ratio = targetPart.Factor / part.Factor
				usedExternal = true
			}
			childParent := mul4(parent, mul4(cm, scaleMatrix4(ratio)))
			if e = emitObject(targetPath, c.ObjectID, childParent, stack, depth+1); e != nil {
				return e
			}
		}
		return nil
	}

	rootScale := scaleMatrix4(root.Factor)
	for _, it := range items {
		m, e := parse3MFTransform(it.Transform)
		if e != nil {
			return nil, 0, nil, e
		}
		targetPath := root.Path
		ratio := 1.0
		if strings.TrimSpace(it.Path) != "" {
			targetPath, e = resolve3MFModelPath(it.Path, root.Path, modelFiles)
			if e != nil {
				return nil, 0, nil, e
			}
			targetPart, e := loadPart(targetPath)
			if e != nil {
				return nil, 0, nil, e
			}
			ratio = targetPart.Factor / root.Factor
			usedExternal = true
		}
		parent := mul4(rootScale, mul4(m, scaleMatrix4(ratio)))
		if e = emitObject(targetPath, it.ObjectID, parent, map[string]bool{}, 0); e != nil {
			return nil, 0, nil, e
		}
		instances++
	}
	if len(tris) == 0 {
		return nil, 0, nil, errors.New("3MF senza triangoli utilizzabili")
	}
	var warnings []string
	if usedExternal || len(parts) > 1 {
		warnings = append(warnings, "3MF Production Extension rilevato: geometrie esterne e riferimenti p:path risolti mantenendo trasformazioni e unità.")
	}
	return tris, instances, warnings, nil
}

func writeGeometryOnly3MF(path string, tris []triangle) error {
	if len(tris) == 0 || len(tris) > MaxSanitizedTriangles {
		return fmt.Errorf("triangoli fuori limite per 3MF geometrico: %d", len(tris))
	}
	vertices := make([]vec3, 0, len(tris))
	faces := make([][3]int, 0, len(tris))
	index := make(map[qv]int, len(tris))
	add := func(v vec3) int {
		k := quant(v)
		if i, ok := index[k]; ok {
			return i
		}
		i := len(vertices)
		index[k] = i
		vertices = append(vertices, v)
		return i
	}
	for _, t := range tris {
		// A non-finite coordinate becomes "NaN" or "+Inf" in the file, which
		// makes the whole 3MF unreadable — a corrupt output is worse than a
		// refusal. Analysis already rejects such meshes, so reaching here means
		// something downstream (a placement, a cut) produced one, and it must
		// not be written. This is the last gate before the bytes.
		for _, v := range []vec3{t.A, t.B, t.C} {
			if !finiteVec(v) {
				return errors.New("coordinate non finite: geometria non scrivibile in 3MF")
			}
		}
		faces = append(faces, [3]int{add(t.A), add(t.B), add(t.C)})
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	write := func(name, body string) error {
		w, e := zw.Create(name)
		if e != nil {
			return e
		}
		_, e = io.WriteString(w, body)
		return e
	}
	if err = write("[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="model" ContentType="application/vnd.ms-package.3dmanufacturing-3dmodel+xml"/></Types>`); err != nil {
		zw.Close()
		f.Close()
		return err
	}
	if err = write("_rels/.rels", `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Target="/3D/3dmodel.model" Id="rel0" Type="http://schemas.microsoft.com/3dmanufacturing/2013/01/3dmodel"/></Relationships>`); err != nil {
		zw.Close()
		f.Close()
		return err
	}
	w, err := zw.Create("3D/3dmodel.model")
	if err != nil {
		zw.Close()
		f.Close()
		return err
	}
	bw := bufio.NewWriterSize(w, 256*1024)
	io.WriteString(bw, `<?xml version="1.0" encoding="UTF-8"?><model unit="millimeter" xml:lang="en-US" xmlns="http://schemas.microsoft.com/3dmanufacturing/core/2015/02"><metadata name="Title">FlashFit geometry-only import</metadata><resources><object id="1" type="model"><mesh><vertices>`)
	for _, v := range vertices {
		fmt.Fprintf(bw, `<vertex x="%s" y="%s" z="%s"/>`, coord(v.X), coord(v.Y), coord(v.Z))
	}
	io.WriteString(bw, `</vertices><triangles>`)
	for _, fc := range faces {
		fmt.Fprintf(bw, `<triangle v1="%d" v2="%d" v3="%d"/>`, fc[0], fc[1], fc[2])
	}
	io.WriteString(bw, `</triangles></mesh></object></resources><build><item objectid="1"/></build></model>`)
	if err = bw.Flush(); err != nil {
		zw.Close()
		f.Close()
		return err
	}
	if err = zw.Close(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// coord renders a vertex coordinate for the 3MF.
//
// It writes exactly the precision the mesh actually carries and not a digit
// more. Vertices are welded on a 1e-5 mm grid, so five decimals reproduce the
// welded mesh without loss; the previous nine significant figures spelled out
// the noise of a float64 division, which no printer can act on and every
// consumer of the file has to read.
//
// Fewer digits than the weld would be worse than wasteful: two vertices that
// are distinct on the grid would print identically, and a triangle whose
// corners collapse that way becomes a degenerate face for the slicer to find
// and repair.
//
// On a 135k-triangle model this is about a fifth off the largest part of the
// file, which is parsing the slicer does not have to do.
func coord(v float64) string {
	text := strconv.FormatFloat(v, 'f', 5, 64)
	if !strings.ContainsRune(text, '.') {
		return text
	}
	text = strings.TrimRight(text, "0")
	text = strings.TrimSuffix(text, ".")
	if text == "" || text == "-" {
		return "0"
	}
	return text
}

// finiteVec reports whether every component of a vertex is a real, finite
// number — the only kind a 3MF coordinate is allowed to be.
func finiteVec(v vec3) bool {
	return !math.IsNaN(v.X) && !math.IsNaN(v.Y) && !math.IsNaN(v.Z) &&
		!math.IsInf(v.X, 0) && !math.IsInf(v.Y, 0) && !math.IsInf(v.Z, 0)
}
