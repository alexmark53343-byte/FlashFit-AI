package shared

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

// FilamentCalibration contains only values that must be measured on the real
// printer with the actual spool. TDS values are intentionally not accepted as
// substitutes for flow ratio, pressure advance or maximum volumetric speed.
type FilamentCalibration struct {
	Brand              string   `json:"brand"`
	Product            string   `json:"product"`
	Material           string   `json:"material"`
	Variant            string   `json:"variant,omitempty"`
	NozzleTemperature  *float64 `json:"nozzle_temperature,omitempty"`
	MaxVolumetricSpeed *float64 `json:"max_volumetric_speed,omitempty"`
	FlowRatio          *float64 `json:"flow_ratio,omitempty"`
	PressureAdvance    *float64 `json:"pressure_advance,omitempty"`
	Method             string   `json:"method"`
	MeasuredAt         string   `json:"measured_at,omitempty"`
}

type calibrationDocument struct {
	SchemaVersion int                   `json:"schema_version"`
	Calibrations  []FilamentCalibration `json:"calibrations"`
}

func FilamentCalibrationKey(brand, product, material, variant string) string {
	return strings.ToLower(strings.Join([]string{strings.TrimSpace(brand), strings.TrimSpace(product), strings.TrimSpace(material), strings.TrimSpace(variant)}, "|"))
}

func ApplyFilamentCalibration(f Filament, c FilamentCalibration) (Filament, error) {
	if FilamentCalibrationKey(f.Brand, f.Product, f.Material, f.Variant) != FilamentCalibrationKey(c.Brand, c.Product, c.Material, c.Variant) {
		return f, errors.New("calibrazione riferita a un filamento diverso")
	}
	if strings.TrimSpace(c.Method) == "" {
		return f, errors.New("metodo di calibrazione mancante")
	}
	changed := false
	if c.NozzleTemperature != nil {
		if *c.NozzleTemperature < f.NozzleMin || *c.NozzleTemperature > f.NozzleMax || *c.NozzleTemperature > 280 {
			return f, fmt.Errorf("temperatura misurata fuori TDS: %.1f °C", *c.NozzleTemperature)
		}
		f.NozzleDefault, changed = *c.NozzleTemperature, true
	}
	if c.MaxVolumetricSpeed != nil {
		if *c.MaxVolumetricSpeed < 2 || *c.MaxVolumetricSpeed > 32 {
			return f, fmt.Errorf("MVS misurata fuori limite: %.2f mm³/s", *c.MaxVolumetricSpeed)
		}
		f.MaxVolumetricSpeed, changed = *c.MaxVolumetricSpeed, true
	}
	if c.FlowRatio != nil {
		if *c.FlowRatio < 0.85 || *c.FlowRatio > 1.15 {
			return f, fmt.Errorf("flow ratio misurato fuori limite: %.3f", *c.FlowRatio)
		}
		f.FlowRatio, changed = *c.FlowRatio, true
	}
	if c.PressureAdvance != nil {
		if *c.PressureAdvance < 0 || *c.PressureAdvance > 0.20 {
			return f, fmt.Errorf("pressure advance misurato fuori limite: %.3f", *c.PressureAdvance)
		}
		value := *c.PressureAdvance
		f.PressureAdvance, changed = &value, true
	}
	if !changed {
		return f, errors.New("calibrazione senza valori misurati")
	}
	f.MeasuredCalibration = true
	f.Confidence = "misurata sulla bobina e limitata dalla TDS"
	f.Source = strings.TrimSpace(c.Method)
	if c.MeasuredAt != "" {
		f.Source += " • " + c.MeasuredAt
	}
	return f, ValidateFilament(f)
}

// LoadFilamentCalibrations reads an optional local calibration database. An
// invalid entry fails closed instead of silently applying an unsafe value.
func LoadFilamentCalibrations(path string, filaments []Filament) ([]Filament, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return filaments, nil
	}
	if err != nil {
		return nil, err
	}
	if len(b) > 2*1024*1024 {
		return nil, errors.New("database calibrazioni oltre 2 MB")
	}
	var doc calibrationDocument
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	if doc.SchemaVersion != 1 {
		return nil, fmt.Errorf("schema calibrazioni non supportato: %d", doc.SchemaVersion)
	}
	byKey := make(map[string]FilamentCalibration, len(doc.Calibrations))
	for _, c := range doc.Calibrations {
		key := FilamentCalibrationKey(c.Brand, c.Product, c.Material, c.Variant)
		if key == "|||" {
			return nil, errors.New("calibrazione senza identità")
		}
		if _, duplicate := byKey[key]; duplicate {
			return nil, fmt.Errorf("calibrazione duplicata: %s", key)
		}
		byKey[key] = c
	}
	out := append([]Filament(nil), filaments...)
	for i, f := range out {
		if c, ok := byKey[FilamentCalibrationKey(f.Brand, f.Product, f.Material, f.Variant)]; ok {
			calibrated, applyErr := ApplyFilamentCalibration(f, c)
			if applyErr != nil {
				return nil, applyErr
			}
			out[i] = calibrated
		}
	}
	return out, nil
}
