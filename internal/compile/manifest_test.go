package compile

import (
	"encoding/json"
	"testing"
	"time"
)

// TestManifestRoundTrip pins the stable JSON schema of the delegation manifest:
// it survives a marshal/unmarshal round-trip field-for-field, and the emitted
// keys are exactly the contract F-0074 reuses (no rename without revisiting it).
func TestManifestRoundTrip(t *testing.T) {
	m := Manifest{
		Version: ManifestVersion,
		RunID:   "20260625T193301Z-all",
		Entries: []ManifestEntry{
			{
				Page:         "finance",
				PromptPath:   ".docs/wiki/.pending/modules/finance.prompt.md",
				ResponsePath: ".docs/wiki/.pending/modules/finance.md",
				SourceHash:   "deadbeef",
				RunID:        "20260625T193301Z-all",
			},
			{
				Page:         "decisions",
				PromptPath:   ".docs/wiki/.pending/decisions.prompt.md",
				ResponsePath: ".docs/wiki/.pending/decisions.md",
				SourceHash:   "cafe",
				RunID:        "20260625T193301Z-all",
			},
		},
	}

	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var back Manifest
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Version != m.Version || back.RunID != m.RunID || len(back.Entries) != len(m.Entries) {
		t.Fatalf("round-trip lost top-level fields: %+v", back)
	}
	for i, e := range back.Entries {
		if e != m.Entries[i] {
			t.Fatalf("entry %d round-trip mismatch:\n got %+v\nwant %+v", i, e, m.Entries[i])
		}
	}

	// Lock the exact JSON key set — this is the machine contract.
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}
	assertKeys(t, "manifest", top, []string{"version", "run_id", "entries"})

	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(top["entries"], &entries); err != nil {
		t.Fatalf("unmarshal entries: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no entries in JSON")
	}
	assertKeys(t, "entry", entries[0], []string{"page", "prompt_path", "response_path", "source_hash", "run_id"})
}

func assertKeys(t *testing.T, what string, m map[string]json.RawMessage, want []string) {
	t.Helper()
	if len(m) != len(want) {
		t.Fatalf("%s: got %d keys %v, want %d %v", what, len(m), keysOf(m), len(want), want)
	}
	for _, k := range want {
		if _, ok := m[k]; !ok {
			t.Fatalf("%s: missing JSON key %q (got %v)", what, k, keysOf(m))
		}
	}
}

func keysOf(m map[string]json.RawMessage) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

// TestNewRunIDFormat pins the run_id scheme: compact UTC timestamp + scope, with
// "all" standing in for an empty (multi-page) scope. The clock is a parameter so
// the test is deterministic.
func TestNewRunIDFormat(t *testing.T) {
	now := time.Date(2026, 6, 25, 19, 33, 1, 0, time.UTC)
	tests := []struct {
		scope string
		want  string
	}{
		{"finance", "20260625T193301Z-finance"},
		{"", "20260625T193301Z-all"},
		{"decisions", "20260625T193301Z-decisions"},
	}
	for _, tt := range tests {
		if got := NewRunID(tt.scope, now); got != tt.want {
			t.Errorf("NewRunID(%q) = %q, want %q", tt.scope, got, tt.want)
		}
	}
}
