package shared

import (
	"archive/zip"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Producing a project file needs the mesh as geometry, but the cached copy is
// not always in a form that carries settings.
//
// For OBJ and 3MF input the pipeline already stores a converted geometry 3MF.
// For STL it deliberately keeps the original file byte-for-byte, so that the
// bytes the user analysed are the bytes that get printed — which means the
// cached copy is a plain STL with nowhere to put a config. This converts
// whatever is cached into a 3MF that can carry one, without ever touching the
// user's own file.

// isZipArchive reports whether a file starts with the local file header every
// zip — and therefore every 3MF — begins with.
func isZipArchive(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	header := make([]byte, 4)
	if _, err := f.Read(header); err != nil {
		return false
	}
	return header[0] == 'P' && header[1] == 'K' && header[2] == 0x03 && header[3] == 0x04
}

// EnsureGeometry3MF returns a path to a 3MF holding the same geometry as the
// given model. If the model is already a 3MF the path is returned unchanged;
// otherwise a converted copy is written into workDir.
func EnsureGeometry3MF(modelPath, workDir string) (string, error) {
	return EnsureGeometry3MFScaled(modelPath, workDir, 1)
}

// EnsureGeometry3MFScaled additionally resizes the mesh, for models that have
// to be brought down to the size of the plate. A scale of 1 leaves the geometry
// untouched, and the user's own file is never modified either way.
func EnsureGeometry3MFScaled(modelPath, workDir string, scale float64) (string, error) {
	if scale == 1 && isZipArchive(modelPath) {
		return modelPath, nil
	}
	tris, err := loadModelTriangles(modelPath)
	if err != nil {
		return "", err
	}
	tris = scaleTriangles(tris, scale)
	converted := filepath.Join(workDir, "geometry.3mf")
	if err := writeGeometryOnly3MF(converted, tris); err != nil {
		return "", fmt.Errorf("conversione geometria in 3MF fallita: %w", err)
	}
	return converted, nil
}

// loadModelTriangles reads the mesh from any format the app accepts.
func loadModelTriangles(path string) ([]triangle, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".stl":
		return readSTLTriangles(path)
	case ".obj":
		tris, _, _, err := readOBJGeometry(path, newAnalysisDeadline())
		return tris, err
	case ".3mf":
		tris, _, _, err := read3MFGeometry(path, newAnalysisDeadline())
		return tris, err
	default:
		return nil, fmt.Errorf("formato non supportato per la conversione: %s", filepath.Ext(path))
	}
}

// verifyZipReadable is a cheap guard used before repackaging: a truncated or
// corrupt archive should fail here rather than half way through a copy.
func verifyZipReadable(path string) error {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	return reader.Close()
}

// Putting the model on the plate.
//
// A mesh arrives wherever its author left it: centred on the origin, in a
// corner, or a hundred millimetres off because that is where it sat in the
// scene it was exported from. Nothing in the pipeline moved it, so the slicer
// opened the project and drew the part next to the bed rather than on it, and
// the user dragged it back every single time.
//
// Every slicer's "place on bed" does the same two things, and so does this:
// centre the footprint on the plate, and drop the lowest point to z = 0. It is
// applied to the coordinates rather than to a transform in the file, because
// the transform is the part each slicer reads slightly differently and the
// coordinates are the part they all agree on.
//
// The mesh is not resized. A part too big for the plate stays too big — that is
// the plate splitter's job, and quietly shrinking it to fit is the behaviour
// this project removed on purpose.
func centreOnPlate(tris []triangle, plate [3]float64) []triangle {
	if len(tris) == 0 || plate[0] <= 0 || plate[1] <= 0 {
		return tris
	}
	min, max := meshBounds(tris)
	dx := plate[0]/2 - (min.X+max.X)/2
	dy := plate[1]/2 - (min.Y+max.Y)/2
	dz := -min.Z
	if dx == 0 && dy == 0 && dz == 0 {
		return tris
	}
	moved := make([]triangle, len(tris))
	for i, t := range tris {
		moved[i] = triangle{
			A: vec3{t.A.X + dx, t.A.Y + dy, t.A.Z + dz},
			B: vec3{t.B.X + dx, t.B.Y + dy, t.B.Z + dz},
			C: vec3{t.C.X + dx, t.C.Y + dy, t.C.Z + dz},
		}
	}
	return moved
}

// PlaceGeometryOnPlate rewrites a geometry 3MF with the mesh sitting on the
// plate, and returns the path to use.
//
// It always rewrites, including when the input is already a 3MF: the whole
// point is to change where the geometry is, and passing it straight through is
// what left the part off the bed.
func PlaceGeometryOnPlate(modelPath, workDir string, plate [3]float64) (string, error) {
	tris, err := loadModelTriangles(modelPath)
	if err != nil {
		return "", err
	}
	placed := filepath.Join(workDir, "placed.3mf")
	if err := writeGeometryOnly3MF(placed, centreOnPlate(tris, plate)); err != nil {
		return "", fmt.Errorf("posizionamento sul piatto fallito: %w", err)
	}
	return placed, nil
}
