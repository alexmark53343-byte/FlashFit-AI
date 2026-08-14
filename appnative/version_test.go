package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The version in the binary and the version the README advertises have to be
// the same number.
//
// They drifted, and the way they drifted is the point: three releases went out
// with the archive, the release notes and the download heading all saying 4.4.x
// while the executable still reported 4.3.0 in its title bar, its chrome and
// its --version. Everything a user could check said one thing and the thing
// they were running said another, so there was no way to tell an update had
// landed. Nothing failed, because nothing was comparing them.
func TestBuildVersionMatchesTheAdvertisedDownload(t *testing.T) {
	readme, err := os.ReadFile("../README.md")
	if err != nil {
		t.Skipf("README non leggibile da qui: %v", err)
	}
	// The download heading, e.g. "### Windows 11 x64 — 4.4.2 · current".
	heading := regexp.MustCompile(`(?m)^###\s+Windows[^\n]*?([0-9]+\.[0-9]+\.[0-9]+)`)
	match := heading.FindSubmatch(readme)
	if match == nil {
		t.Fatal("nessuna versione Windows annunciata nel README: il confronto non può avvenire")
	}
	advertised := string(match[1])

	// The build string carries its channel after the number; only the number is
	// the version.
	binary := buildVersion
	if index := strings.IndexAny(binary, "-+"); index > 0 {
		binary = binary[:index]
	}
	if binary != advertised {
		t.Fatalf("il binario dichiara %q ma il README pubblicizza %q: aggiorna buildVersion prima di pubblicare", binary, advertised)
	}
}
