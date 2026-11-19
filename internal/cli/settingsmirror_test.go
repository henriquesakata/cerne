package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/tracker"
)

var integrationKeys = []string{"delivery.commit_url", "delivery.forge", "delivery.pr_url", "mirror.close_on", "mirror.enabled", "mirror.trackers"}

// trackerRecords decodes the JSON a records key prints, dropping empty values.
func trackerRecords(t *testing.T, out string) []map[string]string {
	t.Helper()
	var recs []map[string]string
	if err := json.Unmarshal([]byte(out), &recs); err != nil {
		t.Fatalf("records output %q is not a JSON list of string records: %v", out, err)
	}
	for _, r := range recs {
		for k, v := range r {
			if v == "" {
				delete(r, k)
			}
		}
	}
	return recs
}

// F-0449 crit. 1: settings list shows the six integration keys, the delivery
// ones read from the existing [delivery] block, mirror.trackers as compact
// JSON; --json carries their group, project-only scope, human-only marks and
// the record fields.
func TestSettingsListIntegrationKeys(t *testing.T) {
	projDir, _ := settingsProject(t, "[delivery]\nforge = \"github\"\ncommit_url = \"https://github.com/o/r/commit/{sha}\"\n")
	out, err := execCerne(t, "settings", "list", "--dir", projDir)
	if err != nil {
		t.Fatalf("settings list: %v\n%s", err, out)
	}
	rows := map[string]bool{}
	for _, l := range strings.Split(out, "\n") {
		rows[strings.Join(strings.Fields(l), " ")] = true
	}
	for _, want := range []string{
		"delivery.commit_url string https://github.com/o/r/commit/{sha} project",
		"delivery.forge string github project",
		"delivery.pr_url string default",
		"mirror.close_on string accept accept default",
		"mirror.enabled bool false false default",
		"mirror.trackers records [] [] default",
	} {
		if !rows[want] {
			t.Errorf("list lacks row %q:\n%s", want, out)
		}
	}

	byID := settingsJSON(t, projDir)
	for _, id := range integrationKeys {
		s, ok := byID[id]
		if !ok {
			t.Errorf("--json lacks %s", id)
			continue
		}
		if s.Group != "integrations" || !slices.Equal(s.Scopes, []string{project.ScopeProject}) {
			t.Errorf("%s = group %q, scopes %v; want integrations, [project]", id, s.Group, s.Scopes)
		}
		if want := id == "mirror.enabled" || id == "mirror.trackers"; s.HumanOnly != want {
			t.Errorf("%s human_only = %v, want %v", id, s.HumanOnly, want)
		}
	}
	var fields []string
	for _, f := range byID["mirror.trackers"].Fields {
		fields = append(fields, f.ID)
	}
	if !slices.Equal(fields, []string{"name", "kind", "url", "label"}) {
		t.Errorf("mirror.trackers fields = %v, want [name kind url label]", fields)
	}

	if out, err := execCerne(t, "settings", "get", "mirror.trackers", "--dir", projDir); err != nil || out != "[]\n" {
		t.Errorf("get mirror.trackers = %q (%v), want the empty default as compact JSON", out, err)
	}
}

// F-0449 crit. 2 and 3: settings set takes mirror.trackers as JSON and writes
// one inline array under [mirror]; get prints it back as compact JSON; an
// unknown kind and bad JSON are refused; an agent is refused on both
// human-only keys. No refusal touches .cernerc.
func TestSettingsSetMirrorTrackers(t *testing.T) {
	t.Setenv("CERNE_ACTOR", "")
	t.Setenv("COPILOT_CLI", "")
	projDir, _ := settingsProject(t, "")
	rc := filepath.Join(projDir, ".cernerc")
	const raw = `[{"name":"gh","kind":"github","url":"https://github.com/o/issues"},{"name":"ext","kind":"github","url":"https://ghe.example/acme/issues","label":"public"}]`
	if out, err := execCerne(t, "settings", "set", "mirror.trackers", raw, "--scope", "project", "--dir", projDir); err != nil {
		t.Fatalf("settings set mirror.trackers: %v\n%s", err, out)
	}
	body, _ := os.ReadFile(rc)
	var lines []string
	for _, l := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(l, "trackers") {
			lines = append(lines, l)
		}
	}
	if len(lines) != 1 || !strings.Contains(string(body), "\n[mirror]\ntrackers = [{name = \"gh\", kind = \"github\", url = \"https://github.com/o/issues\"}") ||
		!strings.HasSuffix(lines[0], `{name = "ext", kind = "github", url = "https://ghe.example/acme/issues", label = "public"}]`) {
		t.Fatalf(".cernerc lacks the inline [mirror] trackers line:\n%s", body)
	}
	out, err := execCerne(t, "settings", "get", "mirror.trackers", "--dir", projDir)
	if err != nil || strings.Count(out, "\n") != 1 {
		t.Fatalf("get mirror.trackers = %q (%v), want one line", out, err)
	}
	want := []map[string]string{
		{"name": "gh", "kind": "github", "url": "https://github.com/o/issues"},
		{"name": "ext", "kind": "github", "url": "https://ghe.example/acme/issues", "label": "public"},
	}
	if got := trackerRecords(t, out); !reflect.DeepEqual(got, want) {
		t.Fatalf("get mirror.trackers = %v, want %v", got, want)
	}

	before, _ := os.ReadFile(rc)
	_, err = execCerne(t, "settings", "set", "mirror.trackers", `[{"name":"j","kind":"jira","url":"https://jira.example/PROJ"}]`, "--scope", "project", "--dir", projDir)
	if !errors.Is(err, project.ErrInvalidSettingValue) || !errors.Is(err, tracker.ErrUnknownKind) {
		t.Errorf("set a jira tracker: err = %v, want ErrInvalidSettingValue and ErrUnknownKind", err)
	}
	wantText := `unknown tracker kind "jira" (supported: ` + strings.Join(tracker.KindNames(), ", ") + `)`
	if err == nil || !strings.Contains(err.Error(), wantText) {
		t.Errorf("set a jira tracker: err = %v, want the text %s", err, wantText)
	}
	if _, err := execCerne(t, "settings", "set", "mirror.trackers", `[{"name":`, "--scope", "project", "--dir", projDir); !errors.Is(err, project.ErrInvalidSettingValue) {
		t.Errorf("set bad JSON: err = %v, want ErrInvalidSettingValue", err)
	}

	t.Setenv("CERNE_ACTOR", "agent")
	for _, args := range [][]string{
		{"settings", "set", "mirror.trackers", `[{"name":"mine","kind":"github","url":"https://github.com/agent/leak"}]`, "--scope", "project", "--dir", projDir},
		{"settings", "set", "mirror.enabled", "true", "--scope", "project", "--dir", projDir},
		{"settings", "unset", "mirror.trackers", "--scope", "project", "--dir", projDir},
	} {
		if out, err := execCerne(t, args...); !errors.Is(err, app.ErrHumanGate) {
			t.Errorf("agent %v: err = %v (%s), want ErrHumanGate", args[1:3], err, out)
		}
	}
	if after, _ := os.ReadFile(rc); string(after) != string(before) {
		t.Fatalf("a refused write changed .cernerc:\n%s", after)
	}
	if out, err := execCerne(t, "settings", "set", "mirror.close_on", "merge", "--scope", "project", "--dir", projDir); err != nil {
		t.Errorf("agent on mirror.close_on (not human-only): %v\n%s", err, out)
	}
}
