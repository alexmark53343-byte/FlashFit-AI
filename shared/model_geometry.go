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
