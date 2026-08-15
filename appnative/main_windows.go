//go:build windows

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"flashfitai/shared"
)

var (
	// The one place the version is written. The window chrome, the title, the
	// --version flag and the log line at startup all read it from here, so a
	// build cannot claim one version on screen and another in its own log.
	buildVersion = "4.4.6-three-layer-safety-beta"
	appTitle     = "FlashFit AI Spatial " + buildVersion
)

const (
	className = "FlashFitAI_Spatial_MainWindow_v40"

	WM_CREATE         = 0x0001
	WM_DESTROY        = 0x0002
	WM_SIZE           = 0x0005
	WM_CLOSE          = 0x0010
	WM_COMMAND        = 0x0111
	WM_DROPFILES      = 0x0233
	WM_SETFONT        = 0x0030
	WM_LBUTTONDOWN    = 0x0201
	WM_LBUTTONDBLCLK  = 0x0203
	WM_MOUSEMOVE      = 0x0200
	CS_DBLCLKS        = 0x0008
	WM_APP            = 0x8000
	WM_DISCOVERY_DONE = WM_APP + 1
	WM_ANALYSIS_DONE  = WM_APP + 2
	WM_IMPORT_DONE    = WM_APP + 3
	WM_SHOW_TEXTURES  = WM_APP + 4
	WM_PREVIEW_DONE   = WM_APP + 5
	WM_ADVISOR_READY    = WM_APP + 6
	WM_ADVISOR_PROGRESS = WM_APP + 7

	WS_OVERLAPPEDWINDOW = 0x00CF0000
	WS_VISIBLE          = 0x10000000
	WS_CHILD            = 0x40000000
	WS_TABSTOP          = 0x00010000
	WS_BORDER           = 0x00800000
	WS_VSCROLL          = 0x00200000
	WS_CLIPCHILDREN     = 0x02000000
	WS_EX_CLIENTEDGE    = 0x00000200
	WS_EX_ACCEPTFILES   = 0x00000010

	ES_AUTOHSCROLL       = 0x0080
	ES_MULTILINE         = 0x0004
	ES_AUTOVSCROLL       = 0x0040
	ES_READONLY          = 0x0800
	LBS_NOTIFY           = 0x0001
	LBS_NOINTEGRALHEIGHT = 0x0100
	BS_PUSHBUTTON        = 0x00000000

	SW_SHOW          = 5
	SW_RESTORE       = 9
	COLOR_BTNFACE    = 15
	DEFAULT_GUI_FONT = 17
	IDC_ARROW        = 32512
	IDI_APPLICATION  = 32512

	MB_OK              = 0x00000000
	MB_OKCANCEL        = 0x00000001
	MB_ICONERROR       = 0x00000010
	MB_ICONWARNING     = 0x00000030
	MB_ICONINFORMATION = 0x00000040
	IDOK               = 1

	LB_ADDSTRING    = 0x0180
	LB_RESETCONTENT = 0x0184
	LB_SETCURSEL    = 0x0186
	LB_GETCURSEL    = 0x0188
	LB_ERR          = -1
	EN_CHANGE       = 0x0300
	LBN_SELCHANGE   = 1

	OFN_NOCHANGEDIR   = 0x00000008
	OFN_PATHMUSTEXIST = 0x00000800
	OFN_FILEMUSTEXIST = 0x00001000
	OFN_EXPLORER      = 0x00080000

	ERROR_ALREADY_EXISTS        = 183
	BELOW_NORMAL_PRIORITY_CLASS = 0x00004000
)

const (
	idQualityLow      = 1001
	idQualityBalanced = 1002
	idQualityPerfect  = 1003
	idModelEdit       = 1010
	idBrowseModel     = 1011
	idAnalyze         = 1012
	idSearch          = 1020
	idFilamentList    = 1021
	idDetect          = 1030
	idManualProfiles  = 1031
	idImport          = 1040
	idCancelImport    = 1041
)

type wndClassEx struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}
type point struct{ X, Y int32 }
type msg struct {
	Hwnd     uintptr
	Message  uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	Pt       point
	LPrivate uint32
}
type rect struct{ Left, Top, Right, Bottom int32 }
type openFileName struct {
	LStructSize       uint32
	HwndOwner         uintptr
	HInstance         uintptr
	LpstrFilter       *uint16
	LpstrCustomFilter *uint16
	NMaxCustFilter    uint32
	NFilterIndex      uint32
	LpstrFile         *uint16
	NMaxFile          uint32
	LpstrFileTitle    *uint16
	NMaxFileTitle     uint32
	LpstrInitialDir   *uint16
	LpstrTitle        *uint16
	Flags             uint32
	NFileOffset       uint16
	NFileExtension    uint16
	LpstrDefExt       *uint16
	LCustData         uintptr
	LpfnHook          uintptr
	LpTemplateName    *uint16
	PvReserved        uintptr
	DwReserved        uint32
	FlagsEx           uint32
}

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	comdlg32 = syscall.NewLazyDLL("comdlg32.dll")

	pRegisterClassEx     = user32.NewProc("RegisterClassExW")
	pCreateWindowEx      = user32.NewProc("CreateWindowExW")
	pDefWindowProc       = user32.NewProc("DefWindowProcW")
	pShowWindow          = user32.NewProc("ShowWindow")
	pUpdateWindow        = user32.NewProc("UpdateWindow")
	pGetMessage          = user32.NewProc("GetMessageW")
	pTranslateMessage    = user32.NewProc("TranslateMessage")
	pDispatchMessage     = user32.NewProc("DispatchMessageW")
	pPostQuitMessage     = user32.NewProc("PostQuitMessage")
	pMoveWindow          = user32.NewProc("MoveWindow")
	pSendMessage         = user32.NewProc("SendMessageW")
	pSetWindowText       = user32.NewProc("SetWindowTextW")
	pGetWindowText       = user32.NewProc("GetWindowTextW")
	pGetWindowTextLength = user32.NewProc("GetWindowTextLengthW")
	pEnableWindow        = user32.NewProc("EnableWindow")
	pMessageBox          = user32.NewProc("MessageBoxW")
	pPostMessage         = user32.NewProc("PostMessageW")
	pGetClientRect       = user32.NewProc("GetClientRect")
	pDestroyWindow       = user32.NewProc("DestroyWindow")
	pFindWindow          = user32.NewProc("FindWindowW")
	pSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	pIsIconic            = user32.NewProc("IsIconic")
	pLoadCursor          = user32.NewProc("LoadCursorW")
	pLoadIcon            = user32.NewProc("LoadIconW")
	pSetProcessDPI       = user32.NewProc("SetProcessDpiAwarenessContext")
	pScreenToClient      = user32.NewProc("ScreenToClient")

	pGetModuleHandle   = kernel32.NewProc("GetModuleHandleW")
	pCreateMutex       = kernel32.NewProc("CreateMutexW")
	pGetCurrentProcess = kernel32.NewProc("GetCurrentProcess")
	pSetPriorityClass  = kernel32.NewProc("SetPriorityClass")
	pGetStockObject    = gdi32.NewProc("GetStockObject")
	pDragAcceptFiles   = shell32.NewProc("DragAcceptFiles")
	pDragQueryFile     = shell32.NewProc("DragQueryFileW")
	pDragFinish        = shell32.NewProc("DragFinish")
	pGetOpenFileName   = comdlg32.NewProc("GetOpenFileNameW")
)

var (
	mainHwnd      uintptr
	appInstance   uintptr
	hFont         uintptr
	mutexHandle   uintptr
	uiCreateErr   error
	smokeMode     bool
	qaTextureMode bool

	hQualityLow, hQualityBalanced, hQualityPerfect            uintptr
	hModelEdit, hBrowseModel, hAnalyze                        uintptr
	hSearch, hFilamentList, hFilamentDetails                  uintptr
	hAnalysis, hProfiles                                      uintptr
	hDetect, hManualProfiles, hImport, hCancelImport, hStatus uintptr

	app     appState
	pending asyncState
)

