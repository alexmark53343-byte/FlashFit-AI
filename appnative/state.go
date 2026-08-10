package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"flashfitai/shared"
)

type profileMeta struct {
	Type       string
	Name       string
	Material   string
	Compatible string
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
	return profileMeta{Type: strings.ToLower(scalar(m["type"])), Name: scalar(m["name"]), Material: strings.ToUpper(scalar(m["filament_type"])), Compatible: compatible}
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
	switch quality {
	case "low":
		for _, x := range []string{"0.28", "0.3", "draft", "fast"} {
			if strings.Contains(hay, x) {
				score += 30
			}
		}
	case "perfect":
		for _, x := range []string{"0.12", "0.1", "fine", "detail", "quality"} {
			if strings.Contains(hay, x) {
				score += 30
			}
		}
	default:
		for _, x := range []string{"0.20", "0.2", "standard", "normal"} {
			if strings.Contains(hay, x) {
				score += 30
			}
		}
	}
	if strings.Contains(hay, "0.4") {
		score += 8
	}
	if strings.Contains(strings.ToLower(filepath.Base(path)), "user") {
		score -= 5
	}
	return score
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
