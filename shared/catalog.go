package shared

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

//go:embed filaments_seed.json
var seedFS embed.FS

type seedDoc struct {
	Filaments []Filament `json:"filaments"`
}

func LoadBuiltinFilaments() ([]Filament, error) {
	b, err := seedFS.ReadFile("filaments_seed.json")
	if err != nil {
		return nil, err
	}
	var d seedDoc
	if err = json.Unmarshal(b, &d); err != nil {
		return nil, err
	}
	out := make([]Filament, 0, len(d.Filaments))
	for _, f := range d.Filaments {
		f = applyTechnicalProfile(f)
		if err := ValidateFilament(f); err == nil {
			out = append(out, f)
		}
	}
	StableFilamentSort(out)
	return out, nil
}

func ValidateFilament(f Filament) error {
	if strings.TrimSpace(f.Brand) == "" || strings.TrimSpace(f.Product) == "" || strings.TrimSpace(f.Material) == "" {
		return fmt.Errorf("filamento senza marca/prodotto/materiale")
	}
	m := strings.ToUpper(f.Material)
	if !strings.HasPrefix(m, "PLA") && !strings.HasPrefix(m, "PETG") {
		return fmt.Errorf("materiale non abilitato nella versione verde: %s", f.Material)
	}
	for _, t := range []string{"CF", "CARBON", "GF", "GLASS", "WOOD", "METAL", "GLOW"} {
		if strings.Contains(m, " "+t) || strings.Contains(m, "-"+t) || strings.Contains(strings.ToUpper(f.Product+" "+f.Variant), t) {
			return fmt.Errorf("filamento abrasivo non abilitato con ugello standard")
		}
	}
	if f.NozzleMin < 160 || f.NozzleMax > 280 || f.NozzleDefault < f.NozzleMin || f.NozzleDefault > f.NozzleMax {
		return fmt.Errorf("temperature ugello non valide")
	}
	if f.BedMin < 0 || f.BedMax > 110 || f.BedDefault < f.BedMin || f.BedDefault > f.BedMax {
		return fmt.Errorf("temperature piano non valide")
	}
	if f.FanMin < 0 || f.FanMax > 100 || f.FanMin > f.FanMax {
		return fmt.Errorf("ventola non valida")
	}
	if f.RecommendedSpeedMax < 0 || f.RecommendedSpeedMax > 600 {
		return fmt.Errorf("velocità lineare TDS non valida")
	}
	if f.MaxVolumetricSpeed < 2 || f.MaxVolumetricSpeed > 32 {
		return fmt.Errorf("portata volumetrica non valida")
	}
	if f.DryTemperature < 0 || f.DryTemperature > 100 || f.DryHours < 0 || f.DryHours > 48 {
		return fmt.Errorf("parametri di essiccazione non validi")
	}
	if f.FlowRatio == 0 {
		f.FlowRatio = 1
	}
	if f.FlowRatio < 0.85 || f.FlowRatio > 1.15 {
		return fmt.Errorf("flow ratio non valido")
	}
	return nil
}

func scalar(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []any:
		if len(x) > 0 {
			return scalar(x[0])
		}
	case []string:
		if len(x) > 0 {
			return x[0]
		}
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case json.Number:
		return x.String()
	case bool:
		if x {
			return "1"
		}
		return "0"
	}
	return ""
}

func floatField(m map[string]any, key string, def float64) float64 {
	v := scalar(m[key])
	if v == "" {
		return def
	}
	n, e := strconv.ParseFloat(v, 64)
	if e != nil {
		return def
	}
	return n
}
func textField(m map[string]any, key string) string { return strings.TrimSpace(scalar(m[key])) }

func likelyProfileRoots() []string {
	var roots []string
	for _, env := range []string{"APPDATA", "LOCALAPPDATA", "ProgramFiles", "ProgramFiles(x86)"} {
		if v := os.Getenv(env); v != "" {
			roots = append(roots,
				filepath.Join(v, "FlashForge"), filepath.Join(v, "Flashforge"), filepath.Join(v, "Flash Studio"), filepath.Join(v, "Orca-Flashforge"), filepath.Join(v, "OrcaSlicer"))
		}
	}
	return roots
}

