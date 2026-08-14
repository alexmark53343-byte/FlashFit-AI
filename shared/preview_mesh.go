package shared

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// PreviewTriangle is a display-only copy of a face. Preview geometry is
// decimated and never feeds slicing, analysis or the generated project.
type PreviewTriangle struct {
	AX, AY, AZ float32
	BX, BY, BZ float32
	CX, CY, CZ float32
}

// LoadPreviewMesh reads a bounded, evenly sampled triangle soup for on-screen
// display. Sampling keeps huge meshes cheap: the viewport only needs a shape,
// while every dimension shown to the user still comes from the full analysis.
func LoadPreviewMesh(path string, maxTriangles int) ([]PreviewTriangle, error) {
	if maxTriangles <= 0 {
		maxTriangles = 20000
	}
	path = filepath.Clean(path)
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if st.IsDir() || st.Size() <= 0 || st.Size() > MaxModelBytes {
		return nil, errors.New("file modello non valido per l'anteprima")
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".stl":
		return readSTLPreview(path, maxTriangles)
	case ".obj":
		tris, _, _, err := readOBJGeometry(path, newAnalysisDeadline())
		if err != nil {
			return nil, err
		}
		return samplePreview(tris, maxTriangles), nil
	case ".3mf":
		tris, _, _, err := read3MFGeometry(path, newAnalysisDeadline())
		if err != nil {
			return nil, err
		}
		return samplePreview(tris, maxTriangles), nil
	default:
		return nil, errors.New("formato non supportato per l'anteprima")
	}
}

// samplePreview reduces a mesh for display by merging vertices that are close
// together, not by throwing triangles away.
//
// Keeping one triangle in every N looks like decimation on paper and is
// nothing of the sort: it leaves the survivors floating with gaps where their
// neighbours used to be. On a 750k-triangle car against a 120k budget, five
// triangles in six vanished and the result read as a comb of slivers rather
// than a car.
//
// Vertex clustering instead snaps every vertex onto a grid and keeps one
// representative per cell. Triangles whose corners collapse into the same cell
// disappear because they have no area left, and the rest stay joined to their
// neighbours — so the surface stays a surface, just a coarser one.
func samplePreview(tris []triangle, maxTriangles int) []PreviewTriangle {
	if len(tris) == 0 {
		return nil
	}
	if len(tris) <= maxTriangles {
		out := make([]PreviewTriangle, 0, len(tris))
		for _, t := range tris {
			out = append(out, previewFrom(t.A, t.B, t.C))
		}
		return out
	}

	min, max := meshBounds(tris)
	span := math.Max(max.X-min.X, math.Max(max.Y-min.Y, max.Z-min.Z))
	if span <= 0 {
		return nil
	}
	// Triangles on a surface scale with the square of the grid resolution, so
	// this is the resolution that lands near the requested count.
	resolution := int(math.Sqrt(float64(maxTriangles)))
	if resolution < 32 {
		resolution = 32
	}
	if resolution > 900 {
		resolution = 900
	}
	cell := span / float64(resolution)

	snap := func(v vec3) (cellKey, vec3) {
		kx := int32(math.Floor((v.X - min.X) / cell))
		ky := int32(math.Floor((v.Y - min.Y) / cell))
		kz := int32(math.Floor((v.Z - min.Z) / cell))
		return cellKey{kx, ky, kz}, vec3{
			min.X + (float64(kx)+0.5)*cell,
			min.Y + (float64(ky)+0.5)*cell,
			min.Z + (float64(kz)+0.5)*cell,
		}
	}

	out := make([]PreviewTriangle, 0, maxTriangles)
	seen := make(map[[3]cellKey]bool, maxTriangles)
	for _, t := range tris {
		ka, pa := snap(t.A)
		kb, pb := snap(t.B)
		kc, pc := snap(t.C)
		// Two corners in one cell means the triangle has collapsed to a line.
		if ka == kb || kb == kc || ka == kc {
			continue
		}
		// Order-independent key so the same face is not emitted twice.
		key := [3]cellKey{ka, kb, kc}
		sortCellKeys(&key)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, previewFrom(pa, pb, pc))
	}
	return out
}

func previewFrom(a, b, c vec3) PreviewTriangle {
	return PreviewTriangle{
		AX: float32(a.X), AY: float32(a.Y), AZ: float32(a.Z),
		BX: float32(b.X), BY: float32(b.Y), BZ: float32(b.Z),
		CX: float32(c.X), CY: float32(c.Y), CZ: float32(c.Z),
	}
}

