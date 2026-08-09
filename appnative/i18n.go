package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const defaultLanguage = "it"

var uiLanguage = defaultLanguage

var languageNames = map[string]string{
	"it": "Italiano",
	"en": "English",
	"fr": "Français",
	"es": "Español",
	"de": "Deutsch",
}

// English is the final fallback. Every other language is checked by tests to
// ensure that a language change never leaves an English/Italian UI fragment.
var textByLanguage = map[string]map[string]string{
	"it": {
		"appTitle":               "FlashFit AI Spatial",
		"device":                 "Adventurer 5M",
		"nozzle":                 "Ugello 0,4 mm",
		"language":               "Lingua",
		"advanced":               "Avanzate",
		"stepModel":              "Importa modello",
		"stepFilament":           "Scegli filamento",
		"stepQuality":            "Qualità di stampa",
		"stepOpen":               "Apri in Flash Studio",
		"dropTitle":              "Trascina e rilascia qui il tuo modello",
		"dropSubtitle":           "STL, OBJ o 3MF",
		"chooseModel":            "Scegli modello…",
		"noModel":                "Nessun modello selezionato",
		"modelReady":             "Modello pronto",
		"analyzing":              "Analisi in corso…",
		"filamentSelected":       "Selezionato",
		"noFilament":             "Nessun filamento",
		"qualityFast":            "Veloce",
		"qualityBalanced":        "Bilanciata",
		"qualityPerfect":         "Perfetta",
		"openFlash":              "Apri in Flash Studio",
		"ready":                  "Pronto per analizzare",
		"readyOpen":              "Tutto pronto per Flash Studio",
		"statusCatalog":          "Catalogo caricato: %d filamenti. Rilevo Flash Studio…",
		"statusDiscovering":      "Rilevamento di Flash Studio e dei profili…",
		"statusDiscoveryDone":    "Rilevamento completato: %d filamenti e %d profili qualità.",
		"statusAnalyzing":        "Analisi protetta del modello in corso…",
		"statusAnalysisCanceled": "Analisi annullata.",
		"statusAnalysisFailed":   "Analisi non riuscita: %s",
		"statusAnalysisBlocked":  "Modello bloccato: %s",
		"statusAnalysisReady":    "Modello analizzato. Scegli filamento e qualità.",
		"statusCancelImport":     "Annullamento importazione…",
		"statusCancelAnalysis":   "Annullamento analisi…",
		"statusImporting":        "Flash Studio sta creando e verificando il progetto 3MF…",
		"statusImportCanceled":   "Importazione annullata: %s",
		"statusImportDone":       "Progetto verificato e aperto: %s",
		"selectModelTitle":       "Scegli un modello 3D",
		"supportedModels":        "Modelli supportati",
		"allFiles":               "Tutti i file",
		"waitImport":             "Attendi o annulla l'importazione prima di cambiare modello.",
		"unsupportedFormat":      "Formato non supportato. Usa STL, OBJ oppure 3MF.",
		"modelMissing":           "Il modello selezionato non esiste o non è leggibile:\n\n%s%s",
		"modelUnavailable":       "Modello non disponibile",
		"windowsDetail":          "\n\nDettaglio Windows: %s",
		"analyzeFirst":           "Analizza prima il modello.",
		"profilesMissing":        "Mancano Flash Studio o uno dei profili base completi.",
		"importBlocked":          "Importazione bloccata",
		"modelRejected":          "Modello rifiutato",
		"internalError":          "FlashFit AI ha rilevato un errore interno. Il log è stato salvato in:\n%s",
		"alreadyRunning":         "FlashFit AI è già aperto. Controlla la barra delle applicazioni.",
		"databaseError":          "Impossibile caricare il database filamenti: %s",
		"importCanceledTitle":    "Importazione annullata in sicurezza",
		"importCanceledBody":     "Nessun progetto è stato aperto.\n\n%s\n\nIl modello originale non è stato modificato.",
		"importDoneTitle":        "Importazione completata",
		"importDoneBody":         "Progetto 3MF creato, riletto e verificato:\n\n%s\n\nControlla l'anteprima layer prima di stampare.",
		"cancel":                 "Annulla",
		"cancelAnalysis":         "Annulla analisi",
		"working":                "GENERAZIONE E VERIFICA…",
		"filamentTitle":          "Scegli filamento",
		"searchFilament":         "Cerca marca, linea o materiale",
		"useFilament":            "Usa questo filamento",
		"close":                  "Chiudi",
		"internal":               "interno",
		"fromBase":               "dal profilo base",
		"localProfile":           "profilo locale Flash Studio: %s",
		"material":               "Materiale",
		"variant":                "Variante",
		"nozzleTemp":             "Ugello",
		"bedTemp":                "Piano",
		"reliability":            "Affidabilità",
		"source":                 "Fonte",
		"shownResults":           "Mostrati %d di %d risultati. Restringi la ricerca.",
		"analysisSummary":        "%s • %d triangoli • %.1f × %.1f × %.1f mm",
		"supportsOn":             "Supporti consigliati",
		"supportsOff":            "Supporti non necessari",
		"scanAgain":              "Rileva di nuovo Flash Studio",
		"manualProfiles":         "Configura profili manualmente",
		"openLog":                "Apri cartella diagnostica",
		"manualIntro":            "Seleziona in ordine Flash Studio.exe, il profilo macchina AD5M 0.4, il profilo processo e il profilo filamento. Annullando una finestra conserverai il valore precedente.",
		"manualTitle":            "Configurazione manuale",
		"chooseSlicer":           "1/4 • Scegli Flash Studio.exe",
		"chooseMachine":          "2/4 • Profilo macchina Adventurer 5M 0.4",
		"chooseProcess":          "3/4 • Profilo processo base AD5M",
		"chooseBaseFilament":     "4/4 • Profilo filamento base",
		"programs":               "Programmi",
		"jsonProfiles":           "Profili JSON",
	},
	"en": {
		"appTitle": "FlashFit AI Spatial", "device": "Adventurer 5M", "nozzle": "0.4 mm nozzle", "language": "Language", "advanced": "Advanced",
		"stepModel": "Import model", "stepFilament": "Choose filament", "stepQuality": "Print quality", "stepOpen": "Open in Flash Studio",
		"dropTitle": "Drag and drop your model here", "dropSubtitle": "STL, OBJ or 3MF", "chooseModel": "Choose model…", "noModel": "No model selected", "modelReady": "Model ready", "analyzing": "Analyzing…",
		"filamentSelected": "Selected", "noFilament": "No filament", "qualityFast": "Fast", "qualityBalanced": "Balanced", "qualityPerfect": "Perfect", "openFlash": "Open in Flash Studio",
		"ready": "Ready to analyze", "readyOpen": "Everything is ready for Flash Studio", "statusCatalog": "Catalog loaded: %d filaments. Detecting Flash Studio…", "statusDiscovering": "Detecting Flash Studio and installed profiles…", "statusDiscoveryDone": "Detection complete: %d filaments and %d quality profiles.",
		"statusAnalyzing": "Protected model analysis in progress…", "statusAnalysisCanceled": "Analysis canceled.", "statusAnalysisFailed": "Analysis failed: %s", "statusAnalysisBlocked": "Model blocked: %s", "statusAnalysisReady": "Model analyzed. Choose filament and quality.",
		"statusCancelImport": "Canceling import…", "statusCancelAnalysis": "Canceling analysis…", "statusImporting": "Flash Studio is creating and verifying the 3MF project…", "statusImportCanceled": "Import canceled: %s", "statusImportDone": "Verified project opened: %s",
		"selectModelTitle": "Choose a 3D model", "supportedModels": "Supported models", "allFiles": "All files", "waitImport": "Wait for or cancel the import before changing model.", "unsupportedFormat": "Unsupported format. Use STL, OBJ or 3MF.", "modelMissing": "The selected model does not exist or cannot be read:\n\n%s%s", "modelUnavailable": "Model unavailable", "windowsDetail": "\n\nWindows detail: %s",
		"analyzeFirst": "Analyze the model first.", "profilesMissing": "Flash Studio or one of the complete base profiles is missing.", "importBlocked": "Import blocked", "modelRejected": "Model rejected", "internalError": "FlashFit AI detected an internal error. The log was saved to:\n%s", "alreadyRunning": "FlashFit AI is already open. Check the taskbar.", "databaseError": "Unable to load the filament database: %s",
		"importCanceledTitle": "Import canceled safely", "importCanceledBody": "No project was opened.\n\n%s\n\nThe original model was not modified.", "importDoneTitle": "Import complete", "importDoneBody": "The 3MF project was created, read back and verified:\n\n%s\n\nCheck the layer preview before printing.",
		"cancel": "Cancel", "cancelAnalysis": "Cancel analysis", "working": "GENERATING AND VERIFYING…", "filamentTitle": "Choose filament", "searchFilament": "Search brand, line or material", "useFilament": "Use this filament", "close": "Close", "internal": "built-in", "fromBase": "from base profile", "localProfile": "local Flash Studio profile: %s",
		"material": "Material", "variant": "Variant", "nozzleTemp": "Nozzle", "bedTemp": "Bed", "reliability": "Reliability", "source": "Source", "shownResults": "Showing %d of %d results. Narrow your search.", "analysisSummary": "%s • %d triangles • %.1f × %.1f × %.1f mm", "supportsOn": "Supports recommended", "supportsOff": "Supports not required",
		"scanAgain": "Detect Flash Studio again", "manualProfiles": "Configure profiles manually", "openLog": "Open diagnostics folder", "manualIntro": "Select Flash Studio.exe, the AD5M 0.4 machine profile, the process profile and the filament profile in order. Canceling a window keeps the previous value.", "manualTitle": "Manual configuration", "chooseSlicer": "1/4 • Choose Flash Studio.exe", "chooseMachine": "2/4 • Adventurer 5M 0.4 machine profile", "chooseProcess": "3/4 • AD5M base process profile", "chooseBaseFilament": "4/4 • Base filament profile", "programs": "Programs", "jsonProfiles": "JSON profiles",
	},
	"fr": {
		"appTitle": "FlashFit AI Spatial", "device": "Adventurer 5M", "nozzle": "Buse 0,4 mm", "language": "Langue", "advanced": "Avancé",
		"stepModel": "Importer le modèle", "stepFilament": "Choisir le filament", "stepQuality": "Qualité d'impression", "stepOpen": "Ouvrir dans Flash Studio", "dropTitle": "Glissez-déposez votre modèle ici", "dropSubtitle": "STL, OBJ ou 3MF", "chooseModel": "Choisir un modèle…", "noModel": "Aucun modèle sélectionné", "modelReady": "Modèle prêt", "analyzing": "Analyse…",
		"filamentSelected": "Sélectionné", "noFilament": "Aucun filament", "qualityFast": "Rapide", "qualityBalanced": "Équilibrée", "qualityPerfect": "Parfaite", "openFlash": "Ouvrir dans Flash Studio", "ready": "Prêt à analyser", "readyOpen": "Tout est prêt pour Flash Studio",
		"statusCatalog": "Catalogue chargé : %d filaments. Détection de Flash Studio…", "statusDiscovering": "Détection de Flash Studio et des profils…", "statusDiscoveryDone": "Détection terminée : %d filaments et %d profils qualité.", "statusAnalyzing": "Analyse protégée du modèle…", "statusAnalysisCanceled": "Analyse annulée.", "statusAnalysisFailed": "Échec de l'analyse : %s", "statusAnalysisBlocked": "Modèle bloqué : %s", "statusAnalysisReady": "Modèle analysé. Choisissez filament et qualité.", "statusCancelImport": "Annulation de l'importation…", "statusCancelAnalysis": "Annulation de l'analyse…", "statusImporting": "Flash Studio crée et vérifie le projet 3MF…", "statusImportCanceled": "Importation annulée : %s", "statusImportDone": "Projet vérifié ouvert : %s",
		"selectModelTitle": "Choisir un modèle 3D", "supportedModels": "Modèles pris en charge", "allFiles": "Tous les fichiers", "waitImport": "Attendez ou annulez l'importation avant de changer de modèle.", "unsupportedFormat": "Format non pris en charge. Utilisez STL, OBJ ou 3MF.", "modelMissing": "Le modèle sélectionné n'existe pas ou n'est pas lisible :\n\n%s%s", "modelUnavailable": "Modèle indisponible", "windowsDetail": "\n\nDétail Windows : %s", "analyzeFirst": "Analysez d'abord le modèle.", "profilesMissing": "Flash Studio ou un profil de base complet manque.", "importBlocked": "Importation bloquée", "modelRejected": "Modèle refusé", "internalError": "FlashFit AI a détecté une erreur interne. Journal enregistré dans :\n%s", "alreadyRunning": "FlashFit AI est déjà ouvert. Vérifiez la barre des tâches.", "databaseError": "Impossible de charger la base de filaments : %s",
		"importCanceledTitle": "Importation annulée en sécurité", "importCanceledBody": "Aucun projet n'a été ouvert.\n\n%s\n\nLe modèle original n'a pas été modifié.", "importDoneTitle": "Importation terminée", "importDoneBody": "Projet 3MF créé, relu et vérifié :\n\n%s\n\nVérifiez l'aperçu des couches avant d'imprimer.", "cancel": "Annuler", "cancelAnalysis": "Annuler l'analyse", "working": "GÉNÉRATION ET VÉRIFICATION…",
		"filamentTitle": "Choisir le filament", "searchFilament": "Rechercher marque, gamme ou matériau", "useFilament": "Utiliser ce filament", "close": "Fermer", "internal": "interne", "fromBase": "depuis le profil de base", "localProfile": "profil Flash Studio local : %s", "material": "Matériau", "variant": "Variante", "nozzleTemp": "Buse", "bedTemp": "Plateau", "reliability": "Fiabilité", "source": "Source", "shownResults": "%d résultats affichés sur %d. Affinez la recherche.", "analysisSummary": "%s • %d triangles • %.1f × %.1f × %.1f mm", "supportsOn": "Supports recommandés", "supportsOff": "Supports non nécessaires",
		"scanAgain": "Redétecter Flash Studio", "manualProfiles": "Configurer les profils manuellement", "openLog": "Ouvrir le dossier de diagnostic", "manualIntro": "Sélectionnez dans l'ordre Flash Studio.exe, le profil machine AD5M 0.4, le profil de processus et le profil de filament. Annuler conserve la valeur précédente.", "manualTitle": "Configuration manuelle", "chooseSlicer": "1/4 • Choisir Flash Studio.exe", "chooseMachine": "2/4 • Profil machine Adventurer 5M 0.4", "chooseProcess": "3/4 • Profil processus AD5M", "chooseBaseFilament": "4/4 • Profil filament de base", "programs": "Programmes", "jsonProfiles": "Profils JSON",
	},
	"es": {
		"appTitle": "FlashFit AI Spatial", "device": "Adventurer 5M", "nozzle": "Boquilla 0,4 mm", "language": "Idioma", "advanced": "Avanzado",
		"stepModel": "Importar modelo", "stepFilament": "Elegir filamento", "stepQuality": "Calidad de impresión", "stepOpen": "Abrir en Flash Studio", "dropTitle": "Arrastra y suelta aquí tu modelo", "dropSubtitle": "STL, OBJ o 3MF", "chooseModel": "Elegir modelo…", "noModel": "Ningún modelo seleccionado", "modelReady": "Modelo listo", "analyzing": "Analizando…",
		"filamentSelected": "Seleccionado", "noFilament": "Ningún filamento", "qualityFast": "Rápida", "qualityBalanced": "Equilibrada", "qualityPerfect": "Perfecta", "openFlash": "Abrir en Flash Studio", "ready": "Listo para analizar", "readyOpen": "Todo listo para Flash Studio",
		"statusCatalog": "Catálogo cargado: %d filamentos. Detectando Flash Studio…", "statusDiscovering": "Detectando Flash Studio y perfiles…", "statusDiscoveryDone": "Detección completa: %d filamentos y %d perfiles de calidad.", "statusAnalyzing": "Análisis protegido del modelo…", "statusAnalysisCanceled": "Análisis cancelado.", "statusAnalysisFailed": "Error de análisis: %s", "statusAnalysisBlocked": "Modelo bloqueado: %s", "statusAnalysisReady": "Modelo analizado. Elige filamento y calidad.", "statusCancelImport": "Cancelando importación…", "statusCancelAnalysis": "Cancelando análisis…", "statusImporting": "Flash Studio está creando y verificando el proyecto 3MF…", "statusImportCanceled": "Importación cancelada: %s", "statusImportDone": "Proyecto verificado abierto: %s",
		"selectModelTitle": "Elegir un modelo 3D", "supportedModels": "Modelos compatibles", "allFiles": "Todos los archivos", "waitImport": "Espera o cancela la importación antes de cambiar el modelo.", "unsupportedFormat": "Formato no compatible. Usa STL, OBJ o 3MF.", "modelMissing": "El modelo seleccionado no existe o no se puede leer:\n\n%s%s", "modelUnavailable": "Modelo no disponible", "windowsDetail": "\n\nDetalle de Windows: %s", "analyzeFirst": "Analiza primero el modelo.", "profilesMissing": "Falta Flash Studio o uno de los perfiles base completos.", "importBlocked": "Importación bloqueada", "modelRejected": "Modelo rechazado", "internalError": "FlashFit AI detectó un error interno. Registro guardado en:\n%s", "alreadyRunning": "FlashFit AI ya está abierto. Revisa la barra de tareas.", "databaseError": "No se pudo cargar la base de filamentos: %s",
		"importCanceledTitle": "Importación cancelada con seguridad", "importCanceledBody": "No se abrió ningún proyecto.\n\n%s\n\nEl modelo original no se modificó.", "importDoneTitle": "Importación completada", "importDoneBody": "Proyecto 3MF creado, releído y verificado:\n\n%s\n\nComprueba la vista previa de capas antes de imprimir.", "cancel": "Cancelar", "cancelAnalysis": "Cancelar análisis", "working": "GENERANDO Y VERIFICANDO…",
		"filamentTitle": "Elegir filamento", "searchFilament": "Buscar marca, línea o material", "useFilament": "Usar este filamento", "close": "Cerrar", "internal": "interno", "fromBase": "del perfil base", "localProfile": "perfil local de Flash Studio: %s", "material": "Material", "variant": "Variante", "nozzleTemp": "Boquilla", "bedTemp": "Cama", "reliability": "Fiabilidad", "source": "Fuente", "shownResults": "Mostrando %d de %d resultados. Limita la búsqueda.", "analysisSummary": "%s • %d triángulos • %.1f × %.1f × %.1f mm", "supportsOn": "Soportes recomendados", "supportsOff": "Soportes no necesarios",
		"scanAgain": "Detectar Flash Studio de nuevo", "manualProfiles": "Configurar perfiles manualmente", "openLog": "Abrir carpeta de diagnóstico", "manualIntro": "Selecciona en orden Flash Studio.exe, el perfil de máquina AD5M 0.4, el perfil de proceso y el perfil de filamento. Cancelar conserva el valor anterior.", "manualTitle": "Configuración manual", "chooseSlicer": "1/4 • Elegir Flash Studio.exe", "chooseMachine": "2/4 • Perfil de máquina Adventurer 5M 0.4", "chooseProcess": "3/4 • Perfil de proceso AD5M", "chooseBaseFilament": "4/4 • Perfil de filamento base", "programs": "Programas", "jsonProfiles": "Perfiles JSON",
	},
	"de": {
		"appTitle": "FlashFit AI Spatial", "device": "Adventurer 5M", "nozzle": "0,4-mm-Düse", "language": "Sprache", "advanced": "Erweitert",
		"stepModel": "Modell importieren", "stepFilament": "Filament wählen", "stepQuality": "Druckqualität", "stepOpen": "In Flash Studio öffnen", "dropTitle": "Modell hierher ziehen und ablegen", "dropSubtitle": "STL, OBJ oder 3MF", "chooseModel": "Modell wählen…", "noModel": "Kein Modell ausgewählt", "modelReady": "Modell bereit", "analyzing": "Analyse…",
		"filamentSelected": "Ausgewählt", "noFilament": "Kein Filament", "qualityFast": "Schnell", "qualityBalanced": "Ausgewogen", "qualityPerfect": "Perfekt", "openFlash": "In Flash Studio öffnen", "ready": "Bereit zur Analyse", "readyOpen": "Alles bereit für Flash Studio",
		"statusCatalog": "Katalog geladen: %d Filamente. Flash Studio wird erkannt…", "statusDiscovering": "Flash Studio und Profile werden erkannt…", "statusDiscoveryDone": "Erkennung abgeschlossen: %d Filamente und %d Qualitätsprofile.", "statusAnalyzing": "Geschützte Modellanalyse läuft…", "statusAnalysisCanceled": "Analyse abgebrochen.", "statusAnalysisFailed": "Analyse fehlgeschlagen: %s", "statusAnalysisBlocked": "Modell blockiert: %s", "statusAnalysisReady": "Modell analysiert. Filament und Qualität wählen.", "statusCancelImport": "Import wird abgebrochen…", "statusCancelAnalysis": "Analyse wird abgebrochen…", "statusImporting": "Flash Studio erstellt und prüft das 3MF-Projekt…", "statusImportCanceled": "Import abgebrochen: %s", "statusImportDone": "Geprüftes Projekt geöffnet: %s",
		"selectModelTitle": "3D-Modell wählen", "supportedModels": "Unterstützte Modelle", "allFiles": "Alle Dateien", "waitImport": "Warten Sie auf den Import oder brechen Sie ihn ab, bevor Sie das Modell wechseln.", "unsupportedFormat": "Nicht unterstütztes Format. Verwenden Sie STL, OBJ oder 3MF.", "modelMissing": "Das ausgewählte Modell existiert nicht oder ist nicht lesbar:\n\n%s%s", "modelUnavailable": "Modell nicht verfügbar", "windowsDetail": "\n\nWindows-Detail: %s", "analyzeFirst": "Analysieren Sie zuerst das Modell.", "profilesMissing": "Flash Studio oder ein vollständiges Basisprofil fehlt.", "importBlocked": "Import blockiert", "modelRejected": "Modell abgelehnt", "internalError": "FlashFit AI hat einen internen Fehler erkannt. Protokoll gespeichert unter:\n%s", "alreadyRunning": "FlashFit AI ist bereits geöffnet. Prüfen Sie die Taskleiste.", "databaseError": "Filamentdatenbank konnte nicht geladen werden: %s",
		"importCanceledTitle": "Import sicher abgebrochen", "importCanceledBody": "Kein Projekt wurde geöffnet.\n\n%s\n\nDas Originalmodell wurde nicht verändert.", "importDoneTitle": "Import abgeschlossen", "importDoneBody": "3MF-Projekt erstellt, erneut gelesen und geprüft:\n\n%s\n\nPrüfen Sie vor dem Drucken die Ebenenvorschau.", "cancel": "Abbrechen", "cancelAnalysis": "Analyse abbrechen", "working": "ERSTELLUNG UND PRÜFUNG…",
		"filamentTitle": "Filament wählen", "searchFilament": "Marke, Produktlinie oder Material suchen", "useFilament": "Dieses Filament verwenden", "close": "Schließen", "internal": "integriert", "fromBase": "aus dem Basisprofil", "localProfile": "lokales Flash-Studio-Profil: %s", "material": "Material", "variant": "Variante", "nozzleTemp": "Düse", "bedTemp": "Bett", "reliability": "Zuverlässigkeit", "source": "Quelle", "shownResults": "%d von %d Ergebnissen. Suche eingrenzen.", "analysisSummary": "%s • %d Dreiecke • %.1f × %.1f × %.1f mm", "supportsOn": "Stützen empfohlen", "supportsOff": "Keine Stützen nötig",
		"scanAgain": "Flash Studio erneut erkennen", "manualProfiles": "Profile manuell konfigurieren", "openLog": "Diagnoseordner öffnen", "manualIntro": "Wählen Sie nacheinander Flash Studio.exe, das AD5M-0.4-Maschinenprofil, das Prozessprofil und das Filamentprofil. Abbrechen behält den vorherigen Wert bei.", "manualTitle": "Manuelle Konfiguration", "chooseSlicer": "1/4 • Flash Studio.exe wählen", "chooseMachine": "2/4 • Adventurer-5M-0.4-Maschinenprofil", "chooseProcess": "3/4 • AD5M-Basisprozessprofil", "chooseBaseFilament": "4/4 • Basisfilamentprofil", "programs": "Programme", "jsonProfiles": "JSON-Profile",
	},
}

