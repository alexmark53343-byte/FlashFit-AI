package shared

// technicalProfile is a traceable TDS-derived starting point. MVS is a
// deliberately conservative AD5M test baseline, never presented as measured:
// actual color/batch values must come from a volumetric-flow calibration.
type technicalProfile struct {
	NozzleMin, NozzleDefault, NozzleMax float64
	BedMin, BedDefault, BedMax          float64
	FanMin, FanMax                      float64
	SpeedMax, MVS, Density              float64
	DryTemperature, DryHours            float64
	Source                              string
	URLs                                []string
	Notes                               string
}

var technicalProfiles = map[string]technicalProfile{
	FilamentCalibrationKey("Generico", "PLA standard", "PLA", "1.75 mm"): {
		190, 210, 230, 40, 55, 60, 80, 100, 80, 14, 1.24, 50, 6,
		"baseline ingegneristica PLA + limiti AD5M",
		[]string{"https://wiki.flashforge.com/en/adventurer-series/manual/intro-ad5m", "https://doi.org/10.31648/ts.6297"},
		"Punto di partenza prudente; temperatura, MVS, flow e PA richiedono prova sulla bobina reale.",
	},
	FilamentCalibrationKey("Generico", "PLA+ resistente", "PLA+", "1.75 mm"): {
		205, 215, 235, 45, 60, 65, 80, 100, 80, 14, 1.24, 50, 6,
		"baseline ingegneristica PLA+ + limiti AD5M",
		[]string{"https://wiki.flashforge.com/en/adventurer-series/manual/intro-ad5m", "https://doi.org/10.1177/0892705720964560"},
		"Profilo generico conservativo; verificare sempre etichetta e TDS della bobina.",
	},
	FilamentCalibrationKey("Generico", "PLA Silk", "PLA SILK", "1.75 mm"): {
		190, 215, 240, 25, 55, 60, 50, 100, 300, 10, 1.24, 55, 8,
		"Flashforge PLA Silk TDS + guida finitura",
		[]string{"https://eu.flashforge.com/products/pla-silk", "https://wiki.overture3d.com/en/AdvancedGuide/SilkPLA"},
		"La velocità esterna viene limitata per conservare brillantezza e adesione tra strati.",
	},
	FilamentCalibrationKey("Generico", "PLA Matte", "PLA MATTE", "1.75 mm"): {
		190, 210, 230, 40, 55, 65, 80, 100, 100, 14, 1.24, 50, 7,
		"baseline tecnica PLA matte",
		[]string{"https://wiki.overture3d.com/en/Filament/CheatSheet", "https://wiki.polymaker.com/polymaker-wiki/polymaker-wiki-es/productos-polymaker/filamentos-polymaker/panchroma-tm/panchroma-tm-pla-mate"},
		"I matte caricati possono essere leggermente abrasivi; controllare usura dell'ugello nel lungo periodo.",
	},
	FilamentCalibrationKey("Generico", "PETG standard", "PETG", "1.75 mm"): {
		230, 245, 260, 65, 75, 85, 20, 50, 80, 10, 1.27, 60, 6,
		"baseline ingegneristica PETG + limiti AD5M",
		[]string{"https://prusament.com/materials/prusament-petg/", "https://doi.org/10.3390/cryst11080995"},
		"Raffreddamento moderato per non indebolire l'adesione Z; calibrare MVS e retrazione sulla bobina.",
	},
	FilamentCalibrationKey("Flashforge", "PLA Silk+", "PLA SILK", "1.75 mm"): {
		190, 215, 240, 25, 55, 60, 50, 100, 300, 10, 1.24, 55, 8,
		"Flashforge PLA Silk TDS",
		[]string{"https://eu.flashforge.com/products/pla-silk", "https://enterprise.flashforge.com/products/pla-silk"},
		"Flashforge dichiara che la lucentezza cala alle alte velocità; pareti esterne limitate automaticamente.",
	},
	FilamentCalibrationKey("eSUN", "PLA+", "PLA+", "1.75 mm"): {
		210, 215, 230, 45, 55, 60, 100, 100, 100, 14, 1.23, 50, 6,
		"eSUN ePLA+ TDS",
		[]string{"https://www.esun3d.com/es/news/7222/"},
		"Profilo ePLA+ standard, non ePLA-HS; MVS mantenuta prudente finché non viene misurata.",
	},
	FilamentCalibrationKey("eSUN", "PETG", "PETG", "1.75 mm"): {
		240, 250, 260, 75, 80, 90, 50, 100, 200, 12, 1.27, 60, 6,
		"eSUN ePETG TDS",
		[]string{"https://www.esun3d.com/epetg-lite-product/"},
		"Il tetto di velocità del produttore non sostituisce la MVS misurata sull'hotend AD5M.",
	},
	FilamentCalibrationKey("SUNLU", "PLA", "PLA", "1.75 mm"): {
		200, 210, 230, 50, 55, 65, 90, 100, 100, 14, 1.24, 50, 6,
		"SUNLU PLA TDS",
		[]string{"https://store.sunlu.com/hu-de/collections/new-collection/products/5kg-large-spool-pla-abs-petg-and-pla-3d-printer-filament"},
		"Intervallo e velocità dal produttore; MVS da confermare per colore e lotto.",
	},
	FilamentCalibrationKey("SUNLU", "PLA+", "PLA+", "1.75 mm"): {
		210, 220, 235, 55, 60, 65, 90, 100, 100, 14, 1.21, 50, 6,
		"SUNLU PLA+ TDS",
		[]string{"https://www.sunlu.com/it/products/pla-plus-filament", "https://store.sunlu.com/hu-de/collections/new-collection/products/5kg-large-spool-pla-abs-petg-and-pla-3d-printer-filament"},
		"Temperatura scelta al centro della fascia documentata per 50–100 mm/s.",
	},
	FilamentCalibrationKey("SUNLU", "PETG", "PETG", "1.75 mm"): {
		240, 250, 260, 60, 65, 70, 20, 50, 200, 10, 1.27, 60, 6,
		"SUNLU PETG TDS",
		[]string{"https://uk.store.sunlu.com/products/petg-filament-3kg-large-spool-3d-printer-filament-3kg"},
		"Velocità marketing ignorata come MVS; baseline volumetrica prudente per qualità e adesione.",
	},
	FilamentCalibrationKey("Polymaker", "PolyLite PLA", "PLA", "1.75 mm"): {
		190, 210, 230, 25, 55, 60, 90, 100, 200, 14, 1.17, 55, 6,
		"Polymaker PolyLite PLA TDS",
		[]string{"https://wiki.polymaker.com/polymaker-products/more-about-our-products/documents/technical-data-sheets/pla/polylite-tm-pla"},
		"Il TDS usa provini ISO; i valori meccanici non sono trasferiti direttamente a qualunque geometria.",
	},
	FilamentCalibrationKey("Polymaker", "PolyTerra PLA", "PLA MATTE", "1.75 mm"): {
		190, 210, 230, 25, 55, 60, 90, 100, 300, 16, 1.31, 55, 6,
		"Polymaker PolyTerra / Panchroma Matte TDS",
		[]string{"https://polymaker.com/introducing-polyterra-pla/", "https://shop.polymaker.com/products/panchroma-dual-matte"},
		"PolyTerra è ora Panchroma Matte; leggermente più abrasivo del PLA normale.",
	},
	FilamentCalibrationKey("Polymaker", "PolyLite PETG", "PETG", "1.75 mm"): {
		230, 245, 260, 70, 75, 80, 0, 20, 100, 10, 1.25, 65, 6,
		"Polymaker PolyLite PETG TDS",
		[]string{"https://wiki.polymaker.com/polymaker-products/more-about-our-products/documents/technical-data-sheets/petg-pet/polylite-tm-petg"},
		"Ventola OFF–20% come da TDS per proteggere l'adesione tra strati.",
	},
	FilamentCalibrationKey("Bambu Lab", "PLA Basic", "PLA", "1.75 mm"): {
		190, 215, 230, 35, 45, 45, 90, 100, 300, 18, 1.24, 55, 8,
		"Bambu Lab PLA Basic TDS",
		[]string{"https://jp.store.bambulab.com/en/products/pla-basic-gradient"},
		"Il profilo Basic Gradient rimanda esplicitamente alle impostazioni PLA Basic.",
	},
	FilamentCalibrationKey("Bambu Lab", "PLA Matte", "PLA MATTE", "1.75 mm"): {
		190, 210, 230, 35, 45, 45, 90, 100, 300, 16, 1.31, 55, 8,
		"Bambu Lab PLA Matte TDS",
		[]string{"https://ca.store.bambulab.com/collections/filament-membership-product/products/pla-matte"},
		"Baseline conservativa; il profilo Flash Studio locale prevale se disponibile.",
	},
	FilamentCalibrationKey("Prusament", "PLA", "PLA", "1.75 mm"): {
		200, 210, 220, 40, 55, 60, 100, 100, 0, 14, 1.24, 50, 6,
		"Prusament PLA material guide",
		[]string{"https://prusament.com/materials/pla/"},
		"Ugello 210±10 °C, piano 40–60 °C e ventola 100% come da guida ufficiale.",
	},
	FilamentCalibrationKey("Prusament", "PETG", "PETG", "1.75 mm"): {
		240, 250, 260, 70, 80, 90, 30, 50, 0, 10, 1.27, 60, 6,
		"Prusament PETG material guide",
		[]string{"https://prusament.com/materials/prusament-petg/"},
		"Per massima resistenza la guida indica meno ventola; l'app mantiene raffreddamento moderato per qualità generale.",
	},
	FilamentCalibrationKey("Overture", "PLA", "PLA", "1.75 mm"): {
		190, 205, 220, 25, 55, 60, 90, 100, 70, 12, 1.21, 50, 7,
		"Overture PLA TDS e guide tecniche",
		[]string{"https://wiki.overture3d.com/en/Filament/PLA", "https://wiki.overture3d.com/en/Basics/DryingGuide"},
		"Velocità limitata alla raccomandazione 40–70 mm/s della pagina tecnica PLA.",
	},
	FilamentCalibrationKey("Overture", "Matte PLA", "PLA MATTE", "1.75 mm"): {
		190, 210, 230, 50, 60, 70, 90, 100, 200, 12, 1.21, 50, 7,
		"Overture Matte PLA technical guide",
		[]string{"https://wiki.overture3d.com/en/Filament/PLAVariants", "https://wiki.overture3d.com/en/Filament/CheatSheet"},
		"Matte leggermente abrasivo; ugello temprato consigliato dal produttore per uso prolungato.",
	},
	FilamentCalibrationKey("Overture", "PETG", "PETG", "1.75 mm"): {
		230, 245, 260, 65, 70, 70, 20, 50, 90, 10, 1.25, 60, 5,
		"Overture PETG TDS e guide tecniche",
		[]string{"https://wiki.overture3d.com/en/Filament/PETG", "https://wiki.overture3d.com/en/Basics/DryingGuide"},
		"Ventola moderata e prima fase senza raffreddamento per limitare warping e perdita di adesione Z.",
	},
	FilamentCalibrationKey("ELEGOO", "PLA", "PLA", "1.75 mm"): {
		190, 210, 230, 35, 55, 65, 90, 100, 300, 14, 1.26, 50, 8,
		"ELEGOO PLA TDS",
		[]string{"https://eu.elegoo.com/en-be/products/pla-filament-1-75mm-colored-1kg"},
		"Densità e proprietà XY/Z dal TDS ufficiale; MVS resta una baseline da misurare.",
	},
	FilamentCalibrationKey("ELEGOO", "PETG Pro", "PETG", "1.75 mm"): {
		230, 245, 260, 65, 70, 75, 20, 60, 270, 12, 1.27, 60, 8,
		"ELEGOO PETG Pro TDS",
		[]string{"https://eu.elegoo.com/en-pt/products/petg-pro-filament-1-75mm-colored-1kg"},
		"Essiccazione richiesta dal produttore; conservare sotto 20% RH per risultati ripetibili.",
	},
}

func applyTechnicalProfile(f Filament) Filament {
	p, ok := technicalProfiles[FilamentCalibrationKey(f.Brand, f.Product, f.Material, f.Variant)]
	if !ok {
		return f
	}
	f.NozzleMin, f.NozzleDefault, f.NozzleMax = p.NozzleMin, p.NozzleDefault, p.NozzleMax
	f.BedMin, f.BedDefault, f.BedMax = p.BedMin, p.BedDefault, p.BedMax
	f.FanMin, f.FanMax = p.FanMin, p.FanMax
	f.RecommendedSpeedMax, f.MaxVolumetricSpeed, f.Density = p.SpeedMax, p.MVS, p.Density
	f.DryTemperature, f.DryHours = p.DryTemperature, p.DryHours
	f.Confidence, f.Source, f.TechnicalSources, f.Notes = "TDS ufficiale + margine AD5M; calibrazione bobina richiesta", p.Source, append([]string(nil), p.URLs...), p.Notes
	f.FlowRatio, f.PressureAdvance, f.MeasuredCalibration = 1, nil, false
	return f
}