func ScanOfficialFilaments(maxFiles int) ([]Filament, []string) {
	if maxFiles <= 0 {
		maxFiles = 15000
	}
	seen := map[string]bool{}
	var out []Filament
	var notes []string
	files := 0
	for _, root := range likelyProfileRoots() {
		st, err := os.Stat(root)
		if err != nil || !st.IsDir() {
			continue
		}
		err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			if d.IsDir() {
				n := strings.ToLower(d.Name())
				if n == "cache" || n == "logs" || n == "log" || n == "plugins" {
					return filepath.SkipDir
				}
				return nil
			}
			if files >= maxFiles {
				return fs.SkipAll
			}
			if strings.ToLower(filepath.Ext(path)) != ".json" {
				return nil
			}
			files++
			b, err := os.ReadFile(path)
			if err != nil || len(b) > 4*1024*1024 {
				return nil
			}
			var m map[string]any
			if json.Unmarshal(b, &m) != nil {
				return nil
			}
			typ := strings.ToLower(textField(m, "type"))
			if typ != "filament" {
				return nil
			}
			name := textField(m, "name")
			if name == "" {
				name = textField(m, "filament_settings_id")
			}
			if name == "" {
				return nil
			}
			mat := textField(m, "filament_type")
			if mat == "" {
				mat = textField(m, "material")
			}
			mat = strings.ToUpper(mat)
			if !strings.HasPrefix(mat, "PLA") && !strings.HasPrefix(mat, "PETG") {
				return nil
			}
			brand := "Flash Studio"
			product := name
			for _, bname := range []string{"Flashforge", "SUNLU", "eSUN", "Polymaker", "Bambu Lab", "Prusament", "Overture", "ELEGOO", "Creality", "Anycubic", "ERYONE", "Geeetech", "Spectrum", "ColorFabb", "FormFutura", "Fiberlogy", "Fillamentum", "Extrudr", "AzureFilm", "Amolen", "JAYO"} {
				if strings.Contains(strings.ToLower(name), strings.ToLower(bname)) {
					brand = bname
					product = strings.TrimSpace(strings.ReplaceAll(strings.ToLower(name), strings.ToLower(bname), ""))
					if product == "" {
						product = name
					}
					break
				}
			}
			f := Filament{Brand: brand, Product: product, Material: mat, Variant: "profilo Flash Studio", NozzleMin: floatField(m, "nozzle_temperature_range_low", 180), NozzleMax: floatField(m, "nozzle_temperature_range_high", 280), NozzleDefault: floatField(m, "nozzle_temperature", 220), BedMin: 0, BedMax: 110, BedDefault: floatField(m, "hot_plate_temp", 55), FanMin: floatField(m, "fan_min_speed", 0), FanMax: floatField(m, "fan_max_speed", 100), MaxVolumetricSpeed: floatField(m, "filament_max_volumetric_speed", 12), Density: floatField(m, "filament_density", 1.24), FlowRatio: floatField(m, "filament_flow_ratio", 1), Confidence: "profilo installato nello slicer", Source: "Flash Studio/Orca locale", SourcePath: path, OfficialProfile: true}
			if f.NozzleMin == 0 {
				f.NozzleMin = mathMax(160, f.NozzleDefault-20)
			}
			if f.NozzleMax == 0 {
				f.NozzleMax = mathMin(280, f.NozzleDefault+20)
			}
			if f.BedDefault == 0 {
				if strings.HasPrefix(mat, "PETG") {
					f.BedDefault = 75
				} else {
					f.BedDefault = 55
				}
			}
			f.BedMin = mathMax(0, f.BedDefault-15)
			f.BedMax = mathMin(110, f.BedDefault+15)
			if ValidateFilament(f) != nil {
				return nil
			}
			key := strings.ToLower(f.Brand + "|" + f.Product + "|" + f.Material + "|" + f.SourcePath)
			if !seen[key] {
				seen[key] = true
				out = append(out, f)
			}
			return nil
		})
		if err != nil {
			notes = append(notes, fmt.Sprintf("Scansione profili: %v", err))
		}
	}
	StableFilamentSort(out)
	return out, notes
}

func mathMax(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
func mathMin(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
