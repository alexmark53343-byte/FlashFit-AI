package shared

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	pathpkg "path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

type DiscoveredProfiles struct {
	SlicerExe string   `json:"slicer_exe"`
	Machine   string   `json:"machine_profile"`
	Processes []string `json:"process_profiles"`
	Filaments []string `json:"filament_profiles"`
	Notes     []string `json:"notes"`
}

type ImportRequest struct {
	Model        ModelAnalysis
	Filament     Filament
	Quality      string
	Texture      string
	SlicerExe    string
	Machine      string
	BaseProcess  string
	BaseFilament string
	OutputDir    string
}

type ImportResult struct {
	ProjectPath string   `json:"project_path"`
	SummaryPath string   `json:"summary_path"`
	Command     []string `json:"command"`
	Stdout      string   `json:"stdout"`
	Stderr      string   `json:"stderr"`
}

func DiscoverProfiles() DiscoveredProfiles {
	var d DiscoveredProfiles
	for _, p := range possibleSlicerPaths() {
		if fileExists(p) {
			d.SlicerExe = p
			break
		}
	}
	if d.SlicerExe == "" {
		d.SlicerExe = discoverSlicerExecutable()
	}
	seenP, seenF := map[string]bool{}, map[string]bool{}
	for _, root := range likelyProfileRoots() {
		st, err := os.Stat(root)
		if err != nil || !st.IsDir() {
			continue
		}
		_ = filepath.WalkDir(root, func(path string, e os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			if e.IsDir() {
				n := strings.ToLower(e.Name())
				if n == "logs" || n == "log" || n == "cache" || n == "plugins" {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.ToLower(filepath.Ext(path)) != ".json" {
				return nil
			}
			st, err := e.Info()
			if err != nil || st.Size() > 4*1024*1024 {
				return nil
			}
			m, err := readJSONMap(path)
			if err != nil {
				return nil
			}
			typ := strings.ToLower(mapText(m, "type"))
			name := strings.ToLower(mapText(m, "name") + " " + mapText(m, "printer_model") + " " + mapText(m, "inherits"))
			switch typ {
			case "machine", "printer":
				if strings.Contains(name, "adventurer 5m") || strings.Contains(name, "ad5m") {
					if d.Machine == "" {
						d.Machine = path
					}
				}
			case "process":
				if profileMentionsAD5M(m) {
					if !seenP[path] {
						seenP[path] = true
						d.Processes = append(d.Processes, path)
					}
				}
			case "filament":
				mat := strings.ToUpper(mapText(m, "filament_type"))
				if strings.HasPrefix(mat, "PLA") || strings.HasPrefix(mat, "PETG") {
					if !seenF[path] {
						seenF[path] = true
						d.Filaments = append(d.Filaments, path)
					}
				}
			}
			return nil
		})
	}
	sort.Strings(d.Processes)
	sort.Strings(d.Filaments)
	if d.SlicerExe == "" {
		d.Notes = append(d.Notes, "Flash Studio non rilevato automaticamente.")
	}
	if d.Machine == "" {
		d.Notes = append(d.Notes, "Profilo macchina AD5M 0.4 non rilevato.")
	}
	if len(d.Processes) == 0 {
		d.Notes = append(d.Notes, "Nessun profilo processo AD5M ufficiale trovato.")
	}
	return d
}

func possibleSlicerPaths() []string {
	var out []string
	for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)", "LOCALAPPDATA"} {
		if base := os.Getenv(env); base != "" {
			for _, s := range []string{
				filepath.Join("FlashForge", "Flash Studio", "Flash Studio.exe"),
				filepath.Join("FlashForge", "Flash Studio", "FlashStudio.exe"),
				filepath.Join("FlashForge", "Flash Studio Desktop", "Flash Studio Desktop.exe"),
				filepath.Join("Flashforge", "Flash Studio", "Flash Studio.exe"),
				filepath.Join("Flashforge", "FlashStudio", "FlashStudio.exe"),
				filepath.Join("FlashForge", "Orca-Flashforge", "orca-flashforge.exe"),
				filepath.Join("Programs", "Flash Studio", "Flash Studio.exe"),
				filepath.Join("Programs", "Flash Studio Desktop", "Flash Studio Desktop.exe"),
				filepath.Join("OrcaSlicer", "orca-slicer.exe")} {
				out = append(out, filepath.Join(base, s))
			}
		}
	}
	return out
}
func discoverSlicerExecutable() string {
	var roots []string
	for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)"} {
		if base := os.Getenv(env); base != "" {
			roots = append(roots, filepath.Join(base, "FlashForge"), filepath.Join(base, "Flashforge"))
		}
	}
	if base := os.Getenv("LOCALAPPDATA"); base != "" {
		roots = append(roots, filepath.Join(base, "Programs", "FlashForge"), filepath.Join(base, "Programs", "Flashforge"), filepath.Join(base, "Programs", "Flash Studio"), filepath.Join(base, "Programs", "Flash Studio Desktop"), filepath.Join(base, "FlashForge"))
	}
	var candidates []string
	visited := 0
	for _, root := range roots {
		st, err := os.Stat(root)
		if err != nil || !st.IsDir() {
			continue
		}
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			visited++
			if visited > 20000 {
				return filepath.SkipAll
			}
			if d.IsDir() {
				n := strings.ToLower(d.Name())
				if n == "cache" || n == "logs" || n == "log" || n == "updates" || n == "uninstall" {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.ToLower(filepath.Ext(path)) != ".exe" {
				return nil
			}
			n := strings.ToLower(strings.ReplaceAll(d.Name(), "_", "-"))
			if strings.Contains(n, "flash studio") || strings.Contains(n, "flashstudio") || strings.Contains(n, "orca-flashforge") {
				candidates = append(candidates, path)
			}
			return nil
		})
	}
	if len(candidates) == 0 {
		return ""
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := strings.ToLower(filepath.Base(candidates[i])), strings.ToLower(filepath.Base(candidates[j]))
		score := func(n string) int {
			v := 0
			if strings.Contains(n, "flash studio") || strings.Contains(n, "flashstudio") {
				v += 20
			}
			if strings.Contains(n, "orca-flashforge") {
				v += 10
			}
			if strings.Contains(n, "uninstall") || strings.Contains(n, "update") {
				v -= 50
			}
			return v
		}
		if score(a) != score(b) {
			return score(a) > score(b)
		}
		return candidates[i] < candidates[j]
	})
	return candidates[0]
}

