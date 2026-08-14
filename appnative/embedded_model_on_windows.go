//go:build windows && embedmodel

package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// The model and its runtime, carried inside the executable.
//
// This is what turns "install llama.cpp, fetch a GGUF, set an environment
// variable" into "run the app". The cost is honest and large: the binary grows
// from ~7 MB to ~1.1 GB, and building it needs the assets present. Development
// builds therefore leave this file out entirely — it is behind the `embedmodel`
// tag — so the usual `go build` and `go test` stay fast.
//
// The payload cannot be used from memory: llama.cpp memory-maps the weights
// from a path. So on first run each asset is written once into the models
// directory and reused from there afterwards, verified by digest rather than by
// mere presence, because a half-written file from an interrupted first run is
// worse than no file at all.

//go:embed assets/model.gguf
var embeddedModel []byte

//go:embed assets/llama-cpu.zip
var embeddedRuntime []byte

func advisorHasEmbeddedModel() bool { return len(embeddedModel) > 0 }

// ensureEmbeddedAssets materialises the payload and returns the server and
// model paths. It is safe to call on every launch: after the first, it only
// hashes what is already on disk.
func ensureEmbeddedAssets(progress func(string)) (server string, model string, err error) {
	dir := advisorModelsDir()
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return "", "", err
	}

	model = filepath.Join(dir, "model.gguf")
	if err = materialiseFile(model, embeddedModel, progress, "modello"); err != nil {
		return "", "", err
	}

	runtimeDir := filepath.Join(dir, "runtime")
	server = filepath.Join(runtimeDir, "llama-server.exe")
	if !fileExists(server) {
		if err = extractRuntime(runtimeDir, progress); err != nil {
			return "", "", err
		}
	}
	if !fileExists(server) {
		return "", "", fmt.Errorf("llama-server.exe non presente dopo l'estrazione in %s", runtimeDir)
	}
	return server, model, nil
}

// materialiseFile writes payload to path unless it is already there and intact.
//
// Intactness is recorded in a marker written after a successful extraction,
// rather than re-derived on each launch. Hashing the embedded payload to check
// would touch all 1.07 GB of it, which paged the whole model into the app's
// working set every single start — over a gigabyte resident to answer a
// question already settled the first time.
//
// The marker names the exact size that was written, so a run interrupted
// mid-write leaves a file that does not match and is redone.
func materialiseFile(path string, payload []byte, progress func(string), label string) error {
	marker := path + ".ok"
	if extractionIntact(path, marker, int64(len(payload))) {
		return nil
	}
	if progress != nil {
		progress(label)
	}
	// Write beside the target and rename, so a crash mid-write cannot leave a
	// truncated file that looks complete.
	temp := path + ".part"
	if err := os.WriteFile(temp, payload, 0o600); err != nil {
		return err
	}
	if err := os.Rename(temp, path); err != nil {
		os.Remove(temp)
		return err
	}
	// The marker goes down last, so it can only ever describe a file that was
	// written in full.
	digest := sha256.Sum256(payload)
	record := fmt.Sprintf("%d %s", len(payload), hex.EncodeToString(digest[:]))
	if err := os.WriteFile(marker, []byte(record), 0o600); err != nil {
		return err
	}
	return nil
}

// extractionIntact answers, cheaply, whether a previous run already wrote this
// payload out completely.
func extractionIntact(path, marker string, wantSize int64) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() != wantSize {
		return false
	}
	recorded, err := os.ReadFile(marker)
	if err != nil {
		return false
	}
	var size int64
	if _, err := fmt.Sscanf(string(recorded), "%d", &size); err != nil {
		return false
	}
	return size == wantSize
}

// extractRuntime unpacks the llama.cpp binaries. Only the files the server
// actually needs are taken, and every entry is checked so a crafted archive
// cannot write outside the destination.
func extractRuntime(dir string, progress func(string)) error {
	if progress != nil {
		progress("runtime")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	reader, err := zip.NewReader(bytes.NewReader(embeddedRuntime), int64(len(embeddedRuntime)))
	if err != nil {
		return err
	}
	for _, entry := range reader.File {
		name := filepath.Base(entry.Name)
		lower := strings.ToLower(name)
		if !strings.HasSuffix(lower, ".dll") && lower != "llama-server.exe" {
			continue
		}
		target := filepath.Join(dir, name)
		// filepath.Base already strips any path, so target cannot escape dir;
		// this re-check documents the intent and survives future edits.
		if !strings.HasPrefix(target, filepath.Clean(dir)+string(os.PathSeparator)) {
			continue
		}
		source, err := entry.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o700)
		if err != nil {
			source.Close()
			return err
		}
		_, copyErr := io.Copy(out, source)
		source.Close()
		out.Close()
		if copyErr != nil {
			return copyErr
		}
	}
	return nil
}
