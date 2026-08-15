//go:build windows

package main

import (
	"archive/zip"
	"io"
	"regexp"
	"strings"
	"testing"
)

// The published ZIP, the README and the binary must all name the same version.
//
// They drifted once already: a packaging step stopped on an error before
// copying the new archive into downloads/, so the README advertised 4.4.8 while
// the ZIP people actually downloaded was still 4.4.7 — and nothing compared the
// two. This reads the version out of the published archive and holds it to the
// binary's own, which is the one number every other artefact is derived from.
func TestPublishedZipMatchesBuildVersion(t *testing.T) {
	numeric := buildVersion
	if i := strings.IndexAny(numeric, "-+"); i > 0 {
		numeric = numeric[:i]
	}

	zr, err := zip.OpenReader("../downloads/FlashFit-AI-Windows-11-x64.zip")
	if err != nil {
		t.Skipf("archivio pubblicato non presente da qui: %v", err)
	}
	defer zr.Close()

	var readme string
	for _, f := range zr.File {
		if f.Name == "README-Windows.txt" {
			r, _ := f.Open()
			b, _ := io.ReadAll(r)
			r.Close()
			readme = string(b)
		}
	}
	if readme == "" {
		t.Fatal("l'archivio pubblicato non contiene README-Windows.txt")
	}
	m := regexp.MustCompile(`FlashFit AI\s+([0-9]+\.[0-9]+\.[0-9]+)`).FindStringSubmatch(readme)
	if m == nil {
		t.Fatal("nessuna versione nel README dell'archivio")
	}
	if m[1] != numeric {
		t.Fatalf("l'archivio pubblicato è %s ma il binario è %s: release e sorgente divergono", m[1], numeric)
	}
}
