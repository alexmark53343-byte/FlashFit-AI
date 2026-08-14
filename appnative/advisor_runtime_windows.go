//go:build windows

package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"flashfitai/shared"
)

// Lifecycle management for the local model.
//
// The model is far too large to live inside a 7 MB executable, so it lives
// beside it and the app owns its process instead: it finds the runtime and the
// weights, starts the server on a free loopback port, waits for it to become
// healthy, and guarantees it dies with the app.
//
// That last guarantee is the one that matters. A user who force-quits the app,
// or a crash in our own code, must not leave a multi-gigabyte server resident.
// Relying on a deferred Kill would not survive either case, so the child is put
// in a Windows job object with kill-on-close: when our process handle goes away
// for any reason, the kernel terminates the child.

const (
	advisorHealthTimeout = 90 * time.Second
	advisorHealthPoll    = 400 * time.Millisecond
)

var (
	pCreateJobObject         = kernel32.NewProc("CreateJobObjectW")
	pSetInformationJobObject = kernel32.NewProc("SetInformationJobObject")
	pAssignProcessToJobObj   = kernel32.NewProc("AssignProcessToJobObject")
	pOpenProcess             = kernel32.NewProc("OpenProcess")
	pCloseHandle             = kernel32.NewProc("CloseHandle")
)

const (
	jobObjectExtendedLimitInformation = 9
	limitKillOnJobClose               = 0x00002000
	processSetQuota                   = 0x0100
	processTerminate                  = 0x0001
)

type ioCounters struct {
	ReadOperationCount  uint64
	WriteOperationCount uint64
	OtherOperationCount uint64
	ReadTransferCount   uint64
	WriteTransferCount  uint64
	OtherTransferCount  uint64
}

type jobObjectBasicLimitInformation struct {
	PerProcessUserTimeLimit int64
	PerJobUserTimeLimit     int64
	LimitFlags              uint32
	MinimumWorkingSetSize   uintptr
	MaximumWorkingSetSize   uintptr
	ActiveProcessLimit      uint32
	Affinity                uintptr
	PriorityClass           uint32
	SchedulingClass         uint32
}

type jobObjectExtendedLimitInformationStruct struct {
	BasicLimitInformation jobObjectBasicLimitInformation
	IoInfo                ioCounters
	ProcessMemoryLimit    uintptr
	JobMemoryLimit        uintptr
	PeakProcessMemoryUsed uintptr
	PeakJobMemoryUsed     uintptr
}

type advisorRuntime struct {
	mu        sync.Mutex
	cmd       *exec.Cmd
	job       uintptr
	endpoint  string
	modelPath string
	ready     bool
	starting  bool
	lastError string
}

var advisorServer advisorRuntime

// A model the user can choose to run.
type advisorModelChoice struct {
	Label    string
	Path     string // empty means the one carried inside the app
	Embedded bool
	SizeMB   int64
}

// advisorSelectedModel is the file the user picked, remembered between runs.
// Empty means the built-in light model.
var advisorSelectedModel string

// advisorAvailableModels lists the weights present on disk.
//
// Nothing ships inside the application any more: a gigabyte in the executable
// is a gigabyte in every clone of the repository and every download by someone
// who only wanted the app. Models are fetched on first use instead.
//
// The choice does not affect print quality. A heavier model recognises more
// unusual parts, but the settings are computed here either way — the model only
// says what the object is — which is what makes the light one a real option on
// a machine with little memory rather than a downgrade.
func advisorAvailableModels() []advisorModelChoice {
	choices := []advisorModelChoice{}
	dir := advisorModelsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return choices
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".gguf") {
			continue
		}
		full := filepath.Join(dir, entry.Name())
		size := int64(0)
		if info, statErr := entry.Info(); statErr == nil {
			size = info.Size() / (1024 * 1024)
		}
		choices = append(choices, advisorModelChoice{
			Label:  strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())),
			Path:   full,
			SizeMB: size,
		})
	}
	return choices
}

