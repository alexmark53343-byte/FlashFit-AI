package shared

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// PrinterProfile is the engineering envelope FlashFit applies on top of the
// vendor profile. Machine G-code, kinematics, retraction, purge/wipe logic and
// vendor calibration always remain inherited from the installed slicer.
type PrinterProfile struct {
	ID                      string     `json:"id"`
	Brand                   string     `json:"brand"`
	Model                   string     `json:"model"`
	Aliases                 []string   `json:"aliases"`
	BuildVolume             [3]float64 `json:"build_volume_mm"`
	NozzleDiameter          float64    `json:"nozzle_diameter_mm"`
	MaxNozzleTemperature    float64    `json:"max_nozzle_temperature_c"`
	MaxBedTemperature       float64    `json:"max_bed_temperature_c"`
	MaxTravelSpeed          float64    `json:"max_travel_speed_mm_s"`
	MaxAcceleration         float64    `json:"max_acceleration_mm_s2"`
	Motion                  string     `json:"motion"`
	Enclosed                bool       `json:"enclosed"`
	MultiMaterial           bool       `json:"multi_material"`
	PreserveToolchange      bool       `json:"preserve_toolchange"`
	PLAEnclosureHeatRisk    bool       `json:"pla_enclosure_heat_risk"`
	OfficialTechnicalSource string     `json:"official_technical_source"`
}