func tr(key string) string {
	if lang, ok := premiumTextByLanguage[uiLanguage]; ok {
		if value, ok := lang[key]; ok {
			return value
		}
	}
	if lang, ok := textByLanguage[uiLanguage]; ok {
		if value, ok := lang[key]; ok {
			return value
		}
	}
	if value, ok := textByLanguage["en"][key]; ok {
		return value
	}
	if value, ok := premiumTextByLanguage["en"][key]; ok {
		return value
	}
	return key
}

func trf(key string, values ...any) string { return fmt.Sprintf(tr(key), values...) }

func normalizeLanguage(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	if len(code) > 2 {
		code = code[:2]
	}
	if _, ok := textByLanguage[code]; ok {
		return code
	}
	return defaultLanguage
}

func settingsPath() string {
	base := os.Getenv("APPDATA")
	if base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "FlashFitAI", "settings.json")
}

func loadUILanguage() {
	b, err := os.ReadFile(settingsPath())
	if err != nil {
		return
	}
	var settings struct {
		Language string `json:"language"`
	}
	if json.Unmarshal(b, &settings) == nil {
		uiLanguage = normalizeLanguage(settings.Language)
	}
}

func saveUILanguage() {
	p := settingsPath()
	_ = os.MkdirAll(filepath.Dir(p), 0700)
	b, _ := json.MarshalIndent(struct {
		Language string `json:"language"`
	}{uiLanguage}, "", "  ")
	_ = os.WriteFile(p, b, 0600)
}