type appState struct {
	filaments          []shared.Filament
	filtered           []int
	selected           int
	quality            string
	texture            string
	modelPath          string
	analysis           *shared.ModelAnalysis
	profiles           shared.DiscoveredProfiles
	slicer             string
	machine            string
	process            string
	baseFilament       string
	manualSlicer       bool
	manualMachine      bool
	manualProcess      bool
	manualBaseFilament bool
	discovering        bool
	analyzing          bool
	importing          bool
	profileMeta        map[string]profileMeta
	processChoices     map[string]string
	baseChoices        map[string]string
	printerChoices     []shared.DiscoveredMachine
	printerIndex       int
	printer            shared.PrinterProfile
	recommendation     *shared.Recommendation
	filamentMatchTotal int
	ready              bool
	statusKey          string
	statusArgs         []any
	importCancel       context.CancelFunc
	analysisCancel     context.CancelFunc
	analysisGeneration uint64
}
type asyncState struct {
	mu                 sync.Mutex
	discovery          shared.DiscoveredProfiles
	official           []shared.Filament
	notes              []string
	discoveryErr       error
	analysis           shared.ModelAnalysis
	analysisPath       string
	analysisErr        error
	analysisGeneration uint64
	importResult       shared.ImportResult
	importErr          error
	profileMeta        map[string]profileMeta
	mergedFilaments    []shared.Filament
	processChoices     map[string]string
	baseChoices        map[string]string
	previewMesh        []shared.PreviewTriangle
	previewGeneration  uint64
}

type analysisWorkerResult struct {
	Analysis shared.ModelAnalysis `json:"analysis"`
	// The analysis runs in a separate process and comes back as JSON, but both
	// path fields are json:"-" on the struct so they never leak into a saved
	// summary. That also dropped them on the way home, leaving the import with
	// no geometry copy to work from — so they are carried explicitly here.
	StoredModelPath string `json:"stored_model_path,omitempty"`
	SourcePath      string `json:"source_path,omitempty"`
	Error           string `json:"error,omitempty"`
}

type discoveryWorkerResult struct {
	Discovery       shared.DiscoveredProfiles `json:"discovery"`
	Official        []shared.Filament         `json:"official"`
	Notes           []string                  `json:"notes"`
	ProfileMeta     map[string]profileMeta    `json:"profile_meta"`
	MergedFilaments []shared.Filament         `json:"merged_filaments"`
	ProcessChoices  map[string]string         `json:"process_choices"`
	BaseChoices     map[string]string         `json:"base_choices"`
	Error           string                    `json:"error,omitempty"`
}

func runAnalysisWorker(inputPath, outputPath string) int {
	if proc, _, _ := pGetCurrentProcess.Call(); proc != 0 {
		pSetPriorityClass.Call(proc, BELOW_NORMAL_PRIORITY_CLASS)
	}
	result := analysisWorkerResult{}
	a, err := shared.AnalyzeModel(inputPath)
	if err != nil {
		result.Error = err.Error()
	} else {
		result.Analysis = a
		result.StoredModelPath = a.StoredModelPath
		result.SourcePath = a.SourcePath
	}
	b, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return 2
	}
	tmp := outputPath + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return 3
	}
	if err := os.Rename(tmp, outputPath); err != nil {
		_ = os.Remove(tmp)
		return 4
	}
	return 0
}

func writeWorkerJSON(outputPath string, value any) int {
	b, err := json.Marshal(value)
	if err != nil {
		return 2
	}
	tmp := outputPath + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return 3
	}
	if err := os.Rename(tmp, outputPath); err != nil {
		_ = os.Remove(tmp)
		return 4
	}
	return 0
}

func runDiscoveryWorker(outputPath string) int {
	if proc, _, _ := pGetCurrentProcess.Call(); proc != 0 {
		pSetPriorityClass.Call(proc, BELOW_NORMAL_PRIORITY_CLASS)
	}
	result := discoveryWorkerResult{
		ProcessChoices: make(map[string]string),
		BaseChoices:    make(map[string]string),
	}
	defer func() {
		if r := recover(); r != nil {
			result.Error = fmt.Sprintf("discovery panic: %v", r)
			_ = writeWorkerJSON(outputPath, result)
		}
	}()
	base, err := shared.LoadBuiltinFilaments()
	if err != nil {
		result.Error = err.Error()
		return writeWorkerJSON(outputPath, result)
	}
	if calibrated, calibrationErr := shared.LoadFilamentCalibrations(calibrationPath(), base); calibrationErr == nil {
		base = calibrated
	} else {
		result.Notes = append(result.Notes, "Calibrazioni bobina ignorate: "+calibrationErr.Error())
	}
	result.Discovery = shared.DiscoverProfiles()
	var scanNotes []string
	result.Official, scanNotes = shared.ScanOfficialFilaments(15000)
	result.Notes = append(result.Notes, scanNotes...)
	result.ProfileMeta = make(map[string]profileMeta, len(result.Discovery.Processes)+len(result.Discovery.Filaments))
	for _, path := range append(append([]string(nil), result.Discovery.Processes...), result.Discovery.Filaments...) {
		if _, ok := result.ProfileMeta[path]; !ok {
			result.ProfileMeta[path] = readProfileMeta(path)
		}
	}
	result.MergedFilaments = mergeFilaments(base, result.Official)
	printer, printerErr := shared.ResolvePrinterProfile(result.Discovery.Machine)
	for _, quality := range []string{"low", "balanced", "perfect"} {
		if printerErr == nil {
			result.ProcessChoices[quality] = chooseProcessForPrinter(result.Discovery.Processes, quality, printer, result.ProfileMeta)
		}
	}
	for _, material := range []string{"PLA", "PETG", "ABS", "TPU"} {
		result.BaseChoices[material] = chooseBaseFilament(result.Discovery.Filaments, shared.Filament{Material: material}, result.ProfileMeta)
	}
	return writeWorkerJSON(outputPath, result)
}

func main() {
	// Win32 window handles and their message queues belong to the thread that
	// created them. Without this pin the Go scheduler can migrate the message
	// loop onto another OS thread, where GetMessage waits on an empty queue and
	// the window stops responding entirely.
	runtime.LockOSThread()

	if len(os.Args) == 4 && os.Args[1] == "--analyze-worker" {
		os.Exit(runAnalysisWorker(os.Args[2], os.Args[3]))
	}
	if len(os.Args) == 3 && os.Args[1] == "--discover-worker" {
		os.Exit(runDiscoveryWorker(os.Args[2]))
	}
	// Before anything can fail: a GUI binary has no console, so without this
	// the runtime's report of a fatal error is written to a handle that does
	// not exist and the window just disappears.
	captureRuntimeFailures()
	// Stamped first, so every log — and every crash report inside it — says
	// which build produced it. A log without that is guesswork about which
	// version the user was actually running.
	writeLog("FlashFit AI " + buildVersion + " avviato")
	defer func() {
		if r := recover(); r != nil {
			writeLog(fmt.Sprintf("PANIC main: %v\n%s", r, debug.Stack()))
			messageBox(0, trf("internalError", logPath()), appTitle, MB_OK|MB_ICONERROR)
		}
	}()
	for _, a := range os.Args[1:] {
		switch a {
		case "--self-test":
			root := filepath.Join(os.TempDir(), "FlashFitAI-selftest")
			if err := shared.RunSelfTest(root); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			fmt.Println("self-test-ok")
			return
		case "--window-smoke":
			smokeMode = true
		case "--qa-textures":
			qaTextureMode = true
		case "--version":
			fmt.Println(buildVersion)
			return
		}
	}
	if !smokeMode && !acquireSingleInstance() {
		return
	}
	if pSetProcessDPI.Find() == nil {
		// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 = -4.
		pSetProcessDPI.Call(^uintptr(3))
	}
	if err := runGUI(); err != nil {
		writeLog("GUI startup failed: " + err.Error())
		if !smokeMode {
			messageBox(0, err.Error(), appTitle, MB_OK|MB_ICONERROR)
		}
		os.Exit(1)
	}
}