// The catalog covers the current Flash Studio Desktop lineup and every Bambu
// Studio machine family shipped in Bambu Lab's official profile tree.
// Values are hard safety ceilings; the installed machine JSON remains the
// primary authority and may lower them further.
var supportedPrinters = []PrinterProfile{
	{ID: "flashforge-creator-5-pro", Brand: "Flashforge", Model: "Creator 5 Pro", Aliases: []string{"flashforge creator 5 pro", "creator 5 pro", "c5 pro"}, BuildVolume: [3]float64{256, 256, 256}, NozzleDiameter: .4, MaxNozzleTemperature: 320, MaxBedTemperature: 120, MaxTravelSpeed: 600, MaxAcceleration: 30000, Motion: "corexy-toolchanger", Enclosed: true, MultiMaterial: true, PreserveToolchange: true, OfficialTechnicalSource: "https://www.flashforge.com/products/flashforge-creator-5-pro"},
	{ID: "flashforge-creator-5", Brand: "Flashforge", Model: "Creator 5", Aliases: []string{"flashforge creator 5", "creator 5", "c5"}, BuildVolume: [3]float64{256, 256, 256}, NozzleDiameter: .4, MaxNozzleTemperature: 320, MaxBedTemperature: 120, MaxTravelSpeed: 600, MaxAcceleration: 30000, Motion: "corexy-toolchanger", MultiMaterial: true, PreserveToolchange: true, OfficialTechnicalSource: "https://www.flashforge.com/products/flashforge-creator-5"},
	{ID: "flashforge-guider-3-ultra", Brand: "Flashforge", Model: "Guider 3 Ultra", Aliases: []string{"flashforge guider 3 ultra", "guider 3 ultra", "g3 ultra", "g3u"}, BuildVolume: [3]float64{330, 330, 600}, NozzleDiameter: .4, MaxNozzleTemperature: 350, MaxBedTemperature: 120, MaxTravelSpeed: 500, MaxAcceleration: 20000, Motion: "corexy-idex", Enclosed: true, MultiMaterial: true, PreserveToolchange: true, PLAEnclosureHeatRisk: true, OfficialTechnicalSource: "https://enterprise.flashforge.com/pages/guider-3-ultra-3d-printer"},
	{ID: "flashforge-adventurer-5m-pro", Brand: "Flashforge", Model: "Adventurer 5M Pro", Aliases: []string{"flashforge adventurer 5m pro", "adventurer 5m pro", "ad5m pro", "ad5mp"}, BuildVolume: [3]float64{220, 220, 220}, NozzleDiameter: .4, MaxNozzleTemperature: 280, MaxBedTemperature: 110, MaxTravelSpeed: 600, MaxAcceleration: 20000, Motion: "corexy", Enclosed: true, PLAEnclosureHeatRisk: true, OfficialTechnicalSource: "https://www.flashforge.com/products/adventurer-5m-pro-3d-printer"},
	{ID: "flashforge-adventurer-5m", Brand: "Flashforge", Model: "Adventurer 5M", Aliases: []string{"flashforge adventurer 5m", "adventurer 5m", "ad5m"}, BuildVolume: [3]float64{220, 220, 220}, NozzleDiameter: .4, MaxNozzleTemperature: 280, MaxBedTemperature: 110, MaxTravelSpeed: 600, MaxAcceleration: 20000, Motion: "corexy", OfficialTechnicalSource: "https://www.flashforge.com/products/adventurer-5m-3d-printer"},
	{ID: "flashforge-ad5x", Brand: "Flashforge", Model: "AD5X", Aliases: []string{"flashforge ad5x", "ad5x"}, BuildVolume: [3]float64{220, 220, 220}, NozzleDiameter: .4, MaxNozzleTemperature: 300, MaxBedTemperature: 110, MaxTravelSpeed: 600, MaxAcceleration: 20000, Motion: "corexy", MultiMaterial: true, PreserveToolchange: true, OfficialTechnicalSource: "https://www.flashforge.com/products/flashforge-ad5x-3d-printer"},

	{ID: "bambu-h2d-pro", Brand: "Bambu Lab", Model: "H2D Pro", Aliases: []string{"bambu lab h2d pro", "bbl h2dp", "h2d pro"}, BuildVolume: [3]float64{350, 320, 325}, NozzleDiameter: .4, MaxNozzleTemperature: 350, MaxBedTemperature: 120, MaxTravelSpeed: 1000, MaxAcceleration: 20000, Motion: "corexy-dual", Enclosed: true, MultiMaterial: true, PreserveToolchange: true, OfficialTechnicalSource: "https://bambulab.com/en/support/buying-guide"},
	{ID: "bambu-h2d", Brand: "Bambu Lab", Model: "H2D", Aliases: []string{"bambu lab h2d", "bbl h2d", "h2d"}, BuildVolume: [3]float64{350, 320, 325}, NozzleDiameter: .4, MaxNozzleTemperature: 350, MaxBedTemperature: 120, MaxTravelSpeed: 1000, MaxAcceleration: 20000, Motion: "corexy-dual", Enclosed: true, MultiMaterial: true, PreserveToolchange: true, OfficialTechnicalSource: "https://bambulab.com/en/support/buying-guide"},
	{ID: "bambu-h2s", Brand: "Bambu Lab", Model: "H2S", Aliases: []string{"bambu lab h2s", "bbl h2s", "h2s"}, BuildVolume: [3]float64{340, 320, 340}, NozzleDiameter: .4, MaxNozzleTemperature: 350, MaxBedTemperature: 120, MaxTravelSpeed: 1000, MaxAcceleration: 20000, Motion: "corexy", Enclosed: true, MultiMaterial: true, PreserveToolchange: true, OfficialTechnicalSource: "https://bambulab.com/en/support/buying-guide"},
	{ID: "bambu-h2c", Brand: "Bambu Lab", Model: "H2C", Aliases: []string{"bambu lab h2c", "bbl h2c", "h2c"}, BuildVolume: [3]float64{350, 320, 325}, NozzleDiameter: .4, MaxNozzleTemperature: 350, MaxBedTemperature: 120, MaxTravelSpeed: 1000, MaxAcceleration: 20000, Motion: "corexy-toolchanger", Enclosed: true, MultiMaterial: true, PreserveToolchange: true, OfficialTechnicalSource: "https://bambulab.com/en/support/buying-guide"},
	{ID: "bambu-x2d", Brand: "Bambu Lab", Model: "X2D", Aliases: []string{"bambu lab x2d", "bbl x2d", "x2d"}, BuildVolume: [3]float64{256, 256, 256}, NozzleDiameter: .4, MaxNozzleTemperature: 350, MaxBedTemperature: 120, MaxTravelSpeed: 1000, MaxAcceleration: 20000, Motion: "corexy-dual", Enclosed: true, MultiMaterial: true, PreserveToolchange: true, OfficialTechnicalSource: "https://bambulab.com/en/support/buying-guide"},
	{ID: "bambu-p2s", Brand: "Bambu Lab", Model: "P2S", Aliases: []string{"bambu lab p2s", "bbl p2s", "p2s"}, BuildVolume: [3]float64{256, 256, 256}, NozzleDiameter: .4, MaxNozzleTemperature: 300, MaxBedTemperature: 110, MaxTravelSpeed: 600, MaxAcceleration: 20000, Motion: "corexy", Enclosed: true, MultiMaterial: true, PreserveToolchange: true, OfficialTechnicalSource: "https://wiki.bambulab.com/en/p2s/manual/p2s-faq"},
	{ID: "bambu-x1-carbon", Brand: "Bambu Lab", Model: "X1 Carbon", Aliases: []string{"bambu lab x1 carbon", "bbl x1c", "x1 carbon", "x1c"}, BuildVolume: [3]float64{256, 256, 256}, NozzleDiameter: .4, MaxNozzleTemperature: 300, MaxBedTemperature: 120, MaxTravelSpeed: 500, MaxAcceleration: 20000, Motion: "corexy", Enclosed: true, MultiMaterial: true, PreserveToolchange: true, PLAEnclosureHeatRisk: true, OfficialTechnicalSource: "https://bambulab.com/en/support/buying-guide"},
	{ID: "bambu-x1e", Brand: "Bambu Lab", Model: "X1E", Aliases: []string{"bambu lab x1e", "bbl x1e", "x1e"}, BuildVolume: [3]float64{256, 256, 256}, NozzleDiameter: .4, MaxNozzleTemperature: 320, MaxBedTemperature: 120, MaxTravelSpeed: 500, MaxAcceleration: 20000, Motion: "corexy", Enclosed: true, MultiMaterial: true, PreserveToolchange: true, OfficialTechnicalSource: "https://bambulab.com/en/support/buying-guide"},
	{ID: "bambu-x1", Brand: "Bambu Lab", Model: "X1", Aliases: []string{"bambu lab x1", "bbl x1"}, BuildVolume: [3]float64{256, 256, 256}, NozzleDiameter: .4, MaxNozzleTemperature: 300, MaxBedTemperature: 120, MaxTravelSpeed: 500, MaxAcceleration: 20000, Motion: "corexy", Enclosed: true, MultiMaterial: true, PreserveToolchange: true, PLAEnclosureHeatRisk: true, OfficialTechnicalSource: "https://bambulab.com/en/support/buying-guide"},
	{ID: "bambu-p1s", Brand: "Bambu Lab", Model: "P1S", Aliases: []string{"bambu lab p1s", "bbl p1s", "p1s"}, BuildVolume: [3]float64{256, 256, 256}, NozzleDiameter: .4, MaxNozzleTemperature: 300, MaxBedTemperature: 100, MaxTravelSpeed: 500, MaxAcceleration: 20000, Motion: "corexy", Enclosed: true, MultiMaterial: true, PreserveToolchange: true, PLAEnclosureHeatRisk: true, OfficialTechnicalSource: "https://bambulab.com/en/support/buying-guide"},
	{ID: "bambu-p1p", Brand: "Bambu Lab", Model: "P1P", Aliases: []string{"bambu lab p1p", "bbl p1p", "p1p"}, BuildVolume: [3]float64{256, 256, 256}, NozzleDiameter: .4, MaxNozzleTemperature: 300, MaxBedTemperature: 100, MaxTravelSpeed: 500, MaxAcceleration: 20000, Motion: "corexy", MultiMaterial: true, PreserveToolchange: true, OfficialTechnicalSource: "https://bambulab.com/en/support/buying-guide"},
	{ID: "bambu-a2l", Brand: "Bambu Lab", Model: "A2L", Aliases: []string{"bambu lab a2l", "bbl a2l", "a2l"}, BuildVolume: [3]float64{330, 320, 325}, NozzleDiameter: .4, MaxNozzleTemperature: 300, MaxBedTemperature: 100, MaxTravelSpeed: 500, MaxAcceleration: 12000, Motion: "bedslinger", MultiMaterial: true, PreserveToolchange: true, OfficialTechnicalSource: "https://bambulab.com/en/support/buying-guide"},
	{ID: "bambu-a1-mini", Brand: "Bambu Lab", Model: "A1 mini", Aliases: []string{"bambu lab a1 mini", "bbl a1m", "a1 mini", "a1m"}, BuildVolume: [3]float64{180, 180, 180}, NozzleDiameter: .4, MaxNozzleTemperature: 300, MaxBedTemperature: 80, MaxTravelSpeed: 500, MaxAcceleration: 10000, Motion: "bedslinger", MultiMaterial: true, PreserveToolchange: true, OfficialTechnicalSource: "https://bambulab.com/en/support/buying-guide"},
	{ID: "bambu-a1", Brand: "Bambu Lab", Model: "A1", Aliases: []string{"bambu lab a1", "bbl a1"}, BuildVolume: [3]float64{256, 256, 256}, NozzleDiameter: .4, MaxNozzleTemperature: 300, MaxBedTemperature: 100, MaxTravelSpeed: 500, MaxAcceleration: 10000, Motion: "bedslinger", MultiMaterial: true, PreserveToolchange: true, OfficialTechnicalSource: "https://bambulab.com/en/support/buying-guide"},
}

