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
	"runtime/debug"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"flashfitai/shared"
)

const (
	appTitle  = "FlashFit AI Spatial 4.0 Beta"
	className = "FlashFitAI_Spatial_MainWindow_v40"

	WM_CREATE         = 0x0001
	WM_DESTROY        = 0x0002
	WM_SIZE           = 0x0005
	WM_CLOSE          = 0x0010
	WM_COMMAND        = 0x0111
	WM_DROPFILES      = 0x0233
	WM_SETFONT        = 0x0030
	WM_APP            = 0x8000
	WM_DISCOVERY_DONE = WM_APP + 1
	WM_ANALYSIS_DONE  = WM_APP + 2
	WM_IMPORT_DONE    = WM_APP + 3

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
	mainHwnd    uintptr
	appInstance uintptr
	hFont       uintptr
	mutexHandle uintptr
	uiCreateErr error
	smokeMode   bool

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
}

type analysisWorkerResult struct {
	Analysis shared.ModelAnalysis `json:"analysis"`
	Error    string               `json:"error,omitempty"`
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
	result.Discovery = shared.DiscoverProfiles()
	result.Official, result.Notes = shared.ScanOfficialFilaments(15000)
	result.ProfileMeta = make(map[string]profileMeta, len(result.Discovery.Processes)+len(result.Discovery.Filaments))
	for _, path := range append(append([]string(nil), result.Discovery.Processes...), result.Discovery.Filaments...) {
		if _, ok := result.ProfileMeta[path]; !ok {
			result.ProfileMeta[path] = readProfileMeta(path)
		}
	}
	result.MergedFilaments = mergeFilaments(base, result.Official)
	for _, quality := range []string{"low", "balanced", "perfect"} {
		result.ProcessChoices[quality] = chooseProcess(result.Discovery.Processes, quality, result.ProfileMeta)
	}
	for _, material := range []string{"PLA", "PETG", "ABS", "TPU"} {
		result.BaseChoices[material] = chooseBaseFilament(result.Discovery.Filaments, shared.Filament{Material: material}, result.ProfileMeta)
	}
	return writeWorkerJSON(outputPath, result)
}

func main() {
	if len(os.Args) == 4 && os.Args[1] == "--analyze-worker" {
		os.Exit(runAnalysisWorker(os.Args[2], os.Args[3]))
	}
	if len(os.Args) == 3 && os.Args[1] == "--discover-worker" {
		os.Exit(runDiscoveryWorker(os.Args[2]))
	}
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
		case "--version":
			fmt.Println("4.0.0-spatial-beta")
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
	name := utf16Ptr("Local\\FlashFitAI-4.0-Spatial-SingleInstance")
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
	wc := wndClassEx{CbSize: uint32(unsafe.Sizeof(wndClassEx{})), LpfnWndProc: syscall.NewCallback(windowProc), HInstance: inst, HIcon: icon, HCursor: cursor, HbrBackground: COLOR_BTNFACE + 1, LpszClassName: cls, HIconSm: icon}
	atom, _, err := pRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc)))
	if atom == 0 {
		return fmt.Errorf("impossibile registrare la finestra Win32: %v", err)
	}
	title := utf16Ptr(appTitle + " • Adventurer 5M")
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
		return 0
	case WM_SIZE:
		layoutUI(hwnd)
		return 0
	case WM_PAINT:
		paintSpatialUI(hwnd)
		return 0
	case WM_TIMER:
		if wParam == idSpatialAnimation {
			spatialAnimationTick++
			invalidateSpatial()
		}
		return 0
	case WM_ERASEBKGND:
		return 1
	case WM_LBUTTONUP:
		x, y := pointFromLParam(lParam)
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
}