func meshBounds(tris []triangle) (vec3, vec3) {
	min := vec3{math.Inf(1), math.Inf(1), math.Inf(1)}
	max := vec3{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	for _, t := range tris {
		for _, v := range []vec3{t.A, t.B, t.C} {
			min.X, max.X = math.Min(min.X, v.X), math.Max(max.X, v.X)
			min.Y, max.Y = math.Min(min.Y, v.Y), math.Max(max.Y, v.Y)
			min.Z, max.Z = math.Min(min.Z, v.Z), math.Max(max.Z, v.Z)
		}
	}
	return min, max
}

// cellKey identifies one cell of the clustering grid.
type cellKey struct{ X, Y, Z int32 }

func sortCellKeys(k *[3]cellKey) {
	less := func(a, b cellKey) bool {
		if a.X != b.X {
			return a.X < b.X
		}
		if a.Y != b.Y {
			return a.Y < b.Y
		}
		return a.Z < b.Z
	}
	if less(k[1], k[0]) {
		k[0], k[1] = k[1], k[0]
	}
	if less(k[2], k[1]) {
		k[1], k[2] = k[2], k[1]
	}
	if less(k[1], k[0]) {
		k[0], k[1] = k[1], k[0]
	}
}

func readSTLPreview(path string, maxTriangles int) ([]PreviewTriangle, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	header := make([]byte, 84)
	if _, err := io.ReadFull(f, header); err != nil {
		return nil, err
	}
	count := int(binary.LittleEndian.Uint32(header[80:84]))
	if count > 0 && count <= MaxTriangles && int64(84)+int64(count)*50 == st.Size() {
		if _, err := f.Seek(84, io.SeekStart); err != nil {
			return nil, err
		}
		return readBinarySTLPreview(f, count, maxTriangles)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	return readASCIISTLPreview(f, maxTriangles)
}

func readBinarySTLPreview(r io.Reader, count, maxTriangles int) ([]PreviewTriangle, error) {
	stride := 1
	if count > maxTriangles {
		stride = (count + maxTriangles - 1) / maxTriangles
	}
	reader := bufio.NewReaderSize(r, 1<<16)
	buf := make([]byte, 50)
	out := make([]PreviewTriangle, 0, count/stride+1)
	for i := 0; i < count; i++ {
		if _, err := io.ReadFull(reader, buf); err != nil {
			if len(out) > 0 {
				return out, nil
			}
			return nil, fmt.Errorf("STL troncato al triangolo %d", i+1)
		}
		if i%stride != 0 {
			continue
		}
		out = append(out, PreviewTriangle{
			AX: f32(buf[12:]), AY: f32(buf[16:]), AZ: f32(buf[20:]),
			BX: f32(buf[24:]), BY: f32(buf[28:]), BZ: f32(buf[32:]),
			CX: f32(buf[36:]), CY: f32(buf[40:]), CZ: f32(buf[44:]),
		})
	}
	return out, nil
}

func f32(b []byte) float32 {
	return math.Float32frombits(binary.LittleEndian.Uint32(b[:4]))
}

func readASCIISTLPreview(r io.Reader, maxTriangles int) ([]PreviewTriangle, error) {
	scanner := bufio.NewScanner(io.LimitReader(r, int64(MaxModelBytes)))
	scanner.Buffer(make([]byte, 0, 1<<16), 1<<20)
	var vertices []float32
	var out []PreviewTriangle
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 4 || !strings.EqualFold(fields[0], "vertex") {
			continue
		}
		for i := 1; i <= 3; i++ {
			v, err := strconv.ParseFloat(fields[i], 32)
			if err != nil {
				return nil, errors.New("STL ASCII non valido")
			}
			vertices = append(vertices, float32(v))
		}
		if len(vertices) == 9 {
			out = append(out, PreviewTriangle{
				AX: vertices[0], AY: vertices[1], AZ: vertices[2],
				BX: vertices[3], BY: vertices[4], BZ: vertices[5],
				CX: vertices[6], CY: vertices[7], CZ: vertices[8],
			})
			vertices = vertices[:0]
			if len(out) > MaxTriangles {
				return nil, errors.New("STL ASCII oltre il limite di triangoli")
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(out) <= maxTriangles {
		return out, nil
	}
	stride := (len(out) + maxTriangles - 1) / maxTriangles
	sampled := make([]PreviewTriangle, 0, len(out)/stride+1)
	for i := 0; i < len(out); i += stride {
		sampled = append(sampled, out[i])
	}
	return sampled, nil
}
