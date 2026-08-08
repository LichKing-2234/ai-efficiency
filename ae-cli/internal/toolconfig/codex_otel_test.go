package toolconfig

import (
	"os"
	"path/filepath"
	"testing"

	toml "github.com/pelletier/go-toml/v2"
)

func TestConfigureCodexOTLPWritesTraceOnlyJSONExporterAndPreservesConfig(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("model = \"gpt-test\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	endpoint := "https://ae.example.com/api/v1/attribution/otel/v1/traces"
	if _, err := ConfigureCodexOTLP(home, endpoint, "test-otlp-token"); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := toml.Unmarshal(payload, &config); err != nil {
		t.Fatal(err)
	}
	if config["model"] != "gpt-test" {
		t.Fatalf("existing model was not preserved: %+v", config)
	}
	otel := config["otel"].(map[string]any)
	if otel["log_user_prompt"] != false {
		t.Fatalf("log_user_prompt = %v, want false", otel["log_user_prompt"])
	}
	if _, ok := otel["metrics_exporter"]; ok {
		t.Fatalf("unexpected metrics exporter: %+v", otel)
	}
	trace := otel["trace_exporter"].(map[string]any)["otlp-http"].(map[string]any)
	if trace["endpoint"] != endpoint || trace["protocol"] != "json" {
		t.Fatalf("trace exporter = %+v", trace)
	}
	headers := trace["headers"].(map[string]any)
	if headers["Authorization"] != "Bearer test-otlp-token" {
		t.Fatalf("authorization header was not written")
	}
}

func TestDisableCodexOTLPRemovesOnlyMatchingManagedExporter(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".codex", "config.toml")
	endpoint := "https://ae.example.com/api/v1/attribution/otel/v1/traces"
	if _, err := ConfigureCodexOTLP(home, endpoint, "test-otlp-token"); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := toml.Unmarshal(payload, &config); err != nil {
		t.Fatal(err)
	}
	config["model"] = "gpt-test"
	trace := config["otel"].(map[string]any)["trace_exporter"].(map[string]any)
	trace["custom"] = map[string]any{"endpoint": "https://telemetry.example.org/traces"}
	payload, err = toml.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}

	_, changed, err := DisableCodexOTLP(home, endpoint, "test-otlp-token")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("managed exporter was not removed")
	}
	payload, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	config = nil
	if err := toml.Unmarshal(payload, &config); err != nil {
		t.Fatal(err)
	}
	if config["model"] != "gpt-test" {
		t.Fatalf("unrelated Codex config was changed: %+v", config)
	}
	trace = config["otel"].(map[string]any)["trace_exporter"].(map[string]any)
	if _, ok := trace["otlp-http"]; ok {
		t.Fatalf("managed otlp-http exporter survived: %+v", trace)
	}
	if _, ok := trace["custom"]; !ok {
		t.Fatalf("unrelated trace exporter was removed: %+v", trace)
	}
}

func TestDisableCodexOTLPPreservesExporterWhenOwnershipDoesNotMatch(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		token    string
	}{
		{name: "different endpoint", endpoint: "https://other.example.org/traces", token: "test-otlp-token"},
		{name: "different token", endpoint: "https://ae.example.com/api/v1/attribution/otel/v1/traces", token: "other-token"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir()
			endpoint := "https://ae.example.com/api/v1/attribution/otel/v1/traces"
			if _, err := ConfigureCodexOTLP(home, endpoint, "test-otlp-token"); err != nil {
				t.Fatal(err)
			}
			_, changed, err := DisableCodexOTLP(home, test.endpoint, test.token)
			if err != nil {
				t.Fatal(err)
			}
			if changed {
				t.Fatal("exporter with different ownership evidence was removed")
			}
			inspection, err := InspectCodexOTLP(home, endpoint, "test-otlp-token")
			if err != nil {
				t.Fatal(err)
			}
			if !inspection.Configured || !inspection.CredentialAvailable {
				t.Fatalf("managed exporter was changed despite ownership mismatch: %+v", inspection)
			}
		})
	}
}