func SupportedPrinters() []PrinterProfile {
	out := append([]PrinterProfile(nil), supportedPrinters...)
	for i := range out {
		out[i].Aliases = append([]string(nil), out[i].Aliases...)
	}
	return out
}

func DefaultPrinterProfile() PrinterProfile {
	for _, p := range supportedPrinters {
		if p.ID == "flashforge-adventurer-5m" {
			return p
		}
	}
	return supportedPrinters[0]
}

func PrinterByID(id string) (PrinterProfile, bool) {
	for _, p := range supportedPrinters {
		if p.ID == id {
			return p, true
		}
	}
	return PrinterProfile{}, false
}

func normalizePrinterText(s string) string {
	return strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return ' '
	}, s)), " ")
}

func containsPrinterAlias(text, alias string) bool {
	text, alias = normalizePrinterText(text), normalizePrinterText(alias)
	if text == "" || alias == "" {
		return false
	}
	return strings.Contains(" "+text+" ", " "+alias+" ")
}

func MatchPrinterText(text string) (PrinterProfile, bool) {
	type match struct {
		profile PrinterProfile
		length  int
	}
	var matches []match
	for _, p := range supportedPrinters {
		for _, alias := range p.Aliases {
			if containsPrinterAlias(text, alias) {
				matches = append(matches, match{p, len(normalizePrinterText(alias))})
			}
		}
	}
	if len(matches) == 0 {
		return PrinterProfile{}, false
	}
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].length > matches[j].length })
	return matches[0].profile, true
}