var pGlobalMemoryStatusEx = kernel32.NewProc("GlobalMemoryStatusEx")

type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

// modelFitsMemory refuses weights that would not fit in the memory actually
// free. The server needs roughly the file size plus its working buffers, so the
// file has to leave real headroom, not merely fit.
func modelFitsMemory(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	status := memoryStatusEx{}
	status.Length = uint32(unsafe.Sizeof(status))
	ok, _, _ := pGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&status)))
	if ok == 0 || status.AvailPhys == 0 {
		return true // cannot tell; do not stand in the user's way
	}
	needed := uint64(float64(info.Size()) * 1.4)
	return needed < status.AvailPhys
}

// heaviestAvailableModel picks the largest set of weights the user has added,
// which is what the "heavier" button loads. Size is the honest proxy here: a
// bigger file is a bigger model, and the interface does not need to know more
// than that to offer the choice.
func heaviestAvailableModel() (advisorModelChoice, bool) {
	best := advisorModelChoice{}
	found := false
	for _, choice := range advisorAvailableModels() {
		if choice.Embedded {
			continue
		}
		if !found || choice.SizeMB > best.SizeMB {
			best, found = choice, true
		}
	}
	return best, found
}

// chooseAdvisorModel switches to a catalogue model, fetching it first if it is
// not on disk. A download already running is cancelled rather than queued: the
// user changing their mind should not mean waiting for both.
func chooseAdvisorModel(id string) {
	entry, ok := advisorCatalogEntryByID(id)
	if !ok {
		return
	}
	if state := advisorDownloadStatus(); state.Active {
		if state.Label == entry.Label {
			return // already fetching this one
		}
		cancelAdvisorDownload()
	}
	startAdvisorDownload(entry)
}

// selectAdvisorModel switches to a different set of weights, restarting the
// server so the change takes effect immediately.
func selectAdvisorModel(path string) {
	if advisorSelectedModel == path && (advisorServer.currentEndpoint() != "" || path == "") {
		return
	}
	advisorSelectedModel = path
	saveUISettings()
	shared.ResetAdvisorCache()
	advisorServer.stop()
	advisorServer.start()
	invalidateSpatial()
}

// advisorModelsDir is where the user drops the runtime and the weights.
func advisorModelsDir() string {
	base := os.Getenv("APPDATA")
	if base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "FlashFitAI", "models")
}

// findAdvisorAssets locates the llama.cpp server and a GGUF. Both are looked
// for in the models directory first, then the server on PATH, so a user who
// already has llama.cpp installed only needs to supply the weights.
func findAdvisorAssets() (server string, model string, err error) {
	dir := advisorModelsDir()
	entries, readErr := os.ReadDir(dir)
	if readErr == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := strings.ToLower(entry.Name())
			full := filepath.Join(dir, entry.Name())
			switch {
			case strings.HasSuffix(name, ".gguf") && model == "":
				model = full
			case (name == "llama-server.exe" || name == "server.exe") && server == "":
				server = full
			}
		}
	}
	// The downloader unpacks the engine into a runtime subfolder, which the
	// scan above never looked into: it only read the top level, so a runtime
	// that had just been fetched successfully was reported as missing.
	if server == "" {
		unpacked := filepath.Join(dir, "runtime", "llama-server.exe")
		if fileExists(unpacked) {
			server = unpacked
		}
	}
	if server == "" {
		if found, lookErr := exec.LookPath("llama-server.exe"); lookErr == nil {
			server = found
		}
	}
	if model == "" {
		return "", "", fmt.Errorf("nessun file .gguf in %s", dir)
	}
	if server == "" {
		return "", "", fmt.Errorf("llama-server.exe non trovato in %s né nel PATH", dir)
	}
	return server, model, nil
}

// freeLoopbackPort asks the OS for an unused port instead of guessing 8080,
// which is very often already taken.
func freeLoopbackPort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

