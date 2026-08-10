package main

var pickerTextByLanguage = map[string]map[string]string{
	"fr": {
		"printerEyebrow":     "PROFIL MACHINE",
		"printerTitle":       "Choisir imprimante",
		"printerSubtitle":    "Selectionnez le profil machine et la buse installee.",
		"printerUse":         "Utiliser",
		"printerNozzle":      "Buse",
		"printerBuild":       "Volume",
		"printerBuildUnknown": "Volume n/a",
		"printerMotion":      "Mouvement",
		"printerEnclosed":    "fermee",
		"printerOpenFrame":   "ouverte",
		"printerPreviewOnly": "Apercu visuel affiche uniquement dans ce menu.",
		"filamentEyebrow":    "MATERIAU",
		"filamentSubtitle":   "Recherchez et choisissez le profil filament a utiliser.",
		"manualIntro":        "Selectionnez le slicer, un profil machine Flashforge/Bambu Lab supporte, un profil processus compatible et un profil filament.",
		"chooseMachine":      "2/4 - Profil machine supporte",
	},
	"es": {
		"printerEyebrow":     "PERFIL DE MAQUINA",
		"printerTitle":       "Elegir impresora",
		"printerSubtitle":    "Selecciona el perfil de maquina y la boquilla instalada.",
		"printerUse":         "Usar impresora",
		"printerNozzle":      "Boquilla",
		"printerBuild":       "Volumen",
		"printerBuildUnknown": "Volumen n/d",
		"printerMotion":      "Movimiento",
		"printerEnclosed":    "cerrada",
		"printerOpenFrame":   "abierta",
		"printerPreviewOnly": "Vista visual solo en este menu.",
		"filamentEyebrow":    "MATERIAL",
		"filamentSubtitle":   "Busca y elige el perfil de filamento.",
		"manualIntro":        "Selecciona el slicer, un perfil de maquina Flashforge/Bambu Lab compatible, un perfil de proceso y un perfil de filamento.",
		"chooseMachine":      "2/4 - Perfil de maquina compatible",
	},
	"de": {
		"printerEyebrow":     "MASCHINENPROFIL",
		"printerTitle":       "Drucker waehlen",
		"printerSubtitle":    "Maschinenprofil und installierte Duese waehlen.",
		"printerUse":         "Drucker nutzen",
		"printerNozzle":      "Duese",
		"printerBuild":       "Volumen",
		"printerBuildUnknown": "Volumen n/a",
		"printerMotion":      "Bewegung",
		"printerEnclosed":    "geschlossen",
		"printerOpenFrame":   "offen",
		"printerPreviewOnly": "Visuelle Vorschau nur in diesem Menu.",
		"filamentEyebrow":    "MATERIAL",
		"filamentSubtitle":   "Filamentprofil suchen und waehlen.",
		"manualIntro":        "Slicer, unterstuetztes Flashforge/Bambu Lab Maschinenprofil, Prozessprofil und Filamentprofil waehlen.",
		"chooseMachine":      "2/4 - Unterstuetztes Maschinenprofil",
	},
}

func init() {
	for code, values := range pickerTextByLanguage {
		lang := textByLanguage[code]
		if lang == nil {
			continue
		}
		for key, value := range values {
			lang[key] = value
		}
	}
}
