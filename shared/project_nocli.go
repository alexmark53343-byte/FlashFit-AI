package shared

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Some slicer installations have no command line at all.
//
// Flash Studio Desktop ships the Orca engine as a 122 MB DLL behind a 100 KB
// launcher: there is no executable to pass --export-3mf to, and the launcher
// ignores arguments and simply opens its window. Refusing to do anything in
// that case left the user with an error and no project, which is the worst of
// both worlds — the profile had already been computed correctly.
//
// So when the engine cannot be driven, FlashFit writes the project itself. The
// analysis has already produced a geometry-only 3MF of the exact mesh, so this
// only has to repackage it with the settings attached, in the config entry
// every Orca-derived slicer reads when opening a project. The user gets their
// model on the plate with the FlashFit profile already applied, and presses
// Slice themselves.
//
// This is a fallback, not a substitute: it does not slice, so there is no
// sliced time or layer preview until the slicer does its part.

// projectConfigEntry is where Orca, Bambu Studio and Flash Studio keep project
// settings.
//
// This was originally Metadata/Slic3r_PE.config with key = value lines, which
// is PrusaSlicer's convention and is simply not read by this family of slicers:
// the file went into the project, the slicer ignored it, and every setting fell
// back to its own defaults. That is why a profile asking for a 0.14 mm layer
// opened as 0.20 mm, and why the estimates disagreed with the sliced result.
//
// Verified against projects these slicers wrote themselves: the entry is
// project_settings.config and the contents are JSON.
const projectConfigEntry = "Metadata/project_settings.config"

// modelSettingsEntry is the other half of a Bambu/Orca project, and leaving it
// out is why the settings arrived and the model did not.
//
// A 3MF with a valid <build> is loaded object-by-object when it is opened as a
// *model*. Attaching project_settings.config changes what the file is: the
// slicer now recognises a project, and a project describes its objects and its
// plates here. Ours declared none, so Bambu Studio did exactly what it was
// told — read the settings, put nothing on the plate.
//
// It is only synthesised when the source has none. A 3MF that came from a
// slicer already carries its own, describing objects, parts and placements this
// has no business inventing.
const modelSettingsEntry = "Metadata/model_settings.config"

// Filament settings are per-extruder, so the slicer expects them as arrays even
// on a single-extruder machine. Process settings are plain strings.
var filamentArrayKeys = map[string]bool{
	"filament_density": true, "filament_flow_ratio": true, "filament_max_volumetric_speed": true,
	"nozzle_temperature": true, "nozzle_temperature_initial_layer": true,
	"hot_plate_temp": true, "hot_plate_temp_initial_layer": true,
	"textured_plate_temp": true, "textured_plate_temp_initial_layer": true,
	"fan_min_speed": true, "fan_max_speed": true, "enable_pressure_advance": true, "pressure_advance": true,
	"close_fan_the_first_x_layers": true, "full_fan_speed_layer": true,
	"slow_down_for_layer_cooling": true, "slow_down_layer_time": true, "min_print_speed": true,
}

// WriteProjectWithSettings copies a geometry 3MF and attaches the recommended
// process and filament settings, producing a project file the slicer can open.
// ProjectSources names the installed profiles a project is built on top of, so
// the written configuration is complete rather than a fragment.
type ProjectSources struct {
	Machine      string
	BaseProcess  string
	BaseFilament string
}

func WriteProjectWithSettings(geometry3MF, outPath string, rec Recommendation, printer PrinterProfile, profileName string) error {
	return WriteProjectWithSources(geometry3MF, outPath, rec, printer, profileName, ProjectSources{})
}

