//go:build windows

package main

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Fetching a model on demand.
//
// Carrying the weights inside the executable made it a gigabyte, which is a
// gigabyte in every clone of the repository and every download by someone who
// only wanted the app. The weights are fetched instead when a model is first
// chosen, and the app stays a few megabytes.
//
// A download of this size has to be resumable and has to be honest about its
// progress: a silent minute looks identical to a hang.

type advisorCatalogEntry struct {
	ID       string
	Label    string
	URL      string
	Bytes    int64
	SHA256   string
	Describe string
}

// The two models offered. Sizes are the published ones and are verified after
// the transfer; a partial file is never handed to the server.
var advisorCatalog = []advisorCatalogEntry{
	{
		ID:       "light",
		Label:    "Qwen2.5 1.5B",
		URL:      "https://huggingface.co/Qwen/Qwen2.5-1.5B-Instruct-GGUF/resolve/main/qwen2.5-1.5b-instruct-q4_k_m.gguf",
		Bytes:    1117320736,
		Describe: "leggera, gira su qualsiasi macchina",
	},
	{
		ID:       "strong",
		Label:    "Qwen2.5 3B",
		URL:      "https://huggingface.co/Qwen/Qwen2.5-3B-Instruct-GGUF/resolve/main/qwen2.5-3b-instruct-q4_k_m.gguf",
		Bytes:    2104932768,
		Describe: "riconosce oggetti più insoliti, chiede più memoria",
	},
}

// The llama.cpp runtime. Weights are useless without an engine to run them, so
// a build that carries neither has to fetch both — and this one first, because
// it is small and its absence is the difference between "downloading" and
// "downloaded a gigabyte and still cannot start".
var advisorRuntimeSource = advisorCatalogEntry{
	ID:       "runtime",
	Label:    "motore llama.cpp",
	URL:      "https://github.com/ggml-org/llama.cpp/releases/download/b10428/llama-b10428-bin-win-cpu-x64.zip",
	Bytes:    18456625,
	Describe: "runtime CPU, nessuna GPU richiesta",
}

func advisorRuntimeInstalled() bool {
	return fileExists(filepath.Join(advisorModelsDir(), "runtime", "llama-server.exe"))
}

func advisorCatalogEntryByID(id string) (advisorCatalogEntry, bool) {
	for _, entry := range advisorCatalog {
		if entry.ID == id {
			return entry, true
		}
	}
	return advisorCatalogEntry{}, false
}

// advisorModelFile is where a catalogue entry lands once fetched.
func advisorModelFile(entry advisorCatalogEntry) string {
	return filepath.Join(advisorModelsDir(), entry.ID+".gguf")
}

// findExistingModel looks for weights already present that are this catalogue
// entry, whatever they happen to be called. A GGUF is identified by its exact
// byte count: two different models never share one, and the same model always
// has the same.
func findExistingModel(entry advisorCatalogEntry) (string, bool) {
	dir := advisorModelsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	for _, file := range entries {
		if file.IsDir() || !strings.HasSuffix(strings.ToLower(file.Name()), ".gguf") {
			continue
		}
		info, statErr := file.Info()
		if statErr != nil || info.Size() != entry.Bytes {
			continue
		}
		path := filepath.Join(dir, file.Name())
		// Size alone is weak identity for a multi-gigabyte file: a truncated
		// download, an error page padded to length, or an unrelated file of the
		// same size would all pass. A GGUF begins with a four-byte magic, so
		// checking it is a cheap way to refuse something that is the right size
		// but not a model.
		if !looksLikeGGUF(path) {
			continue
		}
		return path, true
	}
	return "", false
}

// looksLikeGGUF reports whether a file begins with the GGUF magic bytes. It is
// not a full validation — only a full hash would be that — but it catches the
// common corruption an interrupted or hijacked download produces: an HTML error
// page, a truncated transfer, or a wholly unrelated file.
func looksLikeGGUF(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var magic [4]byte
	if _, err := io.ReadFull(f, magic[:]); err != nil {
		return false
	}
	return magic[0] == 'G' && magic[1] == 'G' && magic[2] == 'U' && magic[3] == 'F'
}