func localizeEngineText(s string) string {
	if uiLanguage == "it" || strings.TrimSpace(s) == "" {
		return s
	}
	exact := map[string]map[string]string{
		"en": {
			"mesh non chiusa/non manifold: correggila prima di importare":            "the mesh is open/non-manifold; repair it before importing",
			"mesh con facce degeneri: FlashFit non la ripara automaticamente":        "the mesh has degenerate faces; FlashFit does not repair it automatically",
			"modello fuori dal volume AD5M 220×220×220 mm nell'orientamento attuale": "the model exceeds the AD5M 220×220×220 mm volume in its current orientation",
			"numero triangoli fuori limite":                                          "triangle count is outside the safe limit",
			"qualità sconosciuta":                                                    "unknown quality preset",
			"analisi annullata":                                                      "analysis canceled",
		},
		"fr": {
			"mesh non chiusa/non manifold: correggila prima di importare":            "maillage ouvert/non-manifold ; corrigez-le avant l'importation",
			"mesh con facce degeneri: FlashFit non la ripara automaticamente":        "le maillage contient des faces dégénérées ; FlashFit ne les répare pas automatiquement",
			"modello fuori dal volume AD5M 220×220×220 mm nell'orientamento attuale": "le modèle dépasse le volume AD5M 220×220×220 mm dans son orientation actuelle",
			"numero triangoli fuori limite":                                          "nombre de triangles hors limite de sécurité",
			"qualità sconosciuta":                                                    "qualité inconnue",
			"analisi annullata":                                                      "analyse annulée",
		},
		"es": {
			"mesh non chiusa/non manifold: correggila prima di importare":            "la malla está abierta/no manifold; corrígela antes de importar",
			"mesh con facce degeneri: FlashFit non la ripara automaticamente":        "la malla tiene caras degeneradas; FlashFit no las repara automáticamente",
			"modello fuori dal volume AD5M 220×220×220 mm nell'orientamento attuale": "el modelo supera el volumen AD5M 220×220×220 mm en su orientación actual",
			"numero triangoli fuori limite":                                          "número de triángulos fuera del límite seguro",
			"qualità sconosciuta":                                                    "calidad desconocida",
			"analisi annullata":                                                      "análisis cancelado",
		},
		"de": {
			"mesh non chiusa/non manifold: correggila prima di importare":            "das Mesh ist offen/nicht-manifold; vor dem Import reparieren",
			"mesh con facce degeneri: FlashFit non la ripara automaticamente":        "das Mesh enthält degenerierte Flächen; FlashFit repariert sie nicht automatisch",
			"modello fuori dal volume AD5M 220×220×220 mm nell'orientamento attuale": "das Modell überschreitet in der aktuellen Ausrichtung den AD5M-Bauraum von 220×220×220 mm",
			"numero triangoli fuori limite":                                          "Dreiecksanzahl außerhalb des sicheren Bereichs",
			"qualità sconosciuta":                                                    "unbekannte Qualität",
			"analisi annullata":                                                      "Analyse abgebrochen",
		},
	}
	if value := exact[uiLanguage][s]; value != "" {
		return value
	}
	return s
}
