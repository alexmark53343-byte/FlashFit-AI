//go:build windows

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// A file that is the right size but not a GGUF must not be accepted as a model.
// Byte-size alone is weak identity for a multi-gigabyte download: a truncated
// transfer, an HTML error page padded to length, or an unrelated file of the
// same size would all pass it. The magic-byte check refuses those cheaply.
func TestLooksLikeGGUF(t *testing.T) {
	dir := t.TempDir()

	good := filepath.Join(dir, "model.gguf")
	if err := os.WriteFile(good, append([]byte("GGUF"), make([]byte, 100)...), 0o600); err != nil {
		t.Fatal(err)
	}
	if !looksLikeGGUF(good) {
		t.Fatal("un file GGUF valido è stato rifiutato")
	}

	for name, content := range map[string][]byte{
		"pagina html":   []byte("<!DOCTYPE html><html><body>404 Not Found"),
		"vuoto":         {},
		"troncato":      []byte("GG"),
		"magic sbagliato": []byte("ZZZZ and the rest"),
	} {
		bad := filepath.Join(dir, "bad")
		if err := os.WriteFile(bad, content, 0o600); err != nil {
			t.Fatal(err)
		}
		if looksLikeGGUF(bad) {
			t.Errorf("%s: accettato come modello GGUF", name)
		}
		os.Remove(bad)
	}
}

// findExistingModel must not pick up a right-sized file that is not a model.
func TestFindExistingModelRejectsNonGGUF(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("APPDATA", dir)
	models := filepath.Join(dir, "FlashFitAI", "models")
	if err := os.MkdirAll(models, 0o700); err != nil {
		t.Fatal(err)
	}
	entry := advisorCatalogEntry{ID: "light", Bytes: 64}

	// A file of exactly the right size, but filled with zeros — not a GGUF.
	impostor := filepath.Join(models, "light.gguf")
	if err := os.WriteFile(impostor, make([]byte, 64), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := findExistingModel(entry); ok {
		t.Fatal("un file della dimensione giusta ma non-GGUF è stato scambiato per un modello")
	}

	// Now make it a real GGUF of the right size.
	if err := os.WriteFile(impostor, append([]byte("GGUF"), make([]byte, 60)...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := findExistingModel(entry); !ok {
		t.Fatal("un GGUF valido della dimensione giusta non è stato trovato")
	}
}