func loadInitialCatalog() {
	fs, err := shared.LoadBuiltinFilaments()
	if err != nil {
		messageBox(mainHwnd, trf("databaseError", localizeEngineText(err.Error())), appTitle, MB_OK|MB_ICONERROR)
		return
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
	app.discovering = false
	enable(hDetect, true)
	app.profiles = d
	app.profileMeta = meta
	app.processChoices = processChoices
	app.baseChoices = baseChoices
	if !app.manualSlicer {
		app.slicer = d.SlicerExe
	}
	if !app.manualMachine {
		app.machine = d.Machine
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
	details := fmt.Sprintf("%s %s\r\n%s: %s • %s: %s\r\n%s: %.0f °C (%.0f–%.0f) • %s: %.0f °C\r\nMVS: %.1f mm³/s • Flow: %.3f • PA: %s\r\n%s: %s\r\n%s: %s", f.Brand, f.Product, tr("material"), f.Material, tr("variant"), f.Variant, tr("nozzleTemp"), f.NozzleDefault, f.NozzleMin, f.NozzleMax, tr("bedTemp"), f.BedDefault, f.MaxVolumetricSpeed, f.FlowRatio, pa, tr("reliability"), f.Confidence, tr("source"), source)
	if app.filamentMatchTotal > len(app.filtered) {
		details += "\r\n\r\n" + trf("shownResults", len(app.filtered), app.filamentMatchTotal)
	}
	setText(hFilamentDetails, details)
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

func renderAnalysis() {
	if app.analysis == nil {
		setText(hAnalysis, "Importa o trascina un file STL, OBJ o 3MF.\r\n\r\nFlashFit non modifica il file originale. Gli OBJ restano invariati; dai 3MF vengono rimosse le impostazioni nascoste conservando geometria e trasformazioni.")
		return
	}
	a := *app.analysis
	status := "VALIDO"
	if e := shared.ValidateAnalysis(a); e != nil {
		status = "BLOCCATO: " + e.Error()
	}
	lines := []string{fmt.Sprintf("Modello: %s", a.Filename), fmt.Sprintf("Formato: %s • Oggetti/istanze: %d • Geometria ripulita: %t", a.InputFormat, a.ObjectCount, a.Sanitized), fmt.Sprintf("Stato: %s", status), fmt.Sprintf("Dimensioni: %.2f × %.2f × %.2f mm", a.Extents[0], a.Extents[1], a.Extents[2]), fmt.Sprintf("Triangoli: %d • Categoria: %s", a.TriangleCount, a.Category), fmt.Sprintf("Mesh chiusa: %t • Facce degeneri: %d", a.Watertight, a.DegenerateFaces), fmt.Sprintf("Sbalzi stimati: %.1f%% • Supporti: %t • Brim: %t", a.OverhangRatio*100, a.SupportSuggested, a.BrimSuggested)}
	if f, ok := selectedFilament(); ok {
		if r, e := shared.Recommend(a, f, app.quality); e == nil {
			c := r.CriticalValues
			lines = append(lines, "", "LIMITI FLASHFIT", fmt.Sprintf("Layer %.2f mm • Parete esterna %.0f mm/s • Interna %.0f mm/s", c["layer_height"], c["outer_wall_speed"], c["inner_wall_speed"]), fmt.Sprintf("Infill %.0f mm/s • Ponti %.0f mm/s • Accelerazione esterna %.0f mm/s²", c["infill_speed"], c["bridge_speed"], c["outer_acceleration"]), fmt.Sprintf("MVS %.1f mm³/s • Ugello %.0f °C • Piano %.0f °C", c["max_volumetric_speed"], c["nozzle_temperature"], c["bed_temperature"]), "", strings.Join(r.Reasons, "\r\n"))
		}
	}
	if len(a.Warnings) > 0 {
		lines = append(lines, "", "AVVISI", strings.Join(a.Warnings, "\r\n"))
	}
	lines = append(lines, "", "Prima di stampare controlla sempre l’anteprima layer nello slicer.")
	setText(hAnalysis, strings.Join(lines, "\r\n"))
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
	req := shared.ImportRequest{Model: a, Filament: f, Quality: app.quality, SlicerExe: app.slicer, Machine: app.machine, BaseProcess: app.process, BaseFilament: app.baseFilament, OutputDir: out}
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
func defaultOutputDir() string {
	if h, e := os.UserHomeDir(); e == nil {
		return filepath.Join(h, "Documents", "FlashFitAI")
	}
	return filepath.Join(os.TempDir(), "FlashFitAI")
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
