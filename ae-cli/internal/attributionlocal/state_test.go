package attributionlocal

import (
	"path/filepath"
	"testing"
)

func TestAttributionRootDirUsesAeCliState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if got, want := AttributionRootDir(), filepath.Join(home, ".ae-cli", "state", "attribution"); got != want {
		t.Fatalf("AttributionRootDir() = %q, want %q", got, want)
	}
}

func TestSaveJSONAndLoadJSONRoundTrip(t *testing.T) {
	t.Parallel()

	type payload struct {
		Value string `json:"value"`
	}

	path := filepath.Join(t.TempDir(), "state.json")
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