// createKillOnCloseJob returns a job handle whose closure kills every process
// inside it. Assigning the child to it is what makes an orphan impossible.
func createKillOnCloseJob() (uintptr, error) {
	job, _, err := pCreateJobObject.Call(0, 0)
	if job == 0 {
		return 0, err
	}
	info := jobObjectExtendedLimitInformationStruct{}
	info.BasicLimitInformation.LimitFlags = limitKillOnJobClose
	ok, _, setErr := pSetInformationJobObject.Call(
		job,
		jobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		unsafe.Sizeof(info),
	)
	if ok == 0 {
		pCloseHandle.Call(job)
		return 0, setErr
	}
	return job, nil
}

func (r *advisorRuntime) status() (ready bool, starting bool, detail string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ready, r.starting, r.lastError
}

func (r *advisorRuntime) currentEndpoint() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.ready {
		return ""
	}
	return r.endpoint
}

func (r *advisorRuntime) modelName() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.modelPath == "" {
		return ""
	}
	return filepath.Base(r.modelPath)
}

// start launches the server in the background. It returns immediately; the
// interface reflects progress through status().
func (r *advisorRuntime) start() {
	r.mu.Lock()
	if r.starting || r.ready {
		r.mu.Unlock()
		return
	}
	r.starting = true
	r.lastError = ""
	r.mu.Unlock()

	go func() {
		// With an embedded payload this unpacks it on first run; without one it
		// falls back to whatever the user has placed in the models directory.
		server, model, err := findAdvisorAssets()
		if err != nil {
			r.fail(err)
			return
		}
		// A model the user picked overrides the built-in one, provided it is
		// still there and the machine can actually hold it. Loading weights
		// larger than the free memory would swap the machine to a standstill,
		// and the light model is always a working answer.
		if advisorSelectedModel != "" && fileExists(advisorSelectedModel) {
			if modelFitsMemory(advisorSelectedModel) {
				model = advisorSelectedModel
			} else {
				writeLog("advisor: modello scelto troppo grande per la memoria libera, uso quello leggero")
			}
		}
		port, err := freeLoopbackPort()
		if err != nil {
			r.fail(err)
			return
		}
		job, err := createKillOnCloseJob()
		if err != nil {
			writeLog("advisor: job object non creato: " + fmt.Sprint(err))
			job = 0
		}
		cmd := exec.Command(server,
			"-m", model,
			"--host", "127.0.0.1",
			"--port", fmt.Sprint(port),
			"-c", "2048",
			"--threads", fmt.Sprint(advisorThreadCount()),
		)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
		cmd.Stdout = io.Discard
		// The server reports its own loading progress on stderr. Reading it is
		// the only honest source for a percentage here: anything else would be
		// a guess dressed up as a measurement.
		if pipe, pipeErr := cmd.StderrPipe(); pipeErr == nil {
			go trackAdvisorLoadProgress(pipe)
		} else {
			cmd.Stderr = io.Discard
		}
		if err := cmd.Start(); err != nil {
			if job != 0 {
				pCloseHandle.Call(job)
			}
			r.fail(err)
			return
		}
		if job != 0 {
			handle, _, _ := pOpenProcess.Call(processSetQuota|processTerminate, 0, uintptr(cmd.Process.Pid))
			if handle != 0 {
				pAssignProcessToJobObj.Call(job, handle)
				pCloseHandle.Call(handle)
			}
		}

		endpoint := fmt.Sprintf("http://127.0.0.1:%d", port)
		r.mu.Lock()
		r.cmd, r.job, r.endpoint, r.modelPath = cmd, job, endpoint, model
		r.mu.Unlock()

		if err := waitForAdvisorHealth(endpoint); err != nil {
			r.fail(err)
			r.stop()
			return
		}
		r.mu.Lock()
		r.ready, r.starting = true, false
		r.mu.Unlock()
		// Only now does the engine start consulting it.
		shared.ActiveAdvisorEndpoint = endpoint
		writeLog("advisor: modello pronto (" + filepath.Base(model) + ")")
		if mainHwnd != 0 {
			pPostMessage.Call(mainHwnd, WM_ADVISOR_READY, 0, 0)
		}
	}()
}

