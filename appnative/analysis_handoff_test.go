package main

import (
	"encoding/json"
	"testing"

	"flashfitai/shared"
)

// The analysis runs in its own process and comes home as JSON. Both path fields
// are json:"-" on ModelAnalysis so they never end up in a saved summary, which
// also meant they were silently lost in transit: the import then had no
// geometry copy and refused to build anything.
//
// This pins the handoff: whatever the worker found on disk must still be known
// to the caller.
func TestAnalysisWorkerCarriesPathsAcrossProcesses(t *testing.T) {
	original := shared.ModelAnalysis{
		Filename:        "porsche_911.stl",
		StoredModelPath: `C:\cache\flashfit-geometry-123.3mf`,
		SourcePath:      `C:\models\porsche_911.stl`,
		TriangleCount:   1200,
	}

	sent := analysisWorkerResult{
		Analysis:        original,
		StoredModelPath: original.StoredModelPath,
		SourcePath:      original.SourcePath,
	}
	encoded, err := json.Marshal(sent)
	if err != nil {
		t.Fatalf("serializzazione fallita: %v", err)
	}

	var received analysisWorkerResult
	if err := json.Unmarshal(encoded, &received); err != nil {
		t.Fatalf("deserializzazione fallita: %v", err)
	}

	// This is the part that used to fail: the struct alone loses them.
	if received.Analysis.StoredModelPath != "" {
		t.Fatal("presupposto cambiato: ModelAnalysis ora serializza StoredModelPath")
	}

	rebuilt := received.Analysis
	rebuilt.StoredModelPath = received.StoredModelPath
	rebuilt.SourcePath = received.SourcePath

	if rebuilt.StoredModelPath != original.StoredModelPath {
		t.Fatalf("copia geometrica persa nel passaggio fra processi: %q", rebuilt.StoredModelPath)
	}
	if rebuilt.SourcePath != original.SourcePath {
		t.Fatalf("percorso originale perso nel passaggio fra processi: %q", rebuilt.SourcePath)
	}
}
