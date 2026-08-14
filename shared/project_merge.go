package shared

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Building a complete project configuration.
//
// project_settings.config is not a patch, it is the whole settings state: the
// slicer takes it as authoritative and does not fall back to a profile for keys
// it does not find. Writing only the sixty values FlashFit tunes therefore
// blanked everything else — which is how a project came out with no layer_gcode
// and the slicer refused it, asking for the "G92 E0" its own machine profile
// normally supplies.
//
// So the config is assembled the way the slicer assembles one: start from the
// installed machine, process and filament profiles, resolve what they inherit,
// and lay the FlashFit values on top. Vendor G-code, retraction and calibration
// therefore survive untouched, which is the same guarantee the CLI path gives.

// mergedBaseSettings flattens the installed profiles into one map, in the order
// the slicer would apply them.
func mergedBaseSettings(machine, baseProcess, baseFilament string) map[string]any {
	merged := map[string]any{}
	for _, path := range []string{machine, baseProcess, baseFilament} {
		if strings.TrimSpace(path) == "" {
			continue
		}
		for key, value := range resolveProfileChain(path, 0) {
			merged[key] = value
		}
	}
	return merged
}

// resolveProfileChain reads a profile and everything it inherits, with the
// child's own values winning. Vendor profiles are written as short files that
// inherit from a common base, so reading one on its own yields almost nothing.
func resolveProfileChain(path string, depth int) map[string]any {
	if depth > 8 || strings.TrimSpace(path) == "" {
		return nil
	}
	own, err := readJSONMap(path)
	if err != nil {
		return nil
	}
	parentName := strings.TrimSpace(mapText(own, "inherits"))
	if parentName == "" {
		return own
	}
	// The parent lives beside the child, named after the value of "inherits".
	parentPath := filepath.Join(filepath.Dir(path), parentName+".json")
	if !fileExists(parentPath) {
		return own
	}
	merged := resolveProfileChain(parentPath, depth+1)
	if merged == nil {
		merged = map[string]any{}
	}
	for key, value := range own {
		merged[key] = value
	}
	// "inherits" itself is a directive, not a setting.
	delete(merged, "inherits")
	return merged
}

// projectSettingsJSON renders the complete configuration: the installed
// profiles as the base, the FlashFit recommendation on top.
func projectSettingsJSON(rec Recommendation, printer PrinterProfile, profileName string, machine, baseProcess, baseFilament string) string {
	config := mergedBaseSettings(machine, baseProcess, baseFilament)
	if config == nil {
		config = map[string]any{}
	}
	// Identity of the resulting project.
	config["from"] = "project"
	config["name"] = profileName
	config["print_settings_id"] = profileName
	if _, ok := config["printer_model"]; !ok {
		config["printer_model"] = printer.Model
	}

	for key, value := range rec.Process {
		config[key] = fmt.Sprint(value)
	}
	for key, value := range rec.Filament {
		text := fmt.Sprint(value)
		if filamentArrayKeys[key] {
			config[key] = []string{text}
			continue
		}
		config[key] = text
	}

	// Relative extruder addressing needs the per-layer reset, and losing it is
	// exactly what the slicer complained about. If the merged profiles say the
	// addressing is relative, make sure the reset is there.
	if isTrueish(config["use_relative_e_distances"]) {
		config["layer_gcode"] = ensureExtruderReset(config["layer_gcode"])
	}

	encoded, err := json.MarshalIndent(config, "", "    ")
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func isTrueish(v any) bool {
	switch value := v.(type) {
	case bool:
		return value
	case string:
		s := strings.TrimSpace(strings.ToLower(value))
		return s == "1" || s == "true" || s == "yes"
	case []any:
		if len(value) > 0 {
			return isTrueish(value[0])
		}
	case float64:
		return value != 0
	}
	return false
}

// ensureExtruderReset adds the per-layer extruder reset unless it is already
// there, keeping whatever else the profile puts in that hook.
func ensureExtruderReset(existing any) string {
	text := ""
	switch value := existing.(type) {
	case string:
		text = value
	case []any:
		parts := make([]string, 0, len(value))
		for _, item := range value {
			if s, ok := item.(string); ok {
				parts = append(parts, s)
			}
		}
		text = strings.Join(parts, "\n")
	}
	if strings.Contains(strings.ToUpper(text), "G92 E0") {
		return text
	}
	if strings.TrimSpace(text) == "" {
		return "G92 E0"
	}
	return "G92 E0\n" + text
}

// readSettingsFile is a small helper used by the tests to inspect what was
// written into a project.
func readSettingsFile(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}