func fileExists(p string) bool { st, e := os.Stat(p); return e == nil && !st.IsDir() }
func readJSONMap(path string) (map[string]any, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return nil, e
	}
	if len(b) > 4*1024*1024 {
		return nil, errors.New("profilo oltre 4 MB")
	}
	var m map[string]any
	e = json.Unmarshal(b, &m)
	return m, e
}
func mapText(m map[string]any, key string) string { return scalar(m[key]) }
func profileMentionsAD5M(m map[string]any) bool {
	b, _ := json.Marshal(m)
	s := strings.ToLower(string(b))
	return strings.Contains(s, "adventurer 5m") || strings.Contains(s, "ad5m")
}

var processPatchKeys = map[string]bool{
	"layer_height": true, "initial_layer_print_height": true, "line_width": true, "outer_wall_line_width": true, "inner_wall_line_width": true, "sparse_infill_line_width": true,
	"wall_loops": true, "top_shell_layers": true, "bottom_shell_layers": true, "top_shell_thickness": true, "bottom_shell_thickness": true,
	"sparse_infill_density": true, "sparse_infill_pattern": true, "top_surface_pattern": true, "bottom_surface_pattern": true, "top_surface_density": true, "bottom_surface_density": true,
	"wall_generator": true, "precise_outer_wall": true, "only_one_wall_top": true, "ensure_vertical_shell_thickness": true, "infill_wall_overlap": true, "top_bottom_infill_wall_overlap": true,
	"seam_position": true, "staggered_inner_seams": true, "seam_gap": true,
	"enable_support": true, "support_type": true, "support_threshold_angle": true, "brim_type": true, "brim_width": true,
	"outer_wall_speed": true, "inner_wall_speed": true, "sparse_infill_speed": true, "internal_solid_infill_speed": true, "top_surface_speed": true, "small_perimeter_speed": true, "gap_infill_speed": true, "bridge_speed": true, "support_speed": true, "support_interface_speed": true,
	"initial_layer_speed": true, "initial_layer_infill_speed": true, "ironing_speed": true,
	"outer_wall_acceleration": true, "inner_wall_acceleration": true, "sparse_infill_acceleration": true, "internal_solid_infill_acceleration": true, "top_surface_acceleration": true, "bridge_acceleration": true, "initial_layer_acceleration": true, "travel_acceleration": true, "travel_speed": true,
	"bridge_flow": true, "avoid_crossing_wall": true, "reduce_infill_retraction": true, "overhang_1_4_speed": true, "overhang_2_4_speed": true, "overhang_3_4_speed": true, "overhang_4_4_speed": true,
	"ironing_type": true, "ironing_pattern": true, "ironing_flow": true, "ironing_spacing": true, "ironing_inset": true,
	"fuzzy_skin": true, "fuzzy_skin_thickness": true, "fuzzy_skin_point_distance": true, "fuzzy_skin_first_layer": true,
}
var filamentPatchKeys = map[string]bool{
	"filament_density": true, "filament_flow_ratio": true, "filament_max_volumetric_speed": true, "nozzle_temperature": true, "nozzle_temperature_initial_layer": true, "hot_plate_temp": true, "hot_plate_temp_initial_layer": true, "textured_plate_temp": true, "textured_plate_temp_initial_layer": true, "fan_min_speed": true, "fan_max_speed": true, "enable_pressure_advance": true, "pressure_advance": true,
	"close_fan_the_first_x_layers": true, "full_fan_speed_layer": true, "slow_down_for_layer_cooling": true, "slow_down_layer_time": true, "min_print_speed": true,
}

