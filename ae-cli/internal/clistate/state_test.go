package clistate

import (
	"path/filepath"
	"testing"
)

func TestAttributionRootStateDirsUseAeCliRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if got, want := RootDir(), filepath.Join(home, ".ae-cli"); got != want {
		t.Fatalf("RootDir() = %q, want %q", got, want)
	}
	if got, want := StateDir(), filepath.Join(home, ".ae-cli", "state"); got != want {
		t.Fatalf("StateDir() = %q, want %q", got, want)
	}
	if got, want := HooksStateDir(), filepath.Join(home, ".ae-cli", "state", "hooks"); got != want {
		t.Fatalf("HooksStateDir() = %q, want %q", got, want)
	}
	if got, want := AttributionStateDir(), filepath.Join(home, ".ae-cli", "state", "attribution"); got != want {
		t.Fatalf("AttributionStateDir() = %q, want %q", got, want)
	}
}

func TestSaveJSONAndLoadJSONRoundTrip(t *testing.T) {
	t.Parallel()

	type payload struct {
		Value string `json:"value"`
	}

	path := filepath.Join(t.TempDir(), "nested", "state.json")
	if err := SaveJSON(path, payload{Value: "ok"}); err != nil {
		t.Fatalf("SaveJSON: %v", err)
	}

	var got payload
	if err := LoadJSON(path, &got); err != nil {
		t.Fatalf("LoadJSON: %v", err)
	}
	if got.Value != "ok" {
		t.Fatalf("Value = %q, want ok", got.Value)
	}
}