// advisorThreadCount leaves a core free so the interface stays responsive while
// the model is thinking.
func advisorThreadCount() int {
	count := runtime.NumCPU() - 1
	if count < 1 {
		count = 1
	}
	if count > 8 {
		count = 8
	}
	return count
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// advisorLoadPercent is how far the model is through loading, or -1 when the
// server has not said. It is written by the reader below and read by the
// interface.
var (
	advisorLoadMu      sync.Mutex
	advisorLoadPercent = -1.0
)

func advisorLoadingProgress() float64 {
	advisorLoadMu.Lock()
	defer advisorLoadMu.Unlock()
	return advisorLoadPercent
}

func setAdvisorLoadProgress(percent float64) {
	advisorLoadMu.Lock()
	advisorLoadPercent = percent
	advisorLoadMu.Unlock()
	if mainHwnd != 0 {
		pPostMessage.Call(mainHwnd, WM_ADVISOR_PROGRESS, 0, 0)
	}
}

// trackAdvisorLoadProgress watches the server's own output for how far it has
// got. llama.cpp prints a percentage while it maps the weights; the exact
// wording varies between builds, so a percentage is taken from any line that
// mentions loading rather than from one fixed format.
func trackAdvisorLoadProgress(pipe io.ReadCloser) {
	defer pipe.Close()
	setAdvisorLoadProgress(0)
	scanner := bufio.NewScanner(pipe)
	scanner.Buffer(make([]byte, 0, 8192), 1<<16)
	for scanner.Scan() {
		line := strings.ToLower(scanner.Text())
		if !strings.Contains(line, "load") && !strings.Contains(line, "%") {
			continue
		}
		if percent, ok := percentInLine(line); ok {
			setAdvisorLoadProgress(percent)
		}
	}
	// Loading is over, one way or the other.
	setAdvisorLoadProgress(-1)
}

// percentInLine pulls the last percentage out of a log line.
func percentInLine(line string) (float64, bool) {
	index := strings.LastIndex(line, "%")
	if index <= 0 {
		return 0, false
	}
	start := index
	for start > 0 {
		c := line[start-1]
		if (c >= '0' && c <= '9') || c == '.' {
			start--
			continue
		}
		break
	}
	if start == index {
		return 0, false
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(line[start:index]), 64)
	if err != nil || value < 0 || value > 100 {
		return 0, false
	}
	return value, true
}

func (r *advisorRuntime) fail(err error) {
	r.mu.Lock()
	r.starting, r.ready = false, false
	r.lastError = err.Error()
	r.mu.Unlock()
	writeLog("advisor non disponibile: " + err.Error())
	if mainHwnd != 0 {
		pPostMessage.Call(mainHwnd, WM_ADVISOR_READY, 0, 0)
	}
}

// stop shuts the server down. Closing the job handle is what actually
// guarantees the child is gone even if it ignores the kill.
func (r *advisorRuntime) stop() {
	r.mu.Lock()
	cmd, job := r.cmd, r.job
	r.cmd, r.job, r.ready, r.starting = nil, 0, false, false
	shared.ActiveAdvisorEndpoint = ""
	r.mu.Unlock()

	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
		// Reap it so no zombie handle is left behind.
		go func() { _ = cmd.Wait() }()
	}
	if job != 0 {
		pCloseHandle.Call(job)
	}
}

func waitForAdvisorHealth(endpoint string) error {
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(advisorHealthTimeout)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := client.Get(endpoint + "/health")
		if err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			lastErr = fmt.Errorf("stato %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		time.Sleep(advisorHealthPoll)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("timeout")
	}
	return fmt.Errorf("il server del modello non è diventato pronto: %v", lastErr)
}
