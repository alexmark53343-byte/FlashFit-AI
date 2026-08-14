package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"flashfitai/shared"
)

type profileMeta struct {
	Type        string
	Name        string
	Material    string
	Compatible  string
	LayerHeight float64
}

func readProfileMeta(path string) profileMeta {
	b, err := os.ReadFile(path)
	if err != nil || len(b) > 4*1024*1024 {
		return profileMeta{}
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return profileMeta{}
	}
	scalar := func(v any) string {
		switch x := v.(type) {
		case string:
			return strings.TrimSpace(x)
		case []any:
			var out []string
			for _, y := range x {
				if s, ok := y.(string); ok && strings.TrimSpace(s) != "" {
					out = append(out, strings.TrimSpace(s))
				}
			}
			return strings.Join(out, " ")
		default:
			return ""
		}
	}
	compatible := strings.Join([]string{scalar(m["compatible_printers"]), scalar(m["compatible_printers_condition"]), scalar(m["printer_model"]), scalar(m["inherits"])}, " ")
	layer, _ := strconv.ParseFloat(strings.TrimSpace(scalar(m["layer_height"])), 64)
	return profileMeta{Type: strings.ToLower(scalar(m["type"])), Name: scalar(m["name"]), Material: strings.ToUpper(scalar(m["filament_type"])), Compatible: compatible, LayerHeight: layer}
}

func mergeFilaments(base, official []shared.Filament) []shared.Filament {
	seen := make(map[string]bool, len(base)+len(official))
	out := make([]shared.Filament, 0, len(base)+len(official))
	add := func(f shared.Filament) {
		key := strings.ToLower(strings.TrimSpace(f.Brand) + "|" + strings.TrimSpace(f.Product) + "|" + strings.TrimSpace(f.Material) + "|" + strings.TrimSpace(f.Variant) + "|" + strings.TrimSpace(f.SourcePath))
		if key == "||||" || seen[key] || shared.ValidateFilament(f) != nil {
			return
		}
		seen[key] = true
		out = append(out, f)
	}
	for _, f := range official {
		add(f)
	}
	for _, f := range base {
		add(f)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].OfficialProfile != out[j].OfficialProfile {
			return out[i].OfficialProfile
		}
		ai := strings.ToLower(out[i].Brand + " " + out[i].Product + " " + out[i].Variant)
		aj := strings.ToLower(out[j].Brand + " " + out[j].Product + " " + out[j].Variant)
		return ai < aj
	})
	return out
}

func filterFilaments(fs []shared.Filament, query string) []int {
	q := strings.ToLower(strings.TrimSpace(query))
	words := strings.Fields(q)
	out := make([]int, 0, len(fs))
	for i, f := range fs {
		hay := strings.ToLower(strings.Join([]string{f.Brand, f.Product, f.Material, f.Variant, f.Source, f.Notes}, " "))
		ok := true
		for _, w := range words {
			if !strings.Contains(hay, w) {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, i)
		}
	}
	return out
}

func processScore(path, quality string, cache map[string]profileMeta) int {
	return processScoreForPrinter(path, quality, shared.DefaultPrinterProfile(), cache)
}

func processScoreForPrinter(path, quality string, printer shared.PrinterProfile, cache map[string]profileMeta) int {
	m := cache[path]
	if m == (profileMeta{}) {
		m = readProfileMeta(path)
	}
	hay := strings.ToLower(path + " " + m.Name + " " + m.Compatible)
	score := 0
	if m.Type == "process" {
		score += 100
	}
	if shared.PrinterTextMatches(printer, hay) {
		score += 120
	} else if _, matched := shared.MatchPrinterText(hay); matched {
		return -1000
	}
	// Vendors mark a profile compatible with a whole family, so several models'
	// profiles are legitimately usable. Prefer the one actually named for this
	// machine, otherwise an AD5X profile can win on an AD5M.
	title := strings.ToLower(filepath.Base(path) + " " + m.Name)
	if shared.PrinterTextMatches(printer, title) {
		score += 60
	} else if named, matched := shared.MatchPrinterText(title); matched && named.ID != printer.ID {
		score -= 60
	}
	if !profileNozzleCompatible(hay, printer.NozzleDiameter) {
		return -1000
	}
	// Rank by the layer height the profile actually states. Keyword lists could
	// not tell "0.24mm Draft" from "0.12mm Fine" on a machine whose draft is
	// 0.24, which is how Fast and Perfect ended up on the same profile.
	if layer, ok := profileLayerHeight(path, m); ok {
		switch quality {
		case "low":
			score += int(layer * 400) // thicker is faster
		case "perfect":
			score += int((0.4 - layer) * 400) // thinner is finer
		default:
			delta := layer - 0.20
			if delta < 0 {
				delta = -delta
			}
			score += 160 - int(delta*800)
		}
	} else {
		switch quality {
		case "low":
			for _, x := range []string{"draft", "fast", "speed"} {
				if strings.Contains(hay, x) {
					score += 30
				}
			}
		case "perfect":
			for _, x := range []string{"fine", "detail", "quality"} {
				if strings.Contains(hay, x) {
					score += 30
				}
			}
		default:
			for _, x := range []string{"standard", "normal"} {
				if strings.Contains(hay, x) {
					score += 30
				}
			}
		}
	}
	if strings.Contains(hay, fmt.Sprintf("%.1f", printer.NozzleDiameter)) {
		score += 8
	}
	if strings.Contains(strings.ToLower(filepath.Base(path)), "user") {
		score -= 5
	}
	return score
}