func acquireSingleInstance() bool {
	name := utf16Ptr("Local\\FlashFitAI-Spatial-SingleInstance")
	h, _, e := pCreateMutex.Call(0, 0, uintptr(unsafe.Pointer(name)))
	mutexHandle = h
	if errno, ok := e.(syscall.Errno); ok && errno == ERROR_ALREADY_EXISTS {
		cls := utf16Ptr(className)
		old, _, _ := pFindWindow.Call(uintptr(unsafe.Pointer(cls)), 0)
		if old != 0 {
			icon, _, _ := pIsIconic.Call(old)
			if icon != 0 {
				pShowWindow.Call(old, SW_RESTORE)
			}
			pSetForegroundWindow.Call(old)
		} else {
			messageBox(0, tr("alreadyRunning"), appTitle, MB_OK|MB_ICONINFORMATION)
		}
		return false
	}
	return h != 0
}

func runGUI() error {
	inst, _, _ := pGetModuleHandle.Call(0)
	appInstance = inst
	cursor, _, _ := pLoadCursor.Call(0, IDC_ARROW)
	icon, _, _ := pLoadIcon.Call(inst, 1)
	if icon == 0 {
		icon, _, _ = pLoadIcon.Call(0, IDI_APPLICATION)
	}
	cls := utf16Ptr(className)
	wc := wndClassEx{CbSize: uint32(unsafe.Sizeof(wndClassEx{})), Style: CS_DBLCLKS, LpfnWndProc: syscall.NewCallback(windowProc), HInstance: inst, HIcon: icon, HCursor: cursor, HbrBackground: COLOR_BTNFACE + 1, LpszClassName: cls, HIconSm: icon}
	atom, _, err := pRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc)))
	if atom == 0 {
		return fmt.Errorf("impossibile registrare la finestra Win32: %v", err)
	}
	title := utf16Ptr(appTitle)
	style := uintptr(WS_OVERLAPPEDWINDOW | WS_CLIPCHILDREN)
	if !smokeMode {
		style |= WS_VISIBLE
	}
	hwnd, _, err := pCreateWindowEx.Call(WS_EX_ACCEPTFILES, uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(title)), style, 72, 46, 1280, 850, 0, 0, inst, 0)
	if hwnd == 0 {
		return fmt.Errorf("impossibile creare la finestra Win32: %v", err)
	}
	mainHwnd = hwnd
	if !smokeMode {
		pShowWindow.Call(hwnd, SW_SHOW)
		pUpdateWindow.Call(hwnd)
	}
	var m msg
	for {
		r, _, e := pGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) == -1 {
			return fmt.Errorf("errore message loop: %v", e)
		}
		if r == 0 {
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
	return nil
}

