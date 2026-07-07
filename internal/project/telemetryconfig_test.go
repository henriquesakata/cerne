package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTelemetryConfigRoundTrip pins the [telemetry] block (F-0149): values
// survive save+load, and an absent block stays absent in the written TOML.
func TestTelemetryConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	in := Config{
		ProjectID: "demo",
		Backend:   "git@example.com:demo/docs.git",
		Telemetry: TelemetryConfig{
			OtlpEndpoint: "http://tempo.observability:4318",
			ServiceName:  "cerne-service",
			LogLevel:     "debug",
		},
	}
	if err := SaveConfig(dir, in); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	out, _, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if out.Telemetry != in.Telemetry {
		t.Fatalf("telemetry = %+v, want %+v", out.Telemetry, in.Telemetry)
	}
}

func TestTelemetryBlockOmittedWhenZero(t *testing.T) {
	dir := t.TempDir()
	if err := SaveConfig(dir, Config{ProjectID: "demo", Backend: "b"}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".cernerc"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "[telemetry]") {
		t.Fatalf("zero-value [telemetry] must be omitted:\n%s", data)
	}
}
