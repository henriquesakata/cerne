package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// F-0484 critic #2: the network table is detected by parsing, not by substring.
func TestInstallCodexSandboxNetworkDetectsByParsing(t *testing.T) {
	cases := []struct {
		name, extra string
		wantStatus  Status
		wantSame    bool // file byte-identical after the request
		wantNote    string
	}{
		{"enabled true", "\n[permissions.cerne.network]\nenabled = true\n", StatusUnchanged, true, ""},
		{"spacing variant true", "\n[ permissions.cerne . network ]\nenabled=true\n", StatusUnchanged, true, ""},
		{"dotted true", "\n[permissions.cerne]\nnetwork.enabled = true\n", StatusUnchanged, true, ""},
		{"enabled false", "\n[permissions.cerne.network]\nenabled = false\n", StatusSkipped, true, "has enabled = false — not changed"},
		{"commented out", "\n# [permissions.cerne.network]\n# enabled = true\n", StatusUpdated, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			proj, _ := codexProject(t, "x")
			if _, err := Install(proj, ProviderCodex); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(proj, ".codex", "config.toml")
			raw, _ := os.ReadFile(path)
			// the [permissions.cerne] dotted case must live inside that table: place before filesystem
			body := string(raw)
			if strings.Contains(tc.extra, "[permissions.cerne]\n") {
				body = strings.Replace(body, "[permissions.cerne]\nextends = \":workspace\"\n", "[permissions.cerne]\nextends = \":workspace\"\nnetwork.enabled = true\n", 1)
			} else {
				body += tc.extra
			}
			if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			rs, err := InstallNetwork(proj, true, ProviderCodex)
			if err != nil {
				t.Fatal(err)
			}
			var got Result
			for _, r := range rs {
				if r.Asset == codexSandboxAsset {
					got = r
				}
			}
			if got.Status != tc.wantStatus {
				t.Errorf("status = %v, want %v (%s)", got.Status, tc.wantStatus, got.Note)
			}
			if tc.wantNote != "" && !strings.Contains(got.Note, tc.wantNote) {
				t.Errorf("note = %q, want %q", got.Note, tc.wantNote)
			}
			after, _ := os.ReadFile(path)
			if (string(after) == body) != tc.wantSame {
				t.Errorf("file changed = %v, want unchanged = %v:\n%s", string(after) != body, tc.wantSame, after)
			}
			if !tc.wantSame {
				if en, ok := codexNetEnabled(t, string(after)); !ok || !en {
					t.Errorf("table not appended:\n%s", after)
				}
			}
		})
	}
}
