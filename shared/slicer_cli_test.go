package shared

import (
	"os"
	"testing"
)

func TestBuildSlicerArgsNeverPassesStandaloneOne(t *testing.T) {
	args := buildSlicerArgs("machine.json", "process.json", "filament.json", "out.3mf", "model.stl", true)
	foundArrange := false
	for _, arg := range args {
		if arg == "1" {
			t.Fatal("standalone 1 would be interpreted as a model by affected Flash Studio builds")
		}
		if arg == "--arrange=1" {
			foundArrange = true
		}
	}
	if !foundArrange {
		t.Fatal("expected equals-style arrange option")
	}
}

func TestArrangeBugDetection(t *testing.T) {
	cases := []string{
		"Model does not exist: 1",
		"modello inesistente: 1",
		"Il modello non esiste 1\n",
	}
	for _, msg := range cases {
		if !standaloneArrangeValueBug("", msg) {
			t.Fatalf("bug message not detected: %q", msg)
		}
	}
	if standaloneArrangeValueBug("", "model does not exist: C:/missing.stl") {
		t.Fatal("ordinary missing model was mistaken for arrange-value bug")
	}
}

func TestExecutableContainsCLIFlagsStreamed(t *testing.T) {
	p := t.TempDir() + "/fake.exe"
	data := make([]byte, 2*1024*1024+256)
	copy(data[1024*1024-4:], []byte("--export-3mf"))
	copy(data[1024*1024+100:], []byte("--load-settings"))
	copy(data[2*1024*1024+10:], []byte("--load-filaments"))
	if err := os.WriteFile(p, data, 0600); err != nil {
		t.Fatal(err)
	}
	if !executableContainsCLIFlags(p) {
		t.Fatal("streamed CLI marker scan did not find all required flags")
	}
}
