package project

import (
	"os"
	"path/filepath"
	"testing"
)

// TestGatesConfigDefaults pins F-0131 criterion 8: absent [gates] (and absent
// stale_ready_days) mean human gates on both ends and a 14-day ready window.
func TestGatesConfigDefaults(t *testing.T) {
	dir := t.TempDir()
	writeCernerc(t, dir, "project_id = \"p\"\nbackend = \"b\"\ndefault_scope = \"\"\n")

	c, _, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got := c.Gates.ApproveGate(); got != GateHuman {
		t.Errorf("ApproveGate() = %q, want %q", got, GateHuman)
	}
	if got := c.Gates.AcceptGate(); got != GateHuman {
		t.Errorf("AcceptGate() = %q, want %q", got, GateHuman)
	}
	if got := c.Gates.RevertGate(); got != GateHuman {
		t.Errorf("RevertGate() = %q, want %q", got, GateHuman)
	}
	if got := c.StaleReadyWindowDays(); got != 14 {
		t.Errorf("StaleReadyWindowDays() = %d, want 14", got)
	}
}

// TestGatesConfigBypass: the explicit [gates] block flips each end
// independently; unknown values fall back to human (fail-safe).
func TestGatesConfigBypass(t *testing.T) {
	cases := []struct {
		name    string
		toml    string
		approve string
		accept  string
		revert  string
	}{
		{"both auto", "[gates]\napprove = \"auto\"\naccept = \"auto\"\n", GateAuto, GateAuto, GateHuman},
		{"approve only", "[gates]\napprove = \"auto\"\n", GateAuto, GateHuman, GateHuman},
		{"accept only", "[gates]\naccept = \"auto\"\n", GateHuman, GateAuto, GateHuman},
		{"revert only", "[gates]\nrevert = \"auto\"\n", GateHuman, GateHuman, GateAuto},
		{"unknown value is human", "[gates]\napprove = \"banana\"\nrevert = \"banana\"\n", GateHuman, GateHuman, GateHuman},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeCernerc(t, dir, "project_id = \"p\"\nbackend = \"b\"\ndefault_scope = \"\"\n"+tc.toml)
			c, _, err := LoadConfig(dir)
			if err != nil {
				t.Fatalf("LoadConfig: %v", err)
			}
			if got := c.Gates.ApproveGate(); got != tc.approve {
				t.Errorf("ApproveGate() = %q, want %q", got, tc.approve)
			}
			if got := c.Gates.AcceptGate(); got != tc.accept {
				t.Errorf("AcceptGate() = %q, want %q", got, tc.accept)
			}
			if got := c.Gates.RevertGate(); got != tc.revert {
				t.Errorf("RevertGate() = %q, want %q", got, tc.revert)
			}
		})
	}
}

// TestAcceptGateMerge: "merge" is a third accept value, only as that literal;
// the other values keep their meaning and a near miss still reads as human.
// F-0488 crit. 1
func TestAcceptGateMerge(t *testing.T) {
	cases := []struct {
		accept string
		want   string
	}{
		{"merge", GateMerge},
		{"Merge", GateHuman},
		{"merged", GateHuman},
		{"auto", GateAuto},
		{"human", GateHuman},
		{"", GateHuman},
	}
	for _, tc := range cases {
		if got := (GatesConfig{Accept: tc.accept}).AcceptGate(); got != tc.want {
			t.Errorf("AcceptGate() with accept = %q: got %q, want %q", tc.accept, got, tc.want)
		}
	}
	// The other ends never read "merge".
	g := GatesConfig{Approve: "merge", Revert: "merge"}
	if g.ApproveGate() != GateHuman || g.RevertGate() != GateHuman {
		t.Errorf("approve/revert = merge must read as human, got %q/%q", g.ApproveGate(), g.RevertGate())
	}
}

// TestStaleReadyDaysConfigured: stale_ready_days overrides the 14-day default.
func TestStaleReadyDaysConfigured(t *testing.T) {
	dir := t.TempDir()
	writeCernerc(t, dir, "project_id = \"p\"\nbackend = \"b\"\ndefault_scope = \"\"\nstale_ready_days = 3\n")
	c, _, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got := c.StaleReadyWindowDays(); got != 3 {
		t.Errorf("StaleReadyWindowDays() = %d, want 3", got)
	}
}

// TestSaveConfigOmitsZeroGates: a zero Gates block does not serialize an empty
// [gates] table (same stance as the [llm] block).
func TestSaveConfigOmitsZeroGates(t *testing.T) {
	dir := t.TempDir()
	if err := SaveConfig(dir, Config{ProjectID: "p", Backend: "b"}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".cernerc"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if contains := string(data); containsGatesTable(contains) {
		t.Errorf(".cernerc carries an empty [gates] table:\n%s", contains)
	}
}

func containsGatesTable(s string) bool {
	for i := 0; i+7 <= len(s); i++ {
		if s[i:i+7] == "[gates]" {
			return true
		}
	}
	return false
}

func writeCernerc(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, ".cernerc"), []byte(content), 0o644); err != nil {
		t.Fatalf("write .cernerc: %v", err)
	}
}