func ValidateSlicerExe(path string) error {
	if runtime.GOOS != "windows" {
		return nil
	}
	if !fileExists(path) || strings.ToLower(filepath.Ext(path)) != ".exe" {
		return errors.New("eseguibile Flash Studio non valido")
	}
	n := strings.ToLower(filepath.Base(path))
	if !strings.Contains(n, "flash") && !strings.Contains(n, "orca") {
		return errors.New("l'eseguibile non sembra Flash Studio/Orca")
	}
	return nil
}

func ProbeSlicerCLI(path string) error {
	if err := ValidateSlicerExe(path); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		return nil
	}
	// Many Flash Studio builds keep the CLI strings inside the executable.
	// The scan is streamed, so a large EXE cannot create a memory spike.
	if executableContainsCLIFlags(path) {
		return nil
	}
	var combined string
	for _, arg := range []string{"--help", "-h"} {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		cmd := exec.CommandContext(ctx, path, arg)
		out, err := cmd.CombinedOutput()
		cancel()
		combined += "\n" + string(out)
		lower := strings.ToLower(string(out))
		if strings.Contains(lower, "--export-3mf") &&
			strings.Contains(lower, "--load-settings") &&
			strings.Contains(lower, "--load-filaments") {
			return nil
		}
		if err == nil && ctx.Err() == nil {
			continue
		}
	}
	return fmt.Errorf("l'eseguibile rilevato non espone la CLI Orca necessaria. Potrebbe essere soltanto il launcher di Flash Studio. Seleziona manualmente l'eseguibile del motore Orca/Flash Studio. Output: %s", tail(strings.TrimSpace(combined), 500))
}

func executableContainsCLIFlags(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	utf16LE := func(v string) []byte {
		out := make([]byte, 0, len(v)*2)
		for i := 0; i < len(v); i++ {
			out = append(out, v[i], 0)
		}
		return out
	}
	ascii := [][]byte{[]byte("--export-3mf"), []byte("--load-settings"), []byte("--load-filaments")}
	wide := [][]byte{utf16LE("--export-3mf"), utf16LE("--load-settings"), utf16LE("--load-filaments")}
	foundASCII := make([]bool, len(ascii))
	foundWide := make([]bool, len(wide))
	const chunkSize = 1024 * 1024
	const maxScan = int64(512 * 1024 * 1024)
	buf := make([]byte, chunkSize)
	var carry []byte
	var total int64
	for total < maxScan {
		n, readErr := f.Read(buf)
		if n > 0 {
			total += int64(n)
			block := make([]byte, 0, len(carry)+n)
			block = append(block, carry...)
			block = append(block, buf[:n]...)
			for i, pat := range ascii {
				if !foundASCII[i] && bytes.Contains(block, pat) {
					foundASCII[i] = true
				}
			}
			for i, pat := range wide {
				if !foundWide[i] && bytes.Contains(block, pat) {
					foundWide[i] = true
				}
			}
			all := func(v []bool) bool {
				for _, ok := range v {
					if !ok {
						return false
					}
				}
				return true
			}
			if all(foundASCII) || all(foundWide) {
				return true
			}
			keep := 96
			if len(block) < keep {
				keep = len(block)
			}
			carry = append(carry[:0], block[len(block)-keep:]...)
		}
		if readErr != nil {
			break
		}
	}
	return false
}

func buildSlicerArgs(machine, processProfile, filamentProfile, candidate, model string, arrange bool) []string {
	args := []string{
		"--load-settings", machine + ";" + processProfile,
		"--load-filaments", filamentProfile,
	}
	if arrange {
		// Forma con '=': alcune build Flash Studio interpretano il valore separato "1"
		// come se fosse un secondo modello, mostrando "modello inesistente: 1".
		args = append(args, "--arrange=1")
	}
	args = append(args, "--export-3mf", candidate, model)
	return args
}