func PrinterTextMatches(printer PrinterProfile, text string) bool {
	for _, alias := range printer.Aliases {
		if containsPrinterAlias(text, alias) {
			return true
		}
	}
	return false
}

func firstFloat(v any) float64 {
	s := scalar(v)
	n, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return n
}

func machineProfileText(m map[string]any) string {
	return mapText(m, "name") + " " + mapText(m, "printer_model") + " " + mapText(m, "inherits")
}

func resolvePrinterMap(m map[string]any) (PrinterProfile, error) {
	typ := strings.ToLower(mapText(m, "type"))
	if typ != "machine" && typ != "printer" {
		return PrinterProfile{}, errors.New("file non è un profilo macchina")
	}
	p, ok := MatchPrinterText(machineProfileText(m))
	if !ok {
		return PrinterProfile{}, errors.New("profilo macchina non appartenente al catalogo Flashforge/Bambu Lab supportato")
	}
	nozzle := firstFloat(m["nozzle_diameter"])
	if nozzle == 0 {
		nozzle = firstFloat(m["printer_variant"])
	}
	if nozzle == 0 {
		nozzle = p.NozzleDiameter
	}
	if nozzle < 0.1 || nozzle > 1.2 {
		return PrinterProfile{}, fmt.Errorf("profilo %s usa un ugello non valido: %.2f mm", p.Model, nozzle)
	}
	p.NozzleDiameter = nozzle
	if v := firstFloat(m["machine_max_speed_x"]); v > 0 {
		p.MaxTravelSpeed = math.Min(p.MaxTravelSpeed, v)
	}
	if v := firstFloat(m["machine_max_speed_y"]); v > 0 {
		p.MaxTravelSpeed = math.Min(p.MaxTravelSpeed, v)
	}
	if v := firstFloat(m["machine_max_acceleration_x"]); v > 0 {
		p.MaxAcceleration = math.Min(p.MaxAcceleration, v)
	}
	if v := firstFloat(m["machine_max_acceleration_y"]); v > 0 {
		p.MaxAcceleration = math.Min(p.MaxAcceleration, v)
	}
	if v := firstFloat(m["printable_height"]); v > 0 {
		p.BuildVolume[2] = math.Min(p.BuildVolume[2], v)
	}
	return p, nil
}

func ResolvePrinterProfile(path string) (PrinterProfile, error) {
	m, err := readJSONMap(path)
	if err != nil {
		return PrinterProfile{}, fmt.Errorf("profilo macchina illeggibile: %w", err)
	}
	return resolvePrinterMap(m)
}

func ValidateModelForPrinter(a ModelAnalysis, p PrinterProfile) error {
	for i, axis := range []string{"X", "Y", "Z"} {
		if a.Extents[i] > p.BuildVolume[i]+0.01 {
			return fmt.Errorf("modello fuori volume %s: asse %s %.1f mm supera %.1f mm", p.Model, axis, a.Extents[i], p.BuildVolume[i])
		}
	}
	return nil
}
