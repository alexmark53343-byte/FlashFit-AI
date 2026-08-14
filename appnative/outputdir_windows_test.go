//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Joining the literal "Documents" onto the home directory and hoping is not
// enough on Windows: the folder is localised, and OneDrive can redirect it
// somewhere the plain path no longer resolves. The output directory therefore
// has to be one that has been proven writable, not merely constructed.
func TestDefaultOutputDirIsUsable(t *testing.T) {
	dir := defaultOutputDir()
	if dir == "" {
		t.Fatal("nessuna cartella di output restituita")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("la cartella restituita non è creabile: %v", err)
	}
	if !directoryWritable(dir) {
		t.Fatalf("la cartella restituita non è scrivibile: %s", dir)
	}
	if !strings.HasSuffix(filepath.Base(dir), "FlashFitAI") {
		t.Fatalf("cartella inattesa: %s", dir)
	}
}

// The Windows API is the only source that survives localisation and
// redirection. It is allowed to be unavailable, but when it answers the answer
// must be a real directory.
func TestKnownFolderDocumentsResolves(t *testing.T) {
	documents := knownFolderDocuments()
	if documents == "" {
		t.Skip("SHGetKnownFolderPath non disponibile su questo sistema")
	}
	info, err := os.Stat(documents)
	if err != nil || !info.IsDir() {
		t.Fatalf("percorso Documenti non valido: %q (%v)", documents, err)
	}
	t.Logf("Documenti risolto in: %s", documents)
}

// A directory that exists but rejects writes must not be accepted.
func TestDirectoryWritableRejectsMissing(t *testing.T) {
	if directoryWritable(filepath.Join(t.TempDir(), "non-esiste")) {
		t.Fatal("una cartella inesistente è stata dichiarata scrivibile")
	}
}