// profileLayerHeight reads the layer height a process profile declares. The
// stated value in the profile wins; otherwise the leading "0.24mm" in the file
// name is used, which is the convention every vendor profile tree follows.
func profileLayerHeight(path string, m profileMeta) (float64, bool) {
	if m.LayerHeight > 0.01 && m.LayerHeight < 1.5 {
		return m.LayerHeight, true
	}
	name := strings.ToLower(filepath.Base(path))
	for i := 0; i+3 < len(name); i++ {
		if name[i] != '0' || name[i+1] != '.' {
			continue
		}
		end := i + 2
		for end < len(name) && name[end] >= '0' && name[end] <= '9' {
			end++
		}
		if end == i+2 {
			continue
		}
		// Only accept it when it reads as a layer height, not a nozzle size.
		if !strings.HasPrefix(name[end:], "mm") {
			continue
		}
		if value, err := strconv.ParseFloat(name[i:end], 64); err == nil && value > 0.01 && value < 1.5 {
			return value, true
		}
	}
	return 0, false
}

func profileNozzleCompatible(text string, nozzle float64) bool {
	wanted := formatNozzleValue(nozzle)
	mentioned := false
	for _, candidate := range []string{"0.2", "0.25", "0.3", "0.4", "0.5", "0.6", "0.8", "1"} {
		if profileMentionsNozzleValue(text, candidate) {
			mentioned = true
			if candidate == wanted {
				return true
			}
		}
	}
	return !mentioned
}

func profileMentionsNozzleValue(text, value string) bool {
	for _, token := range []string{
		value + " nozzle",
		value + "mm nozzle",
		value + " mm nozzle",
		"nozzle " + value,
		value + " buse",
		value + " boquilla",
		value + " duese",
		value + " ugello",
	} {
		if strings.Contains(text, token) {
			return true
		}
	}
	return false
}

func formatNozzleValue(mm float64) string {
	if mm <= 0 {
		mm = 0.4
	}
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", mm), "0"), ".")
}

func chooseProcess(paths []string, quality string, caches ...map[string]profileMeta) string {
	return chooseProcessForPrinter(paths, quality, shared.DefaultPrinterProfile(), caches...)
}

func chooseProcessForPrinter(paths []string, quality string, printer shared.PrinterProfile, caches ...map[string]profileMeta) string {
	cache := map[string]profileMeta{}
	if len(caches) > 0 && caches[0] != nil {
		cache = caches[0]
	}
	best, bestScore := "", -1
	for _, p := range paths {
		s := processScoreForPrinter(p, quality, printer, cache)
		if s > bestScore {
			best, bestScore = p, s
		}
	}
	if bestScore < 0 {
		return ""
	}
	return best
}

func chooseBaseFilament(paths []string, f shared.Filament, caches ...map[string]profileMeta) string {
	cache := map[string]profileMeta{}
	if len(caches) > 0 && caches[0] != nil {
		cache = caches[0]
	}
	if f.OfficialProfile && f.SourcePath != "" {
		m := cache[f.SourcePath]
		if m == (profileMeta{}) {
			m = readProfileMeta(f.SourcePath)
		}
		if m.Type == "filament" && materialMatches(m.Material, f.Material) {
			return f.SourcePath
		}
	}
	best, bestScore := "", -1
	want := strings.ToUpper(f.Material)
	for _, p := range paths {
		m := cache[p]
		if m == (profileMeta{}) {
			m = readProfileMeta(p)
		}
		if m.Type != "filament" || !materialMatches(m.Material, want) {
			continue
		}
		hay := strings.ToLower(p + " " + m.Name)
		score := 100
		if strings.Contains(hay, strings.ToLower(f.Brand)) {
			score += 25
		}
		if strings.Contains(hay, strings.ToLower(f.Product)) {
			score += 20
		}
		if strings.Contains(hay, "generic") || strings.Contains(hay, "generico") {
			score += 8
		}
		if strings.Contains(hay, "adventurer 5m") || strings.Contains(hay, "ad5m") {
			score += 5
		}
		if score > bestScore {
			best, bestScore = p, score
		}
	}
	return best
}

func materialMatches(profile, selected string) bool {
	p, s := strings.ToUpper(profile), strings.ToUpper(selected)
	if strings.HasPrefix(s, "PLA") {
		return strings.HasPrefix(p, "PLA")
	}
	if strings.HasPrefix(s, "PETG") {
		return strings.HasPrefix(p, "PETG")
	}
	return p == s
}

func materialFamily(material string) string {
	m := strings.ToUpper(strings.TrimSpace(material))
	for _, prefix := range []string{"PETG", "PLA", "ABS", "TPU"} {
		if strings.HasPrefix(m, prefix) {
			return prefix
		}
	}
	return m
}

const maxVisibleFilaments = 400

func visibleFilamentMatches(fs []shared.Filament, query string) ([]int, int) {
	all := filterFilaments(fs, query)
	total := len(all)
	if total > maxVisibleFilaments {
		return append([]int(nil), all[:maxVisibleFilaments]...), total
	}
	return all, total
}