func windowProc(hwnd uintptr, message uint32, wParam, lParam uintptr) (ret uintptr) {
	defer func() {
		if r := recover(); r != nil {
			writeLog(fmt.Sprintf("PANIC wndproc msg=%x: %v\n%s", message, r, debug.Stack()))
			ret, _, _ = pDefWindowProc.Call(hwnd, uintptr(message), wParam, lParam)
		}
	}()
	switch message {
	case WM_CREATE:
		mainHwnd = hwnd
		createUI(hwnd)
		if uiCreateErr != nil {
			writeLog("UI creation failed: " + uiCreateErr.Error())
			return ^uintptr(0)
		}
		pDragAcceptFiles.Call(hwnd, 1)
		if smokeMode {
			pPostMessage.Call(hwnd, WM_CLOSE, 0, 0)
			return 0
		}
		loadInitialCatalog()
		startDiscovery()
		// The model server comes up alongside discovery. It is slow to load, so
		// it must never block the window appearing.
		//
		// Answers arrive on a background goroutine; posting a message is how
		// they reach the UI thread, which owns every repaint.
		shared.AdvisorNotify = func() {
			if mainHwnd != 0 {
				pPostMessage.Call(mainHwnd, WM_ADVISOR_READY, 0, 0)
			}
		}
		advisorServer.start()
		if qaTextureMode {
			pPostMessage.Call(hwnd, WM_SHOW_TEXTURES, 0, 0)
		}
		return 0
	case WM_SIZE:
		layoutUI(hwnd)
		if wParam == SIZE_MINIMIZED {
			stopSpatialAnimation(hwnd)
		} else if !spatialResizing {
			startSpatialAnimation(hwnd)
		}
		return 0
	case WM_ENTERSIZEMOVE:
		spatialResizing = true
		stopSpatialAnimation(hwnd)
		return 0
	case WM_EXITSIZEMOVE:
		spatialResizing = false
		mainSpatialBuffer.reset()
		cleanupSpatialMaterialSystem()
		spatialViewportWidth, spatialViewportHeight = 0, 0
		startSpatialAnimation(hwnd)
		invalidateSpatial()
		return 0
	case WM_PAINT:
		paintSpatialUI(hwnd)
		return 0
	case WM_TIMER:
		if wParam == idSpatialAnimation && spatialAnimationActive(hwnd) {
			// Everything advances on real elapsed time, so effects keep their
			// rate even while the window is busy painting something expensive.
			dt := animClock.tick()
			spatialAnimationTick++
			refreshHoverFromCursor(hwnd)
			advancePlateSpin(dt)
			// Hover changes touch chrome all over the window, so those need a
			// full frame. Continuous effects live in the stage and get a
			// stage-only frame, which is what keeps 60 Hz affordable.
			// Two cadences on one timer. Hover runs at the full 60 Hz because
			// pointer feedback is felt directly; the drifting envelope runs at
			// half that, which is indistinguishable on motion this slow and
			// halves the cost of the only continuously animating region.
			// Tracks advance at the full 60 Hz so the easing keeps its shape,
			// but a frame is only issued every other tick. A hover change still
			// repaints the whole window — the chrome it affects is spread all
			// over it — and at 60 Hz that redraw dominated the CPU.
			hoverMoved := animateHover(dt)
			if spatialAnimationTick%2 != 0 {
				return 0
			}
			switch {
			case hoverMoved || !hoverSettled():
				// Hover never changes the canvas, so the canvas is not redrawn.
				invalidateChrome()
			case sceneNeedsAnimationFrame():
				invalidateStageOnly()
			}
		}
		return 0
	case WM_ERASEBKGND:
		return 1
	case WM_LBUTTONDOWN:
		x, y := pointFromLParam(lParam)
		pressedRegion = regionAt(x, y)
		if contains(spatial.stage, x, y) {
			stageBeginDrag(x, y)
		}
		invalidateSpatial()
		return 0
	case WM_MOUSEMOVE:
		x, y := pointFromLParam(lParam)
		hoveredRegion = regionAt(x, y)
		stageDragTo(x, y)
		return 0
	case WM_LBUTTONDBLCLK:
		x, y := pointFromLParam(lParam)
		if contains(spatial.stage, x, y) {
			resetStageCamera()
			invalidateSpatial()
		}
		return 0
	case WM_MOUSEWHEEL:
		var pt point
		pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
		pScreenToClient.Call(hwnd, uintptr(unsafe.Pointer(&pt)))
		if contains(spatial.stage, pt.X, pt.Y) {
			if int16((wParam>>16)&0xffff) > 0 {
				stageZoomBy(1)
			} else {
				stageZoomBy(-1)
			}
		}
		return 0
	case WM_LBUTTONUP:
		x, y := pointFromLParam(lParam)
		pressedRegion = hoverNone
		if endStageDrag() {
			// The gesture was an orbit, not a click on the stage.
			return 0
		}
		spatialClick(x, y)
		return 0
	case WM_COMMAND:
		id := int(wParam & 0xffff)
		notify := int((wParam >> 16) & 0xffff)
		handleCommand(id, notify)
		return 0
	case WM_DROPFILES:
		handleDrop(wParam)
		return 0
	case WM_DISCOVERY_DONE:
		finishDiscovery()
		return 0
	case WM_ANALYSIS_DONE:
		finishAnalysis()
		return 0
	case WM_IMPORT_DONE:
		finishImport()
		return 0
	case WM_SHOW_TEXTURES:
		setQuality("perfect")
		return 0
	case WM_ADVISOR_PROGRESS:
		// Only the toolbar changed, so the canvas keeps what it has.
		invalidateChrome()
		return 0
	case WM_ADVISOR_READY:
		if failure := shared.AdvisorPanicLog; failure != "" {
			writeLog("PANIC " + failure)
			shared.AdvisorPanicLog = ""
		}
		// Whatever the outcome, the inspector now has something new to say.
		renderAnalysis()
		invalidateSpatial()
		return 0
	case WM_PREVIEW_DONE:
		finishPreviewMeshLoad()
		return 0
	case WM_CLOSE:
		if app.importCancel != nil {
			app.importCancel()
		}
		if app.analysisCancel != nil {
			app.analysisCancel()
		}
		pDestroyWindow.Call(hwnd)
		return 0
	case WM_DESTROY:
		// Stop the model server before we go. The job object would kill it
		// anyway, but shutting it down explicitly keeps the exit clean.
		advisorServer.stop()
		cleanupSpatialUI()
		pPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := pDefWindowProc.Call(hwnd, uintptr(message), wParam, lParam)
	return r
}

func createUI(parent uintptr) {
	initSpatialUI(parent)
	app.quality = "balanced"
	app.texture = "satin"
	app.selected = -1
	invalidateSpatial()
}

var staticLabels []uintptr

func label(parent uintptr, text string, id int) uintptr {
	h := control(parent, "STATIC", text, WS_CHILD|WS_VISIBLE, 0, id)
	staticLabels = append(staticLabels, h)
	return h
}

func control(parent uintptr, class, text string, style, ex uintptr, id int) uintptr {
	c, t := utf16Ptr(class), utf16Ptr(text)
	h, _, callErr := pCreateWindowEx.Call(ex, uintptr(unsafe.Pointer(c)), uintptr(unsafe.Pointer(t)), style, 0, 0, 10, 10, parent, uintptr(id), appInstance, 0)
	if h == 0 && uiCreateErr == nil {
		uiCreateErr = fmt.Errorf("controllo Win32 %s non creato: %v", class, callErr)
	}
	if h != 0 && hFont != 0 {
		pSendMessage.Call(h, WM_SETFONT, hFont, 1)
	}
	return h
}

func layoutUI(hwnd uintptr) {
	invalidateSpatial()
}
func move(hwnd uintptr, x, y, w, h int) {
	if hwnd != 0 {
		pMoveWindow.Call(hwnd, uintptr(x), uintptr(y), uintptr(w), uintptr(h), 1)
	}
}

func handleCommand(id, notify int) {
	switch id {
	case idQualityLow:
		if notify == 0 {
			setQuality("low")
		}
	case idQualityBalanced:
		if notify == 0 {
			setQuality("balanced")
		}
	case idQualityPerfect:
		if notify == 0 {
			setQuality("perfect")
		}
	case idBrowseModel:
		if notify == 0 {
			chooseAndSetModel()
		}
	case idAnalyze:
		if notify == 0 && app.modelPath != "" {
			startAnalyze(app.modelPath)
		}
	case idSearch:
		if notify == EN_CHANGE {
			refreshFilamentList()
		}
	case idFilamentList:
		if notify == LBN_SELCHANGE {
			selectFilamentFromList()
		}
	case idDetect:
		if notify == 0 {
			startDiscovery()
		}
	case idManualProfiles:
		if notify == 0 {
			configureProfilesManually()
		}
	case idImport:
		if notify == 0 {
			startImport()
		}
	case idCancelImport:
		if notify == 0 {
			if app.importCancel != nil {
				setStatusKey("statusCancelImport")
				app.importCancel()
				enable(hCancelImport, false)
				return
			}
			if app.analysisCancel != nil {
				setStatusKey("statusCancelAnalysis")
				app.analysisCancel()
				enable(hCancelImport, false)
			}
		}
	}
}

func setQuality(q string) {
	if app.importing {
		return
	}
	app.quality = q
	if !app.manualProcess {
		app.process = app.processChoices[q]
	}
	renderAnalysis()
	renderProfiles()
	refreshReady()
	invalidateSpatial()
	if q == "perfect" {
		showTexturePicker()
	}
}

func loadInitialCatalog() {
	fs, err := shared.LoadBuiltinFilaments()
	if err != nil {
		messageBox(mainHwnd, trf("databaseError", localizeEngineText(err.Error())), appTitle, MB_OK|MB_ICONERROR)
		return
	}
	if calibrated, calibrationErr := shared.LoadFilamentCalibrations(calibrationPath(), fs); calibrationErr == nil {
		fs = calibrated
	} else {
		writeLog("CALIBRATION ERROR: " + calibrationErr.Error())
	}
	app.filaments = fs
	app.selected = -1
	refreshFilamentList()
	setStatusKey("statusCatalog", len(fs))
}
func startDiscovery() {
	if app.discovering || app.importing {
		return
	}
	app.discovering = true
	enable(hDetect, false)
	setStatusKey("statusDiscovering")
	go func() {
		var d shared.DiscoveredProfiles
		var official []shared.Filament
		var notes []string
		var runErr error
		var meta map[string]profileMeta
		var merged []shared.Filament
		var processChoices map[string]string
		var baseChoices map[string]string
		defer func() {
			if r := recover(); r != nil {
				runErr = fmt.Errorf("errore interno durante il rilevamento: %v", r)
				writeLog(fmt.Sprintf("PANIC discovery: %v\n%s", r, debug.Stack()))
			}
			pending.mu.Lock()
			pending.discovery, pending.official, pending.notes, pending.discoveryErr = d, official, notes, runErr
			pending.profileMeta, pending.mergedFilaments = meta, merged
			pending.processChoices, pending.baseChoices = processChoices, baseChoices
			pending.mu.Unlock()
			pPostMessage.Call(mainHwnd, WM_DISCOVERY_DONE, 0, 0)
		}()
		exe, err := os.Executable()
		if err != nil {
			runErr = err
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
		defer cancel()
		resultFile := filepath.Join(os.TempDir(), fmt.Sprintf("flashfit-discovery-%d.json", os.Getpid()))
		defer os.Remove(resultFile)
		cmd := exec.CommandContext(ctx, exe, "--discover-worker", resultFile)
		out, err := cmd.CombinedOutput()
		if ctx.Err() != nil {
			runErr = errors.New("profile discovery timed out after 75 seconds")
			return
		}
		if err != nil {
			runErr = fmt.Errorf("discovery worker failed: %v (%s)", err, strings.TrimSpace(string(out)))
			return
		}
		data, err := os.ReadFile(resultFile)
		if err != nil {
			runErr = err
			return
		}
		var result discoveryWorkerResult
		if err := json.Unmarshal(data, &result); err != nil {
			runErr = err
			return
		}
		if result.Error != "" {
			runErr = errors.New(result.Error)
		}
		d, official, notes = result.Discovery, result.Official, result.Notes
		meta, merged = result.ProfileMeta, result.MergedFilaments
		processChoices, baseChoices = result.ProcessChoices, result.BaseChoices
	}()
}
func finishDiscovery() {
	pending.mu.Lock()
	d := pending.discovery
	official := append([]shared.Filament(nil), pending.official...)
	notes := append([]string(nil), pending.notes...)
	runErr := pending.discoveryErr
	meta := pending.profileMeta
	merged := append([]shared.Filament(nil), pending.mergedFilaments...)
	processChoices := pending.processChoices
	baseChoices := pending.baseChoices
	pending.mu.Unlock()
	previousPrinterID := app.printer.ID
	app.discovering = false
	enable(hDetect, true)
	app.profiles = d
	app.profileMeta = meta
	app.processChoices = processChoices
	app.baseChoices = baseChoices
	app.printerChoices = append([]shared.DiscoveredMachine(nil), d.Machines...)
	app.printerIndex = -1
	if !app.manualSlicer {
		app.slicer = d.SlicerExe
	}
	if !app.manualMachine {
		app.machine = d.Machine
		if printer, err := shared.ResolvePrinterProfile(d.Machine); err == nil {
			app.printer = printer
		}
		if previousPrinterID != "" && previousPrinterID != app.printer.ID {
			for i, choice := range app.printerChoices {
				if choice.PrinterID == previousPrinterID {
					app.machine = choice.Path
					app.printerIndex = i
					if printer, err := shared.ResolvePrinterProfile(choice.Path); err == nil {
						app.printer = printer
						app.processChoices = make(map[string]string, 3)
						for _, quality := range []string{"low", "balanced", "perfect"} {
							app.processChoices[quality] = chooseProcessForPrinter(d.Processes, quality, printer, meta)
						}
					}
					if !app.manualSlicer && choice.SlicerExe != "" {
						app.slicer = choice.SlicerExe
					}
					break
				}
			}
		}
		for i, choice := range app.printerChoices {
			if filepath.Clean(choice.Path) == filepath.Clean(d.Machine) {
				app.printerIndex = i
				if !app.manualSlicer && choice.SlicerExe != "" {
					app.slicer = choice.SlicerExe
				}
				break
			}
		}
	}
	if len(merged) > 0 {
		app.filaments = merged
	} else {
		app.filaments = mergeFilaments(app.filaments, official)
	}
	refreshFilamentList()
	autoSelectProfiles()
	renderProfiles()
	refreshReady()
	if len(notes) > 0 {
		writeLog(strings.Join(notes, " • "))
	}
	if runErr != nil {
		writeLog("DISCOVERY ERROR: " + runErr.Error())
	}
	setStatusKey("statusDiscoveryDone", len(app.filaments), len(d.Processes))
}

func refreshFilamentList() {
	query := ""
	if hSearch != 0 {
		query = getText(hSearch)
	}
	oldKey := ""
	if app.selected >= 0 && app.selected < len(app.filaments) {
		oldKey = filKey(app.filaments[app.selected])
	}
	app.filtered, app.filamentMatchTotal = visibleFilamentMatches(app.filaments, query)
	if hFilamentList != 0 {
		pSendMessage.Call(hFilamentList, LB_RESETCONTENT, 0, 0)
	}
	selectedRow := -1
	for row, idx := range app.filtered {
		f := app.filaments[idx]
		origin := tr("internal")
		if f.OfficialProfile {
			origin = "Flash Studio"
		}
		line := fmt.Sprintf("%s • %s • %s  [%s]", f.Brand, f.Product, f.Material, origin)
		u := utf16Ptr(line)
		if hFilamentList != 0 {
			pSendMessage.Call(hFilamentList, LB_ADDSTRING, 0, uintptr(unsafe.Pointer(u)))
		}
		if oldKey != "" && filKey(f) == oldKey {
			selectedRow = row
		}
	}
	if selectedRow < 0 && len(app.filtered) > 0 {
		selectedRow = 0
	}
	if selectedRow >= 0 {
		if hFilamentList != 0 {
			pSendMessage.Call(hFilamentList, LB_SETCURSEL, uintptr(selectedRow), 0)
		}
		app.selected = app.filtered[selectedRow]
	} else {
		app.selected = -1
	}
	updateFilamentDetails()
	ensureSelectedFilamentVisible()
	autoSelectProfiles()
	renderAnalysis()
	renderProfiles()
	refreshReady()
	invalidateSpatial()
}
func filKey(f shared.Filament) string {
	return strings.ToLower(f.Brand + "|" + f.Product + "|" + f.Material + "|" + f.Variant + "|" + f.SourcePath)
}
func selectFilamentFromList() {
	r, _, _ := pSendMessage.Call(hFilamentList, LB_GETCURSEL, 0, 0)
	row := int(int32(r))
	if row < 0 || row >= len(app.filtered) {
		return
	}
	app.selected = app.filtered[row]
	app.manualBaseFilament = false
	updateFilamentDetails()
	autoSelectProfiles()
	renderAnalysis()
	renderProfiles()
	refreshReady()
}
func selectedFilament() (shared.Filament, bool) {
	if app.selected < 0 || app.selected >= len(app.filaments) {
		return shared.Filament{}, false
	}
	return app.filaments[app.selected], true
}
func updateFilamentDetails() {
	f, ok := selectedFilament()
	if !ok {
		setText(hFilamentDetails, tr("noFilament"))
		if hFilamentDialog != 0 {
			pInvalidateRect.Call(hFilamentDialog, 0, 0)
		}
		return
	}
	pa := tr("fromBase")
	if f.PressureAdvance != nil {
		pa = fmt.Sprintf("%.3f", *f.PressureAdvance)
	}
	source := f.Source
	if f.OfficialProfile {
		source = trf("localProfile", f.SourcePath)
	}
	calibration := tr("calibrationBaseline")
	if f.MeasuredCalibration {
		calibration = tr("calibrationMeasured")
	}
	drying := "—"
	if f.DryTemperature > 0 && f.DryHours > 0 {
		drying = fmt.Sprintf("%.0f °C / %.0f h", f.DryTemperature, f.DryHours)
	}
	details := fmt.Sprintf("%s %s\r\n%s: %s • %s: %s\r\n%s: %.0f °C (%.0f–%.0f) • %s: %.0f °C\r\nMVS: %.1f mm³/s • Flow: %.3f • PA: %s\r\n%s: %s • %s: %s\r\n%s: %s\r\n%s: %s", f.Brand, f.Product, tr("material"), f.Material, tr("variant"), f.Variant, tr("nozzleTemp"), f.NozzleDefault, f.NozzleMin, f.NozzleMax, tr("bedTemp"), f.BedDefault, f.MaxVolumetricSpeed, f.FlowRatio, pa, tr("calibration"), calibration, tr("drying"), drying, tr("reliability"), f.Confidence, tr("source"), source)
	if app.filamentMatchTotal > len(app.filtered) {
		details += "\r\n\r\n" + trf("shownResults", len(app.filtered), app.filamentMatchTotal)
	}
	setText(hFilamentDetails, details)
	if hFilamentDialog != 0 {
		pInvalidateRect.Call(hFilamentDialog, 0, 0)
	}
}

func setModelPath(path string) {
	if app.importing {
		messageBox(mainHwnd, tr("waitImport"), appTitle, MB_OK|MB_ICONWARNING)
		return
	}
	path = filepath.Clean(strings.TrimSpace(path))
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".stl" && ext != ".obj" && ext != ".3mf" {
		messageBox(mainHwnd, tr("unsupportedFormat"), appTitle, MB_OK|MB_ICONWARNING)
		return
	}
	st, statErr := os.Stat(path)
	if statErr != nil || st.IsDir() || st.Size() <= 0 {
		detail := ""
		if statErr != nil {
			detail = trf("windowsDetail", statErr.Error())
		}
		messageBox(mainHwnd, trf("modelMissing", path, detail), tr("modelUnavailable"), MB_OK|MB_ICONERROR)
		return
	}
	if app.analysisCancel != nil {
		app.analysisCancel()
		app.analysisCancel = nil
		app.analyzing = false
	}
	app.modelPath = path
	app.analysis = nil
	renderAnalysis()
	refreshReady()
	invalidateSpatial()
	startAnalyze(path)
}

func startAnalyze(path string) {
	if app.importing || strings.TrimSpace(path) == "" {
		return
	}
	if app.analysisCancel != nil {
		app.analysisCancel()
	}
	app.analysisGeneration++
	generation := app.analysisGeneration
	ctx, cancel := context.WithTimeout(context.Background(), 135*time.Second)
	app.analysisCancel = cancel
	app.analyzing = true
	enable(hAnalyze, false)
	enable(hBrowseModel, true)
	enable(hCancelImport, true)
	setText(hCancelImport, tr("cancelAnalysis"))
	setStatusKey("statusAnalyzing")
	go func(p string, gen uint64, runCtx context.Context) {
		var a shared.ModelAnalysis
		var e error
		resultFile := filepath.Join(os.TempDir(), fmt.Sprintf("flashfit-analysis-%d-%d.json", os.Getpid(), gen))
		defer os.Remove(resultFile)
		defer func() {
			if r := recover(); r != nil {
				e = fmt.Errorf("errore interno durante l’analisi: %v", r)
				writeLog(fmt.Sprintf("PANIC analysis supervisor: %v\n%s", r, debug.Stack()))
			}
			pending.mu.Lock()
			pending.analysis, pending.analysisErr, pending.analysisPath, pending.analysisGeneration = a, e, p, gen
			pending.mu.Unlock()
			pPostMessage.Call(mainHwnd, WM_ANALYSIS_DONE, 0, 0)
		}()
		exe, exeErr := os.Executable()
		if exeErr != nil {
			e = fmt.Errorf("eseguibile FlashFit non individuabile: %w", exeErr)
			return
		}
		cmd := exec.CommandContext(runCtx, exe, "--analyze-worker", p, resultFile)
		out, runErr := cmd.CombinedOutput()
		if runCtx.Err() != nil {
			if errors.Is(runCtx.Err(), context.Canceled) {
				e = errors.New("analisi annullata")
			} else {
				e = errors.New("analisi interrotta dopo 2 minuti e 15 secondi")
			}
			return
		}
		if runErr != nil {
			e = fmt.Errorf("processo di analisi terminato con errore: %v (%s)", runErr, strings.TrimSpace(string(out)))
			return
		}
		data, readErr := os.ReadFile(resultFile)
		if readErr != nil {
			e = fmt.Errorf("risultato analisi non leggibile: %w", readErr)
			return
		}
		var wr analysisWorkerResult
		if jsonErr := json.Unmarshal(data, &wr); jsonErr != nil {
			e = fmt.Errorf("risultato analisi non valido: %w", jsonErr)
			return
		}
		if wr.Error != "" {
			e = errors.New(wr.Error)
			return
		}
		a = wr.Analysis
		// Restore what json:"-" stripped on the way across the process boundary.
		a.StoredModelPath = wr.StoredModelPath
		a.SourcePath = wr.SourcePath
	}(path, generation, ctx)
}

func finishAnalysis() {
	pending.mu.Lock()
	a, e, p, generation := pending.analysis, pending.analysisErr, pending.analysisPath, pending.analysisGeneration
	pending.mu.Unlock()
	if generation != app.analysisGeneration {
		return
	}
	if app.analysisCancel != nil {
		app.analysisCancel()
		app.analysisCancel = nil
	}
	app.analyzing = false
	enable(hAnalyze, true)
	enable(hBrowseModel, true)
	if !app.importing {
		enable(hCancelImport, false)
		setText(hCancelImport, tr("cancel"))
	}
	if filepath.Clean(p) != filepath.Clean(app.modelPath) {
		return
	}
	if e != nil {
		app.analysis = nil
		if e.Error() == "analisi annullata" {
			setStatusKey("statusAnalysisCanceled")
		} else {
			localized := localizeEngineText(e.Error())
			setStatusKey("statusAnalysisFailed", e.Error())
			messageBox(mainHwnd, localized, tr("modelRejected"), MB_OK|MB_ICONERROR)
		}
	} else {
		app.analysis = &a
		startPreviewMeshLoad(a, generation)
		if ve := shared.ValidateAnalysis(a); ve != nil {
			setStatusKey("statusAnalysisBlocked", ve.Error())
		} else {
			setStatusKey("statusAnalysisReady")
		}
	}
	renderAnalysis()
	refreshReady()
	invalidateSpatial()
}

// The stage shows the geometry the analysis accepted. Parsing runs off the UI
// thread; a stale generation is discarded so a fast second import always wins.
func startPreviewMeshLoad(a shared.ModelAnalysis, generation uint64) {
	source := a.StoredModelPath
	if source == "" {
		source = a.SourcePath
	}
	if source == "" {
		return
	}
	go func() {
		var tris []shared.PreviewTriangle
		// The window is told the load finished whatever happened, including a
		// panic: leaving it waiting on a message that never arrives would trade
		// a crash for a stage stuck on the placeholder.
		defer func() {
			pending.mu.Lock()
			pending.previewMesh, pending.previewGeneration = tris, generation
			pending.mu.Unlock()
			pPostMessage.Call(mainHwnd, WM_PREVIEW_DONE, 0, 0)
		}()
		// Geometry work on a file the code has never seen. A malformed mesh
		// that trips a bounds check here used to take the whole window with it.
		guard("preview mesh", func() {
			loaded, err := shared.LoadPreviewMesh(source, stagePreviewTriangleBudget)
			if err != nil {
				writeLog("preview mesh unavailable: " + err.Error())
				return
			}
			tris = loaded
		})
	}()
}

func finishPreviewMeshLoad() {
	pending.mu.Lock()
	tris, generation := pending.previewMesh, pending.previewGeneration
	pending.mu.Unlock()
	if generation != app.analysisGeneration || len(tris) == 0 {
		return
	}
	setStagePreviewMesh(tris)
}

func renderAnalysis() {
	updateRecommendation()
	if app.analysis == nil {
		setText(hAnalysis, "Importa o trascina un file STL, OBJ o 3MF.\r\n\r\nFlashFit non modifica il file originale. Gli OBJ restano invariati; dai 3MF vengono rimosse le impostazioni nascoste conservando geometria e trasformazioni.")
		return
	}
	a := *app.analysis
	status := "VALIDO"
	if e := shared.ValidateAnalysis(a); e != nil {
		status = "BLOCCATO: " + e.Error()
	} else if printer, ok := selectedPrinter(); ok {
		if e := shared.ValidateModelForPrinter(a, printer); e != nil {
			status = "BLOCCATO: " + e.Error()
		}
	}
	lines := []string{fmt.Sprintf("Modello: %s", a.Filename), fmt.Sprintf("Formato: %s • Oggetti/istanze: %d • Geometria ripulita: %t", a.InputFormat, a.ObjectCount, a.Sanitized), fmt.Sprintf("Stato: %s", status), fmt.Sprintf("Dimensioni: %.2f × %.2f × %.2f mm", a.Extents[0], a.Extents[1], a.Extents[2]), fmt.Sprintf("Triangoli: %d • Categoria: %s", a.TriangleCount, a.Category), fmt.Sprintf("Mesh chiusa: %t • Facce degeneri: %d", a.Watertight, a.DegenerateFaces), fmt.Sprintf("Sbalzi stimati: %.1f%% • Supporti: %t • Brim: %t", a.OverhangRatio*100, a.SupportSuggested, a.BrimSuggested)}
	if f, ok := selectedFilament(); ok {
		if printer, printerOK := selectedPrinter(); printerOK {
			if r, e := shared.RecommendForPrinterWithTexture(a, f, printer, app.quality, app.texture); e == nil {
				c := r.CriticalValues
				lines = append(lines, "", "LIMITI FLASHFIT", fmt.Sprintf("Layer %.2f mm • Parete esterna %.0f mm/s • Interna %.0f mm/s", c["layer_height"], c["outer_wall_speed"], c["inner_wall_speed"]), fmt.Sprintf("Infill %.0f mm/s • Ponti %.0f mm/s • Accelerazione esterna %.0f mm/s²", c["infill_speed"], c["bridge_speed"], c["outer_acceleration"]), fmt.Sprintf("MVS %.1f mm³/s • Ugello %.0f °C • Piano %.0f °C", c["max_volumetric_speed"], c["nozzle_temperature"], c["bed_temperature"]), "", strings.Join(r.Reasons, "\r\n"))
			}
		}
	}
	if len(a.Warnings) > 0 {
		lines = append(lines, "", "AVVISI", strings.Join(a.Warnings, "\r\n"))
	}
	lines = append(lines, "", "Prima di stampare controlla sempre l’anteprima layer nello slicer.")
	setText(hAnalysis, strings.Join(lines, "\r\n"))
}

// The inspector shows the guarded settings the slicer will actually receive, so
// the recommendation is kept on the state instead of being recomputed per paint.
func updateRecommendation() {
	app.recommendation = nil
	if app.analysis == nil {
		return
	}
	f, ok := selectedFilament()
	if !ok {
		return
	}
	printer, printerOK := selectedPrinter()
	if !printerOK {
		return
	}
	r, err := shared.RecommendForPrinterWithTexture(*app.analysis, f, printer, app.quality, app.texture)
	if err != nil {
		return
	}
	// S.O.G runs here, not only at import.
	//
	// It used to run inside PrepareImport alone, which meant the panel showed
	// the profile *before* its corrections and the slicer received the one
	// after: different speeds, a different estimate, and no way to tell from
	// the window which set was real. Securing the profile as it is computed
	// makes what is on screen the thing that will be printed — and gives the
	// checks panel something to report before the user has committed to
	// anything. Import recomputes from scratch and secures once, so nothing is
	// corrected twice.
	shared.LastSOGVerdict = shared.SecureProfile(&r, *app.analysis, f, printer)
	app.recommendation = &r
}

func autoSelectProfiles() {
	if !app.manualProcess {
		app.process = app.processChoices[app.quality]
	}
	if !app.manualBaseFilament {
		if f, ok := selectedFilament(); ok {
			if f.OfficialProfile && f.SourcePath != "" {
				app.baseFilament = f.SourcePath
			} else {
				app.baseFilament = app.baseChoices[materialFamily(f.Material)]
			}
		} else {
			app.baseFilament = ""
		}
	}
}

func selectedPrinter() (shared.PrinterProfile, bool) {
	if strings.TrimSpace(app.printer.ID) != "" {
		return app.printer, true
	}
	if strings.TrimSpace(app.machine) == "" {
		return shared.PrinterProfile{}, false
	}
	printer, err := shared.ResolvePrinterProfile(app.machine)
	if err == nil {
		app.printer = printer
	}
	return printer, err == nil
}

func selectedPrinterLabel() string {
	if printer, ok := selectedPrinter(); ok {
		return printer.Brand + " " + printer.Model
	}
	return tr("device")
}

func selectDiscoveredMachine(index int) {
	if app.importing || index < 0 || index >= len(app.printerChoices) {
		return
	}
	choice := app.printerChoices[index]
	printer, err := shared.ResolvePrinterProfile(choice.Path)
	if err != nil {
		messageBox(mainHwnd, localizeEngineText(err.Error()), tr("profilesMissing"), MB_OK|MB_ICONERROR)
		return
	}
	app.printerIndex = index
	app.machine = choice.Path
	app.printer = printer
	app.manualMachine = false
	if !app.manualSlicer && choice.SlicerExe != "" {
		app.slicer = choice.SlicerExe
	}
	app.processChoices = make(map[string]string, 3)
	for _, quality := range []string{"low", "balanced", "perfect"} {
		app.processChoices[quality] = chooseProcessForPrinter(app.profiles.Processes, quality, printer, app.profileMeta)
	}
	app.manualProcess = false
	autoSelectProfiles()
	renderAnalysis()
	renderProfiles()
	refreshReady()
	setStatusKey("statusPrinterSelected", choice.Label+" "+formatNozzleMM(printer.NozzleDiameter))
	invalidateSpatial()
}
func renderProfiles() {
	invalidateSpatial()
}
func configureProfilesManually() {
	if app.importing {
		return
	}
	if messageBox(mainHwnd, tr("manualIntro"), tr("manualTitle"), MB_OKCANCEL|MB_ICONINFORMATION) != IDOK {
		return
	}
	if p := chooseFile(tr("chooseSlicer"), tr("programs")+" (*.exe)\x00*.exe\x00\x00", "exe"); p != "" {
		app.slicer = p
		app.manualSlicer = true
	}
	if p := chooseFile(tr("chooseMachine"), tr("jsonProfiles")+" (*.json)\x00*.json\x00\x00", "json"); p != "" {
		app.machine = p
		app.manualMachine = true
		if printer, err := shared.ResolvePrinterProfile(p); err == nil {
			app.printer = printer
		} else {
			app.printer = shared.PrinterProfile{}
		}
	}
	if p := chooseFile(tr("chooseProcess"), tr("jsonProfiles")+" (*.json)\x00*.json\x00\x00", "json"); p != "" {
		app.process = p
		app.manualProcess = true
	}
	if p := chooseFile(tr("chooseBaseFilament"), tr("jsonProfiles")+" (*.json)\x00*.json\x00\x00", "json"); p != "" {
		app.baseFilament = p
		app.manualBaseFilament = true
	}
	renderProfiles()
	refreshReady()
}

func refreshReady() {
	ready := !app.importing && !app.analyzing && app.analysis != nil && shared.ValidateAnalysis(*app.analysis) == nil && app.slicer != "" && app.machine != "" && app.process != "" && app.baseFilament != ""
	if ready {
		printer, ok := selectedPrinter()
		if !ok || shared.ValidateModelForPrinter(*app.analysis, printer) != nil {
			ready = false
		}
	}
	if _, ok := selectedFilament(); !ok {
		ready = false
	}
	app.ready = ready
	enable(hImport, ready)
	invalidateSpatial()
}
func startImport() {
	if app.importing {
		return
	}
	if app.analysis == nil {
		messageBox(mainHwnd, tr("analyzeFirst"), appTitle, MB_OK|MB_ICONWARNING)
		return
	}
	f, ok := selectedFilament()
	if !ok {
		return
	}
	if app.slicer == "" || app.machine == "" || app.process == "" || app.baseFilament == "" {
		messageBox(mainHwnd, tr("profilesMissing"), appTitle, MB_OK|MB_ICONWARNING)
		return
	}
	a := *app.analysis
	if e := shared.ValidateAnalysis(a); e != nil {
		messageBox(mainHwnd, localizeEngineText(e.Error()), tr("importBlocked"), MB_OK|MB_ICONERROR)
		return
	}
	app.importing = true
	ctx, cancel := context.WithCancel(context.Background())
	app.importCancel = cancel
	setBusy(true)
	setStatusKey("statusImporting")
	out := defaultOutputDir()
	req := shared.ImportRequest{Model: a, Filament: f, Quality: app.quality, Texture: app.texture, SlicerExe: app.slicer, Machine: app.machine, BaseProcess: app.process, BaseFilament: app.baseFilament, OutputDir: out}
	go func(runCtx context.Context) {
		var r shared.ImportResult
		var e error
		defer func() {
			if x := recover(); x != nil {
				e = fmt.Errorf("errore interno durante l’importazione: %v", x)
				writeLog(fmt.Sprintf("PANIC import: %v\n%s", x, debug.Stack()))
			}
			pending.mu.Lock()
			pending.importResult, pending.importErr = r, e
			pending.mu.Unlock()
			pPostMessage.Call(mainHwnd, WM_IMPORT_DONE, 0, 0)
		}()
		r, e = shared.BuildAndOpenContext(runCtx, req)
	}(ctx)
}
func finishImport() {
	pending.mu.Lock()
	r, e := pending.importResult, pending.importErr
	pending.mu.Unlock()
	app.importing = false
	app.importCancel = nil
	setBusy(false)
	refreshReady()
	if e != nil {
		writeLog("IMPORT ERROR: " + e.Error())
		localized := localizeEngineText(e.Error())
		setStatusKey("statusImportCanceled", e.Error())
		messageBox(mainHwnd, trf("importCanceledBody", localized), tr("importCanceledTitle"), MB_OK|MB_ICONERROR)
		return
	}
	setStatusKey("statusImportDone", r.ProjectPath)
	messageBox(mainHwnd, trf("importDoneBody", r.ProjectPath), tr("importDoneTitle"), MB_OK|MB_ICONINFORMATION)
}
func setBusy(b bool) {
	for _, h := range []uintptr{hQualityLow, hQualityBalanced, hQualityPerfect, hBrowseModel, hAnalyze, hSearch, hFilamentList, hDetect, hManualProfiles} {
		enable(h, !b)
	}
	if b {
		enable(hImport, false)
		enable(hCancelImport, true)
		setText(hImport, tr("working"))
	} else {
		enable(hCancelImport, false)
		setText(hImport, tr("openFlash"))
	}
	invalidateSpatial()
}

func handleDrop(hdrop uintptr) {
	count, _, _ := pDragQueryFile.Call(hdrop, 0xffffffff, 0, 0)
	if count > 0 {
		n, _, _ := pDragQueryFile.Call(hdrop, 0, 0, 0)
		buf := make([]uint16, n+1)
		pDragQueryFile.Call(hdrop, 0, uintptr(unsafe.Pointer(&buf[0])), n+1)
		setModelPath(syscall.UTF16ToString(buf))
	}
	pDragFinish.Call(hdrop)
}

func chooseFile(title, filter, defExt string) string {
	buf := make([]uint16, 32768)
	filters := utf16Multi(filter)
	t := utf16Ptr(title)
	ext := utf16Ptr(defExt)
	ofn := openFileName{LStructSize: uint32(unsafe.Sizeof(openFileName{})), HwndOwner: mainHwnd, LpstrFilter: &filters[0], NFilterIndex: 1, LpstrFile: &buf[0], NMaxFile: uint32(len(buf)), LpstrTitle: t, Flags: OFN_EXPLORER | OFN_FILEMUSTEXIST | OFN_PATHMUSTEXIST | OFN_NOCHANGEDIR, LpstrDefExt: ext}
	r, _, _ := pGetOpenFileName.Call(uintptr(unsafe.Pointer(&ofn)))
	if r == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}
func utf16Multi(s string) []uint16 {
	u := utf16.Encode([]rune(s))
	if len(u) < 2 || u[len(u)-1] != 0 || u[len(u)-2] != 0 {
		u = append(u, 0, 0)
	}
	return u
}
func utf16Ptr(s string) *uint16 { p, _ := syscall.UTF16PtrFromString(s); return p }
func setText(hwnd uintptr, s string) {
	if hwnd == 0 {
		return
	}
	p := utf16Ptr(s)
	pSetWindowText.Call(hwnd, uintptr(unsafe.Pointer(p)))
}
func getText(hwnd uintptr) string {
	n, _, _ := pGetWindowTextLength.Call(hwnd)
	if n == 0 {
		return ""
	}
	b := make([]uint16, n+1)
	pGetWindowText.Call(hwnd, uintptr(unsafe.Pointer(&b[0])), n+1)
	return syscall.UTF16ToString(b)
}
func enable(hwnd uintptr, on bool) {
	v := uintptr(0)
	if on {
		v = 1
	}
	pEnableWindow.Call(hwnd, v)
}
func setStatus(s string) {
	app.statusKey = ""
	app.statusArgs = nil
	setText(hStatus, s)
	writeLog(s)
	invalidateSpatial()
}
func setStatusKey(key string, args ...any) {
	app.statusKey = key
	app.statusArgs = append([]any(nil), args...)
	value := trf(key, args...)
	setText(hStatus, value)
	writeLog(value)
	invalidateSpatial()
}
func messageBox(owner uintptr, text, title string, flags uintptr) int {
	t := utf16Ptr(text)
	c := utf16Ptr(title)
	r, _, _ := pMessageBox.Call(owner, uintptr(unsafe.Pointer(t)), uintptr(unsafe.Pointer(c)), flags)
	return int(r)
}
// Where generated projects go.
//
// The old version joined the literal string "Documents" onto the home
// directory and assumed the result was writable. Neither holds on Windows: the
// folder name is localised, and OneDrive's Known Folder Move can redirect it
// somewhere the plain path no longer resolves — which is how creating the
// output directory failed with "the system cannot find the file specified" on a
// machine whose Documents folder was plainly there.
//
// So the location is asked for rather than guessed, and each candidate is
// proven writable before it is used. Somewhere to write always exists, because
// the temporary directory is the last resort.
func defaultOutputDir() string {
	for _, base := range outputDirCandidates() {
		if base == "" {
			continue
		}
		dir := filepath.Join(base, "FlashFitAI")
		if err := os.MkdirAll(dir, 0700); err != nil {
			continue
		}
		if !directoryWritable(dir) {
			continue
		}
		return dir
	}
	// Every candidate failed; hand back the temp path and let the caller report
	// the real error rather than pretending.
	return filepath.Join(os.TempDir(), "FlashFitAI")
}

func outputDirCandidates() []string {
	candidates := []string{knownFolderDocuments()}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, "Documents"))
	}
	candidates = append(candidates, os.Getenv("LOCALAPPDATA"), os.TempDir())
	return candidates
}