func standaloneArrangeValueBug(stdout, stderr string) bool {
	t := strings.ToLower(stdout + "\n" + stderr)
	hasMissing := strings.Contains(t, "not exist") || strings.Contains(t, "does not exist") ||
		strings.Contains(t, "non esiste") || strings.Contains(t, "inesistente") ||
		strings.Contains(t, "not found")
	hasModel := strings.Contains(t, "model") || strings.Contains(t, "modello")
	return hasMissing && hasModel && (strings.Contains(t, `"1"`) || strings.Contains(t, ": 1") || strings.Contains(t, " 1\n"))
}

func ValidateMachineProfile(path string) error {
	m, e := readJSONMap(path)
	if e != nil {
		return fmt.Errorf("profilo macchina illeggibile: %w", e)
	}
	b, _ := json.Marshal(m)
	s := strings.ToLower(string(b))
	if !strings.Contains(s, "adventurer 5m") && !strings.Contains(s, "ad5m") {
		return errors.New("profilo macchina non AD5M")
	}
	if nd := scalar(m["nozzle_diameter"]); nd != "" && !strings.Contains(nd, "0.4") {
		return errors.New("profilo macchina non usa ugello 0,4 mm")
	}
	return nil
}
func validateBaseProcess(path string) (map[string]any, error) {
	m, e := readJSONMap(path)
	if e != nil {
		return nil, e
	}
	if strings.ToLower(mapText(m, "type")) != "process" {
		return nil, errors.New("file base non è un profilo processo")
	}
	if !profileMentionsAD5M(m) {
		return nil, errors.New("profilo processo non dichiara compatibilità AD5M")
	}
	return m, nil
}
func validateBaseFilament(path, material string) (map[string]any, error) {
	m, e := readJSONMap(path)
	if e != nil {
		return nil, e
	}
	if strings.ToLower(mapText(m, "type")) != "filament" {
		return nil, errors.New("file base non è un profilo filamento")
	}
	mat := strings.ToUpper(mapText(m, "filament_type"))
	want := strings.ToUpper(material)
	if strings.HasPrefix(want, "PLA") && !strings.HasPrefix(mat, "PLA") {
		return nil, fmt.Errorf("profilo base %s non PLA", mat)
	}
	if strings.HasPrefix(want, "PETG") && !strings.HasPrefix(mat, "PETG") {
		return nil, fmt.Errorf("profilo base %s non PETG", mat)
	}
	return m, nil
}

func PatchProfiles(baseProcess, baseFilament string, rec Recommendation, f Filament, outDir string) (string, string, string, error) {
	p, e := validateBaseProcess(baseProcess)
	if e != nil {
		return "", "", "", e
	}
	fp, e := validateBaseFilament(baseFilament, f.Material)
	if e != nil {
		return "", "", "", e
	}
	pname := recommendationProfileName(rec)
	fname := safeProfileName("FlashFit " + f.Brand + " " + f.Product)
	p["type"] = "process"
	p["name"] = pname
	p["from"] = "user"
	p["instantiation"] = "true"
	p["is_custom_defined"] = "1"
	p["print_settings_id"] = pname
	fp["type"] = "filament"
	fp["name"] = fname
	fp["from"] = "user"
	fp["instantiation"] = "true"
	fp["filament_settings_id"] = []string{fname}
	for k, v := range rec.Process {
		if !processPatchKeys[k] {
			return "", "", "", fmt.Errorf("parametro processo non autorizzato: %s", k)
		}
		p[k] = fmt.Sprint(v)
	}
	for k, v := range rec.Filament {
		if !filamentPatchKeys[k] {
			return "", "", "", fmt.Errorf("parametro filamento non autorizzato: %s", k)
		}
		fp[k] = []string{fmt.Sprint(v)}
	}
	if err := os.MkdirAll(outDir, 0700); err != nil {
		return "", "", "", err
	}
	pp := filepath.Join(outDir, "flashfit_process.json")
	ff := filepath.Join(outDir, "flashfit_filament.json")
	ss := filepath.Join(outDir, "flashfit_summary.json")
	summary := map[string]any{"schema_version": 6, "machine": "Flashforge Adventurer 5M", "nozzle_mm": 0.4, "base_process": baseProcess, "base_filament": baseFilament, "process_profile_name": pname, "filament_profile_name": fname, "critical_values": rec.CriticalValues, "critical_settings": rec.CriticalSettings, "recommendation": rec, "model_sha256": ""}
	if e = atomicJSON(pp, p); e != nil {
		return "", "", "", e
	}
	if e = atomicJSON(ff, fp); e != nil {
		return "", "", "", e
	}
	if e = atomicJSON(ss, summary); e != nil {
		return "", "", "", e
	}
	// Rilettura completa, non solo esistenza file.
	p2, e := readJSONMap(pp)
	if e != nil {
		return "", "", "", e
	}
	f2, e := readJSONMap(ff)
	if e != nil {
		return "", "", "", e
	}
	for k, v := range rec.Process {
		if scalar(p2[k]) != fmt.Sprint(v) {
			return "", "", "", fmt.Errorf("verifica profilo processo fallita su %s", k)
		}
	}
	for k, v := range rec.Filament {
		if scalar(f2[k]) != fmt.Sprint(v) {
			return "", "", "", fmt.Errorf("verifica profilo filamento fallita su %s", k)
		}
	}
	return pp, ff, ss, nil
}
func safeProfileName(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`\\/:*?"<>|;`, r) {
			return '_'
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if len(s) > 110 {
		s = s[:110]
	}
	return s
}