// WriteProjectWithSources is the full form: the installed profiles provide
// every setting FlashFit does not tune, which is most of them.
func WriteProjectWithSources(geometry3MF, outPath string, rec Recommendation, printer PrinterProfile, profileName string, sources ProjectSources) error {
	source, err := zip.OpenReader(geometry3MF)
	if err != nil {
		return fmt.Errorf("3MF geometrico non leggibile: %w", err)
	}
	defer source.Close()

	out, err := os.Create(outPath)
	if err != nil {
		return err
	}
	writer := zip.NewWriter(out)

	// What the source already describes decides what has to be added: a 3MF
	// written by a slicer brings its own object and plate description, and
	// overwriting that with a synthesised one would throw away real placements.
	hasModelSettings := false
	var modelXML []byte

	for _, entry := range source.File {
		// Our own config wins if the source somehow already carries one.
		if strings.EqualFold(entry.Name, projectConfigEntry) {
			continue
		}
		if strings.EqualFold(entry.Name, modelSettingsEntry) {
			hasModelSettings = true
		}
		reader, err := entry.Open()
		if err != nil {
			writer.Close()
			out.Close()
			return err
		}
		target, err := writer.Create(entry.Name)
		if err != nil {
			reader.Close()
			writer.Close()
			out.Close()
			return err
		}
		var copyErr error
		if strings.EqualFold(entry.Name, modelEntry) {
			// Kept so the objects it builds can be listed below.
			modelXML, copyErr = io.ReadAll(reader)
			if copyErr == nil {
				_, copyErr = target.Write(modelXML)
			}
		} else {
			_, copyErr = io.Copy(target, reader)
		}
		reader.Close()
		if copyErr != nil {
			writer.Close()
			out.Close()
			return copyErr
		}
	}

	if !hasModelSettings {
		if err := writeModelSettings(writer, modelXML, profileName); err != nil {
			writer.Close()
			out.Close()
			return err
		}
	}

	config, err := writer.Create(projectConfigEntry)
	if err != nil {
		writer.Close()
		out.Close()
		return err
	}
	settings := projectSettingsJSON(rec, printer, profileName, sources.Machine, sources.BaseProcess, sources.BaseFilament)
	if _, err = config.Write([]byte(settings)); err != nil {
		writer.Close()
		out.Close()
		return err
	}
	if err = writer.Close(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// projectConfigText renders the settings as the JSON object the slicer reads.
// Every value is a string, or an array of strings for per-extruder settings —
// that is the shape these slicers write and expect, and a number where a string
// belongs is enough for the whole file to be discarded.
// projectConfigText is the no-sources form, kept for callers that have no
// installed profiles to build on.
func projectConfigText(rec Recommendation, printer PrinterProfile, profileName string) string {
	return projectSettingsJSON(rec, printer, profileName, "", "", "")
}

// writePlateProjects writes one project per plate, numbering them so the order
// is obvious in the folder. The first path returned is the one to open.
func writePlateProjects(plates []Plate, basePath string, rec Recommendation, printer PrinterProfile, profileName, workDir string, sources ProjectSources) ([]string, error) {
	written := make([]string, 0, len(plates))
	stem := strings.TrimSuffix(basePath, filepath.Ext(basePath))
	for i, plate := range plates {
		geometry := filepath.Join(workDir, fmt.Sprintf("plate-%d.3mf", i+1))
		if err := writeGeometryOnly3MF(geometry, PlateTriangles(plate)); err != nil {
			return nil, fmt.Errorf("piatto %d non generabile: %w", i+1, err)
		}
		out := basePath
		if len(plates) > 1 {
			out = fmt.Sprintf("%s_piatto%d.3mf", stem, i+1)
		}
		name := profileName
		if len(plates) > 1 {
			name = fmt.Sprintf("%s (piatto %d/%d)", profileName, i+1, len(plates))
		}
		if err := WriteProjectWithSources(geometry, out, rec, printer, name, sources); err != nil {
			return nil, fmt.Errorf("progetto del piatto %d non scrivibile: %w", i+1, err)
		}
		written = append(written, out)
	}
	if len(written) == 0 {
		return nil, errors.New("nessun piatto scritto")
	}
	return written, nil
}

// SlicerHasCLI reports whether the engine can be driven from the command line.
// The strings live in whichever binary holds the engine, which on a launcher
// based install is a sibling DLL rather than the executable itself.
func SlicerHasCLI(exePath string) bool {
	return ProbeSlicerCLI(exePath) == nil
}

// modelEntry is the 3MF part that declares the build.
const modelEntry = "3D/3dmodel.model"

// writeModelSettings describes, for the slicer, the objects the build already
// contains and the plate they belong to.
//
// The object ids are read out of the build rather than assumed, because a
// project written for several plates, or from a file that held several objects,
// declares more than one — and an instance pointing at an id that is not there
// puts the plate back to empty, which is the failure being fixed.
func writeModelSettings(writer *zip.Writer, modelXML []byte, name string) error {
	ids := buildItemObjectIDs(modelXML)
	if len(ids) == 0 {
		// Nothing was built, so there is nothing to place. Writing a plate with
		// no instances would be worse than writing nothing: it states, wrongly,
		// that the plate is meant to be empty.
		return nil
	}
	entry, err := writer.Create(modelSettingsEntry)
	if err != nil {
		return err
	}
	_, err = io.WriteString(entry, modelSettingsXML(ids, name))
	return err
}

// buildItemObjectIDs pulls the objectid of every <item> in the build section.
func buildItemObjectIDs(modelXML []byte) []string {
	const marker = `objectid="`
	var ids []string
	seen := map[string]bool{}
	text := string(modelXML)
	for {
		at := strings.Index(text, marker)
		if at < 0 {
			break
		}
		text = text[at+len(marker):]
		end := strings.IndexByte(text, '"')
		if end <= 0 {
			break
		}
		id := text[:end]
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
		text = text[end:]
	}
	return ids
}

// modelSettingsXML renders the object and plate description, in the shape these
// slicers write themselves.
func modelSettingsXML(ids []string, name string) string {
	label := strings.TrimSpace(name)
	if label == "" {
		label = "FlashFit"
	}
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<config>\n")
	for _, id := range ids {
		fmt.Fprintf(&b, "  <object id=\"%s\">\n", xmlAttr(id))
		fmt.Fprintf(&b, "    <metadata key=\"name\" value=\"%s\"/>\n", xmlAttr(label))
		b.WriteString("    <metadata key=\"extruder\" value=\"1\"/>\n")
		b.WriteString("    <part id=\"1\" subtype=\"normal_part\">\n")
		fmt.Fprintf(&b, "      <metadata key=\"name\" value=\"%s\"/>\n", xmlAttr(label))
		// Identity: the geometry is already written in plate coordinates, so a
		// transform here would move it a second time.
		b.WriteString("      <metadata key=\"matrix\" value=\"1 0 0 0 0 1 0 0 0 0 1 0 0 0 0 1\"/>\n")
		b.WriteString("    </part>\n  </object>\n")
	}
	b.WriteString("  <plate>\n")
	b.WriteString("    <metadata key=\"plater_id\" value=\"1\"/>\n")
	b.WriteString("    <metadata key=\"plater_name\" value=\"\"/>\n")
	b.WriteString("    <metadata key=\"locked\" value=\"false\"/>\n")
	for i, id := range ids {
		b.WriteString("    <model_instance>\n")
		fmt.Fprintf(&b, "      <metadata key=\"object_id\" value=\"%s\"/>\n", xmlAttr(id))
		fmt.Fprintf(&b, "      <metadata key=\"instance_id\" value=\"%d\"/>\n", i)
		b.WriteString("    </model_instance>\n")
	}
	b.WriteString("  </plate>\n</config>\n")
	return b.String()
}

// xmlAttr escapes the few characters that would break an attribute. The ids
// come from a file the user supplied, so they are not trusted to be clean.
func xmlAttr(s string) string {
	return strings.NewReplacer(`&`, "&amp;", `"`, "&quot;", `<`, "&lt;", `>`, "&gt;", `'`, "&apos;").Replace(s)
}