// advisorModelPresent reports whether a catalogue entry can be loaded without
// downloading anything, so the interface can say so before the user commits to
// a transfer.
func advisorModelPresent(id string) bool {
	entry, ok := advisorCatalogEntryByID(id)
	if !ok {
		return false
	}
	_, present := findExistingModel(entry)
	return present
}

type advisorDownload struct {
	mu       sync.Mutex
	active   bool
	entry    advisorCatalogEntry
	received int64
	total    int64
	failed   string
	cancel   context.CancelFunc
}

var advisorFetch advisorDownload

// DownloadState is what the interface draws.
type advisorDownloadState struct {
	Active   bool
	Label    string
	Fraction float64
	Received int64
	Total    int64
	Failed   string
}

func advisorDownloadStatus() advisorDownloadState {
	advisorFetch.mu.Lock()
	defer advisorFetch.mu.Unlock()
	state := advisorDownloadState{
		Active:   advisorFetch.active,
		Label:    advisorFetch.entry.Label,
		Received: advisorFetch.received,
		Total:    advisorFetch.total,
		Failed:   advisorFetch.failed,
	}
	if state.Total > 0 {
		state.Fraction = float64(state.Received) / float64(state.Total)
	}
	return state
}

func cancelAdvisorDownload() {
	advisorFetch.mu.Lock()
	cancel := advisorFetch.cancel
	advisorFetch.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// startAdvisorDownload fetches a model in the background and selects it when it
// completes. Calling it for a model already on disk simply selects that one.
func startAdvisorDownload(entry advisorCatalogEntry) {
	// Look for the weights already on disk before fetching a gigabyte again.
	// Matching on the name we would have given the file is not enough: weights
	// downloaded by hand, or by an earlier version of this app, sit there under
	// the name their publisher used. Size identifies them regardless.
	if existing, ok := findExistingModel(entry); ok {
		selectAdvisorModel(existing)
		return
	}
	target := advisorModelFile(entry)

	advisorFetch.mu.Lock()
	if advisorFetch.active {
		advisorFetch.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	advisorFetch.active, advisorFetch.entry, advisorFetch.cancel = true, entry, cancel
	advisorFetch.received, advisorFetch.total, advisorFetch.failed = 0, entry.Bytes, ""
	advisorFetch.mu.Unlock()
	invalidateSpatial()

	go func() {
		defer cancel()

		// The engine comes first: it is 18 MB against a gigabyte, and without
		// it the weights cannot be run at all.
		err := ensureAdvisorRuntime(ctx)
		if err == nil {
			advisorFetch.mu.Lock()
			advisorFetch.entry, advisorFetch.received, advisorFetch.total = entry, 0, entry.Bytes
			advisorFetch.mu.Unlock()
			err = fetchAdvisorModel(ctx, entry, target)
		}

		advisorFetch.mu.Lock()
		advisorFetch.active = false
		if err != nil {
			advisorFetch.failed = err.Error()
		}
		advisorFetch.mu.Unlock()

		if err != nil {
			writeLog("advisor: download fallito: " + err.Error())
		} else {
			writeLog("advisor: modello scaricato: " + filepath.Base(target))
			selectAdvisorModel(target)
		}
		if mainHwnd != 0 {
			pPostMessage.Call(mainHwnd, WM_ADVISOR_READY, 0, 0)
		}
	}()
}

// fetchAdvisorModel downloads to a partial file and only renames it into place
// once the whole thing has arrived, so an interrupted transfer can never be
// mistaken for a usable model. An existing partial file is resumed.
func fetchAdvisorModel(ctx context.Context, entry advisorCatalogEntry, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	partial := target + ".part"

	var already int64
	if info, err := os.Stat(partial); err == nil {
		already = info.Size()
	}
	if already > entry.Bytes {
		// A partial larger than the whole file is corrupt; start over.
		os.Remove(partial)
		already = 0
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, entry.URL, nil)
	if err != nil {
		return err
	}
	if already > 0 {
		request.Header.Set("Range", fmt.Sprintf("bytes=%d-", already))
	}

	client := &http.Client{Timeout: 0} // a multi-gigabyte transfer sets its own pace
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	switch response.StatusCode {
	case http.StatusOK:
		// The server ignored the range request, so start from scratch.
		already = 0
	case http.StatusPartialContent:
		// Resuming.
	default:
		return fmt.Errorf("il server ha risposto %d", response.StatusCode)
	}

	flags := os.O_CREATE | os.O_WRONLY
	if already > 0 {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	file, err := os.OpenFile(partial, flags, 0o600)
	if err != nil {
		return err
	}

	written := already
	buffer := make([]byte, 1<<20)
	lastReport := time.Now()
	for {
		read, readErr := response.Body.Read(buffer)
		if read > 0 {
			if _, writeErr := file.Write(buffer[:read]); writeErr != nil {
				file.Close()
				return writeErr
			}
			written += int64(read)

			// Report at a human rate, not per megabyte: repainting the window
			// a thousand times a second would cost more than the download.
			if time.Since(lastReport) > 200*time.Millisecond {
				lastReport = time.Now()
				advisorFetch.mu.Lock()
				advisorFetch.received = written
				advisorFetch.mu.Unlock()
				if mainHwnd != 0 {
					pPostMessage.Call(mainHwnd, WM_ADVISOR_PROGRESS, 0, 0)
				}
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			file.Close()
			// The partial file is kept: the next attempt resumes from here.
			return readErr
		}
		if ctx.Err() != nil {
			file.Close()
			return ctx.Err()
		}
	}
	if err := file.Close(); err != nil {
		return err
	}

	if info, statErr := os.Stat(partial); statErr != nil || info.Size() != entry.Bytes {
		size := int64(0)
		if info != nil {
			size = info.Size()
		}
		return fmt.Errorf("scaricati %d byte invece di %d", size, entry.Bytes)
	}
	if entry.SHA256 != "" {
		sum, hashErr := fileDigest(partial)
		if hashErr != nil {
			return hashErr
		}
		if sum != entry.SHA256 {
			os.Remove(partial)
			return fmt.Errorf("impronta del file scaricato non corrispondente")
		}
	} else if strings.HasSuffix(strings.ToLower(target), ".gguf") && !looksLikeGGUF(partial) {
		// No published hash to check against, so the content is verified as far
		// as it cheaply can be: a file that is the right size but does not even
		// begin with the GGUF magic is a corrupt or hijacked transfer, and
		// keeping it would hand the model server something it cannot load.
		os.Remove(partial)
		return fmt.Errorf("il file scaricato non è un modello GGUF valido")
	}
	return os.Rename(partial, target)
}

// ensureAdvisorRuntime fetches and unpacks the engine if it is not already
// there. Only the server and its libraries are kept; the release archive also
// carries tools this app never invokes.
func ensureAdvisorRuntime(ctx context.Context) error {
	if advisorRuntimeInstalled() {
		return nil
	}
	advisorFetch.mu.Lock()
	advisorFetch.entry = advisorRuntimeSource
	advisorFetch.received, advisorFetch.total = 0, advisorRuntimeSource.Bytes
	advisorFetch.mu.Unlock()

	archive := filepath.Join(advisorModelsDir(), "runtime.zip")
	if err := fetchAdvisorModel(ctx, advisorRuntimeSource, archive); err != nil {
		return fmt.Errorf("motore non scaricabile: %w", err)
	}
	defer os.Remove(archive)

	runtimeDir := filepath.Join(advisorModelsDir(), "runtime")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		return err
	}
	reader, err := zip.OpenReader(archive)
	if err != nil {
		return fmt.Errorf("archivio del motore illeggibile: %w", err)
	}
	defer reader.Close()
	for _, file := range reader.File {
		name := filepath.Base(file.Name)
		lower := strings.ToLower(name)
		if !strings.HasSuffix(lower, ".dll") && lower != "llama-server.exe" {
			continue
		}
		source, openErr := file.Open()
		if openErr != nil {
			return openErr
		}
		// filepath.Base has already stripped any path, so nothing can be
		// written outside the destination.
		out, createErr := os.OpenFile(filepath.Join(runtimeDir, name), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o700)
		if createErr != nil {
			source.Close()
			return createErr
		}
		_, copyErr := io.Copy(out, source)
		source.Close()
		out.Close()
		if copyErr != nil {
			return copyErr
		}
	}
	if !advisorRuntimeInstalled() {
		return fmt.Errorf("llama-server.exe non presente dopo l'estrazione")
	}
	return nil
}

func fileDigest(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	sum := sha256.New()
	if _, err := io.Copy(sum, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}