func recommendationProfileName(rec Recommendation) string {
	name := "FlashFit " + rec.QualityLabel
	if rec.Quality == "perfect" && rec.TextureLabel != "" && rec.Texture != "none" {
		name += " - " + rec.TextureLabel
	}
	return safeProfileName(name + " AD5M 0.4")
}
func atomicJSON(path string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	tmp := path + ".tmp"
	if e = os.WriteFile(tmp, b, 0600); e != nil {
		return e
	}
	return os.Rename(tmp, path)
}

func BuildAndOpen(req ImportRequest) (ImportResult, error) {
	return BuildAndOpenContext(context.Background(), req)
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}
func (b *lockedBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.b.String() }
func (b *lockedBuffer) Reset()         { b.mu.Lock(); b.b.Reset(); b.mu.Unlock() }

func BuildAndOpenContext(parent context.Context, req ImportRequest) (ImportResult, error) {
	if err := ValidateAnalysis(req.Model); err != nil {
		return ImportResult{}, err
	}
	if err := ValidateFilament(req.Filament); err != nil {
		return ImportResult{}, err
	}
	if err := ValidateSlicerExe(req.SlicerExe); err != nil {
		return ImportResult{}, err
	}
	if err := ProbeSlicerCLI(req.SlicerExe); err != nil {
		return ImportResult{}, err
	}
	if err := ValidateMachineProfile(req.Machine); err != nil {
		return ImportResult{}, err
	}
	if !fileExists(req.Model.StoredModelPath) {
		return ImportResult{}, errors.New("copia geometrica temporanea non trovata")
	}
	h, e := fileSHA256(req.Model.StoredModelPath)
	if e != nil || h != req.Model.SHA256 {
		return ImportResult{}, errors.New("il modello usato per l'importazione è cambiato dopo l'analisi")
	}
	if req.Model.SourcePath != "" && req.Model.SourceSHA256 != "" {
		sh, hashErr := fileSHA256(req.Model.SourcePath)
		if hashErr != nil || sh != req.Model.SourceSHA256 {
			return ImportResult{}, errors.New("il file originale è cambiato dopo l'analisi: riesegui Analizza")
		}
	}
	rec, e := RecommendWithTexture(req.Model, req.Filament, req.Quality, req.Texture)
	if e != nil {
		return ImportResult{}, e
	}
	outDir := req.OutputDir
	if outDir == "" {
		outDir = filepath.Join(userDataDir(), "output")
	}
	if e = os.MkdirAll(outDir, 0700); e != nil {
		return ImportResult{}, e
	}
	work, e := os.MkdirTemp(outDir, ".flashfit-")
	if e != nil {
		return ImportResult{}, e
	}
	defer os.RemoveAll(work)
	pp, ff, summary, e := PatchProfiles(req.BaseProcess, req.BaseFilament, rec, req.Filament, work)
	if e != nil {
		return ImportResult{}, e
	}
	if sm, readErr := readJSONMap(summary); readErr == nil {
		sm["model_sha256"] = req.Model.SHA256
		sm["source_sha256"] = req.Model.SourceSHA256
		sm["model_filename"] = req.Model.Filename
		sm["input_format"] = req.Model.InputFormat
		sm["geometry_sanitized"] = req.Model.Sanitized
		sm["model_triangles"] = req.Model.TriangleCount
		sm["model_extents_mm"] = req.Model.Extents
		sm["flashfit_engine"] = "1.1-native"
		if writeErr := atomicJSON(summary, sm); writeErr != nil {
			return ImportResult{}, fmt.Errorf("rapporto di verifica non scrivibile: %w", writeErr)
		}
	}
	name := strings.TrimSuffix(filepath.Base(req.Model.Filename), filepath.Ext(req.Model.Filename))
	project := uniquePath(filepath.Join(outDir, safeProfileName(name)+"_FlashFit.3mf"))
	candidate := filepath.Join(work, "candidate.3mf")
	if strings.Contains(req.Machine, ";") || strings.Contains(pp, ";") || strings.Contains(ff, ";") {
		return ImportResult{}, errors.New("i percorsi dei profili non possono contenere ;")
	}
	args := buildSlicerArgs(req.Machine, pp, ff, candidate, req.Model.StoredModelPath, true)
	ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
	defer cancel()
	var stdout, stderr lockedBuffer
	runAttempt := func(runArgs []string) error {
		stdout.Reset()
		stderr.Reset()
		_ = os.Remove(candidate)
		cmd := exec.Command(req.SlicerExe, runArgs...)
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		return runSlicerUntilProject(ctx, cmd, candidate)
	}
	e = runAttempt(args)
	if e != nil && ctx.Err() == nil {
		// Secondo tentativo compatibile senza auto-arrange. Questo copre sia le build
		// che interpretavano il vecchio valore separato "1" come un modello, sia
		// i fork che non espongono affatto l'opzione arrange in modalità CLI.
		args = buildSlicerArgs(req.Machine, pp, ff, candidate, req.Model.StoredModelPath, false)
		e = runAttempt(args)
	}
	if e != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return ImportResult{}, errors.New("importazione annullata dall'utente")
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return ImportResult{}, errors.New("Flash Studio non ha prodotto il progetto entro 2 minuti; operazione interrotta")
		}
		return ImportResult{}, fmt.Errorf("Flash Studio non ha prodotto un progetto valido: %v\n%s", e, tail(stderr.String(), 1800))
	}
	markers := []string{recommendationProfileName(rec), safeProfileName("FlashFit " + req.Filament.Brand + " " + req.Filament.Product)}
	if e = validate3MF(candidate, markers, rec, &req.Model); e != nil {
		return ImportResult{}, fmt.Errorf("3MF prodotto non certificato: %w", e)
	}
	if e = copyAtomic(candidate, project); e != nil {
		return ImportResult{}, e
	}
	if e = validate3MF(project, markers, rec, &req.Model); e != nil {
		os.Remove(project)
		return ImportResult{}, e
	}
	finalSummary := strings.TrimSuffix(project, ".3mf") + "_report.json"
	b, _ := os.ReadFile(summary)
	_ = os.WriteFile(finalSummary, b, 0600)
	open := exec.Command(req.SlicerExe, project)
	if e = open.Start(); e != nil {
		os.Remove(project)
		os.Remove(finalSummary)
		return ImportResult{}, fmt.Errorf("progetto verificato ma impossibile aprire Flash Studio: %w", e)
	}
	return ImportResult{ProjectPath: project, SummaryPath: finalSummary, Command: append([]string{req.SlicerExe}, args...), Stdout: stdout.String(), Stderr: stderr.String()}, nil
}