// directoryWritable proves the directory accepts a file, which a directory that
// merely exists does not: a redirected or policy-locked folder can be listed
// and still refuse writes.
func directoryWritable(dir string) bool {
	probe, err := os.CreateTemp(dir, ".flashfit-write-*")
	if err != nil {
		return false
	}
	name := probe.Name()
	probe.Close()
	os.Remove(name)
	return true
}

var pSHGetKnownFolderPath = syscall.NewLazyDLL("shell32.dll").NewProc("SHGetKnownFolderPath")

// knownFolderDocuments asks Windows for the Documents folder, which is the only
// way to get the right answer when it is localised or redirected.
func knownFolderDocuments() string {
	if pSHGetKnownFolderPath.Find() != nil {
		return ""
	}
	// FOLDERID_Documents {FDD39AD0-238F-46AF-ADB4-6C85480369C7}
	guid := [16]byte{
		0xD0, 0x9A, 0xD3, 0xFD, 0x8F, 0x23, 0xAF, 0x46,
		0xAD, 0xB4, 0x6C, 0x85, 0x48, 0x03, 0x69, 0xC7,
	}
	var wide uintptr
	ret, _, _ := pSHGetKnownFolderPath.Call(uintptr(unsafe.Pointer(&guid[0])), 0, 0, uintptr(unsafe.Pointer(&wide)))
	if ret != 0 || wide == 0 {
		return ""
	}
	defer pCoTaskMemFree.Call(wide)
	return utf16PtrToString(wide)
}

var pCoTaskMemFree = syscall.NewLazyDLL("ole32.dll").NewProc("CoTaskMemFree")

// The path comes back in memory the shell allocated, not the Go heap, so it is
// read through a single conversion and a bounded view rather than by walking a
// raw address — one unchecked pointer step is enough to read past the end.
const maxKnownFolderChars = 32768

func utf16PtrToString(p uintptr) string {
	if p == 0 {
		return ""
	}
	units := unsafe.Slice((*uint16)(unsafe.Pointer(p)), maxKnownFolderChars)
	for i, unit := range units {
		if unit == 0 {
			return syscall.UTF16ToString(units[:i])
		}
	}
	return ""
}
func logPath() string {
	base := os.Getenv("APPDATA")
	if base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "FlashFitAI", "flashfit.log")
}
func writeLog(s string) {
	p := logPath()
	_ = os.MkdirAll(filepath.Dir(p), 0700)
	f, e := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if e == nil {
		fmt.Fprintf(f, "%s %s\r\n", time.Now().Format(time.RFC3339), s)
		f.Close()
	}
}