// Some Flash Studio builds keep their GUI process alive after completing a CLI export.
// Waiting for process exit made FlashFit look frozen. We instead watch the output file,
// while still detecting an early command failure and honoring cancellation.
func runSlicerUntilProject(ctx context.Context, cmd *exec.Cmd, candidate string) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	lastSize := int64(-1)
	stable := 0
	processDone := false
	var processErr error
	for {
		select {
		case <-ctx.Done():
			if cmd.Process != nil && !processDone {
				_ = cmd.Process.Kill()
			}
			return ctx.Err()
		case err := <-waitCh:
			processDone = true
			processErr = err
			// Continue a few ticks: antivirus/indexing can delay the final rename.
		case <-ticker.C:
			st, statErr := os.Stat(candidate)
			if statErr == nil && st.Size() >= 200 {
				if st.Size() == lastSize {
					stable++
					if stable >= 3 {
						return nil
					}
				} else {
					lastSize = st.Size()
					stable = 0
				}
			}
			if processDone && processErr != nil && stable == 0 {
				return processErr
			}
			if processDone && processErr == nil && statErr != nil {
				return errors.New("il comando si è chiuso senza creare il 3MF")
			}
		}
	}
}

func waitStable(path string, timeout time.Duration) error {
	end := time.Now().Add(timeout)
	last := -1
	stable := 0
	for time.Now().Before(end) {
		st, e := os.Stat(path)
		if e == nil && st.Size() > 0 {
			if int(st.Size()) == last {
				stable++
				if stable >= 3 {
					return nil
				}
			} else {
				stable = 0
				last = int(st.Size())
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	return errors.New("Flash Studio non ha prodotto un 3MF stabile")
}
func uniquePath(path string) string {
	if !fileExists(path) {
		return path
	}
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	for i := 2; i < 10000; i++ {
		p := fmt.Sprintf("%s_%d%s", base, i, ext)
		if !fileExists(p) {
			return p
		}
	}
	return base + "_new" + ext
}
func copyAtomic(src, dst string) error {
	if fileExists(dst) {
		return errors.New("destinazione già esistente")
	}
	in, e := os.Open(src)
	if e != nil {
		return e
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, e := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	_, e = io.Copy(out, in)
	ce := out.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		os.Remove(tmp)
		return e
	}
	return os.Rename(tmp, dst)
}
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
func userDataDir() string {
	if v := os.Getenv("APPDATA"); v != "" {
		return filepath.Join(v, "FlashFitAI")
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".flashfitai")
}

type meshGeometry struct {
	Triangles int
	Extents   [3]float64
}

func Validate3MF(path string, markers []string, rec Recommendation) error {
	return validate3MF(path, markers, rec, nil)
}

func validate3MF(path string, markers []string, rec Recommendation, expectedModel *ModelAnalysis) error {
	st, e := os.Stat(path)
	if e != nil || st.Size() < 200 {
		return errors.New("3MF mancante o troppo piccolo")
	}
	zr, e := zip.OpenReader(path)
	if e != nil {
		return errors.New("3MF non è un archivio ZIP valido")
	}
	defer zr.Close()
	models := 0
	triangles := 0
	var totalSize uint64
	var geometries []meshGeometry
	var text strings.Builder
	for _, f := range zr.File {
		rawName := strings.ReplaceAll(f.Name, "\\", "/")
		cleanName := pathpkg.Clean(rawName)
		if rawName == "" || strings.HasPrefix(rawName, "/") || cleanName == ".." || strings.HasPrefix(cleanName, "../") || strings.Contains(rawName, ":") || f.Mode()&os.ModeSymlink != 0 {
			return errors.New("3MF contiene un percorso interno non sicuro")
		}
		totalSize += f.UncompressedSize64
		if totalSize > 512*1024*1024 {
			return errors.New("contenuto 3MF complessivo troppo grande")
		}
		n := strings.ToLower(cleanName)
		if f.UncompressedSize64 > 64*1024*1024 {
			return errors.New("voce 3MF troppo grande")
		}
		r, e := f.Open()
		if e != nil {
			return e
		}
		data, e := io.ReadAll(io.LimitReader(r, 64*1024*1024+1))
		r.Close()
		if e != nil {
			return e
		}
		if strings.HasSuffix(n, ".model") {
			models++
			var doc struct {
				Unit      string `xml:"unit,attr"`
				Resources struct {
					Objects []struct {
						Mesh struct {
							Vertices []struct {
								X float64 `xml:"x,attr"`
								Y float64 `xml:"y,attr"`
								Z float64 `xml:"z,attr"`
							} `xml:"vertices>vertex"`
							Triangles []struct{} `xml:"triangles>triangle"`
						} `xml:"mesh"`
					} `xml:"object"`
				} `xml:"resources"`
			}
			if xml.Unmarshal(data, &doc) == nil {
				factor := modelUnitFactor(doc.Unit)
				for _, o := range doc.Resources.Objects {
					triCount := len(o.Mesh.Triangles)
					triangles += triCount
					if triCount == 0 || len(o.Mesh.Vertices) == 0 {
						continue
					}
					minv := [3]float64{math.Inf(1), math.Inf(1), math.Inf(1)}
					maxv := [3]float64{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
					for _, v := range o.Mesh.Vertices {
						coords := [3]float64{v.X * factor, v.Y * factor, v.Z * factor}
						for i := 0; i < 3; i++ {
							if math.IsNaN(coords[i]) || math.IsInf(coords[i], 0) {
								return errors.New("3MF contiene coordinate non finite")
							}
							minv[i] = math.Min(minv[i], coords[i])
							maxv[i] = math.Max(maxv[i], coords[i])
						}
					}
					geometries = append(geometries, meshGeometry{Triangles: triCount, Extents: [3]float64{maxv[0] - minv[0], maxv[1] - minv[1], maxv[2] - minv[2]}})
				}
			}
		}
		if strings.HasSuffix(n, ".json") || strings.HasSuffix(n, ".xml") || strings.HasSuffix(n, ".config") || strings.HasSuffix(n, ".model") {
			if len(data) < 12*1024*1024 {
				text.Write(data)
				text.WriteByte('\n')
			}
		}
	}
	if models == 0 || triangles == 0 {
		return errors.New("3MF senza geometria triangolare leggibile")
	}
	if expectedModel != nil && !matchesOriginalGeometry(geometries, *expectedModel) {
		return errors.New("la geometria del 3MF non corrisponde allo STL analizzato: possibile scala, perdita di facce o trasformazione inattesa")
	}
	all := text.String()
	for _, m := range markers {
		if !strings.Contains(all, m) {
			return fmt.Errorf("marcatore profilo assente: %s", m)
		}
	}
	// Verifica chiavi e, quando disponibili, anche i valori realmente scritti.
	expected := map[string]string{
		"layer_height":                  fmt2(rec.CriticalValues["layer_height"]),
		"outer_wall_speed":              fmt0(rec.CriticalValues["outer_wall_speed"]),
		"outer_wall_acceleration":       fmt0(rec.CriticalValues["outer_acceleration"]),
		"filament_max_volumetric_speed": fmt2(rec.CriticalValues["max_volumetric_speed"]),
		"nozzle_temperature":            fmt0(rec.CriticalValues["nozzle_temperature"]),
	}
	for _, k := range []string{"layer_height", "outer_wall_speed", "outer_wall_acceleration", "filament_max_volumetric_speed", "nozzle_temperature"} {
		if !strings.Contains(all, k) {
			return fmt.Errorf("parametro critico non conservato nel 3MF: %s", k)
		}
		if len(rec.CriticalValues) > 0 && !keyHasNumericValue(all, k, expected[k]) {
			return fmt.Errorf("valore critico non conservato nel 3MF: %s=%s", k, expected[k])
		}
	}
	for key, value := range rec.CriticalSettings {
		if !keyHasTextValue(all, key, value) {
			return fmt.Errorf("impostazione profilo non conservata nel 3MF: %s=%s", key, value)
		}
	}
	return nil
}

func modelUnitFactor(unit string) float64 {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "micron":
		return 0.001
	case "centimeter":
		return 10
	case "meter":
		return 1000
	case "inch":
		return 25.4
	case "foot":
		return 304.8
	default:
		return 1
	}
}

func matchesOriginalGeometry(gs []meshGeometry, expected ModelAnalysis) bool {
	ex := expected.Extents
	exXY := [2]float64{math.Min(ex[0], ex[1]), math.Max(ex[0], ex[1])}
	triTol := int(math.Max(1, math.Ceil(float64(expected.TriangleCount)*0.002)))
	for _, g := range gs {
		if absInt(g.Triangles-expected.TriangleCount) > triTol {
			continue
		}
		gXY := [2]float64{math.Min(g.Extents[0], g.Extents[1]), math.Max(g.Extents[0], g.Extents[1])}
		if closeExtent(gXY[0], exXY[0]) && closeExtent(gXY[1], exXY[1]) && closeExtent(g.Extents[2], ex[2]) {
			return true
		}
	}
	return false
}

func closeExtent(got, want float64) bool {
	tol := math.Max(0.15, math.Abs(want)*0.003)
	return math.Abs(got-want) <= tol
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func keyHasNumericValue(text, key, value string) bool {
	if key == "" || value == "" {
		return false
	}
	start := 0
	for {
		i := strings.Index(text[start:], key)
		if i < 0 {
			return false
		}
		i += start
		end := i + len(key) + 180
		if end > len(text) {
			end = len(text)
		}
		window := text[i+len(key) : end]
		for pos := 0; ; {
			j := strings.Index(window[pos:], value)
			if j < 0 {
				break
			}
			j += pos
			leftOK := j == 0 || !numericByte(window[j-1])
			right := j + len(value)
			rightOK := right >= len(window) || !numericByte(window[right])
			if leftOK && rightOK {
				return true
			}
			pos = j + 1
		}
		start = i + len(key)
	}
}

func keyHasTextValue(text, key, value string) bool {
	if key == "" || value == "" {
		return false
	}
	start := 0
	for {
		i := strings.Index(text[start:], key)
		if i < 0 {
			return false
		}
		i += start
		end := i + len(key) + 300
		if end > len(text) {
			end = len(text)
		}
		if strings.Contains(text[i+len(key):end], value) {
			return true
		}
		start = i + len(key)
	}
}

func numericByte(b byte) bool {
	return (b >= '0' && b <= '9') || b == '.' || b == '-' || b == '+' || b == 'e' || b == 'E'
}
