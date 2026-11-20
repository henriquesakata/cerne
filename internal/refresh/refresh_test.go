package refresh

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/henriquesakata/cerne/internal/hooks"
)

// fixtureV0143 is what cerne v0.14.3 installed, copied from the release itself
// by `go run ./tools/knowncontents fixture` (not by hand).
func fixtureV0143(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	src := filepath.Join("testdata", "v0.14.3")
	err := filepath.WalkDir(src, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if e.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

func currentProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := InstallAll(dir); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestClassify(t *testing.T) {
	edit := func(rel, body string) func(*testing.T, string) {
		return func(t *testing.T, dir string) {
			if err := os.WriteFile(filepath.Join(dir, rel), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	remove := func(rel string) func(*testing.T, string) {
		return func(t *testing.T, dir string) {
			if err := os.Remove(filepath.Join(dir, rel)); err != nil {
				t.Fatal(err)
			}
		}
	}
	tests := []struct {
		name    string
		project func(*testing.T) string
		change  func(*testing.T, string)
		agent   hooks.Provider
		want    State
		unknown string // a path the Unknown list must carry
	}{
		{"a whole v0.14.3 install is renewable", fixtureV0143, nil, hooks.ProviderClaude, Renewable, ""},
		{"the v0.14.3 guide of another agent is renewable", fixtureV0143, nil, hooks.ProviderCopilot, Renewable, ""},
		{"this build's own install is up to date", currentProject, nil, hooks.ProviderClaude, UpToDate, ""},
		{"an edited script is unrecognized", fixtureV0143, edit(".claude/hooks/cerne-work-gate.sh", "#!/bin/sh\necho mine\n"), hooks.ProviderClaude, Unrecognized, ".claude/hooks/cerne-work-gate.sh"},
		{"a content no release wrote (a newer cerne) is unrecognized", currentProject, edit(".claude/skills/cerne-authoring/SKILL.md", "written by cerne v9\n"), hooks.ProviderClaude, Unrecognized, ".claude/skills/cerne-authoring/SKILL.md"},
		{"a new file of this version missing, the rest old, is renewable", fixtureV0143, remove(".claude/hooks/cerne-work-gate.sh"), hooks.ProviderClaude, Renewable, ""},
		{"an edit in one agent leaves the others renewable", fixtureV0143, edit(".claude/hooks/cerne-work-gate.sh", "mine\n"), hooks.ProviderCursor, Renewable, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := tc.project(t)
			if tc.change != nil {
				tc.change(t, dir)
			}
			v := Classify(dir, tc.agent, true)
			if v.State != tc.want {
				t.Fatalf("state = %v (unknown %v), want %v", v.State, v.Unknown, tc.want)
			}
			if tc.unknown != "" && !slices.Contains(v.Unknown, tc.unknown) {
				t.Fatalf("unknown = %v, want it to carry %s", v.Unknown, tc.unknown)
			}
			if tc.want != Unrecognized && len(v.Unknown) != 0 {
				t.Fatalf("a %v agent lists unknown items: %v", v.State, v.Unknown)
			}
		})
	}
}

// An agent that is not connected has nothing to classify or to renew.
func TestClassifyUnconnectedAgentIsLeftAlone(t *testing.T) {
	dir := t.TempDir()
	if _, err := hooks.Install(dir, hooks.ProviderClaude); err != nil {
		t.Fatal(err)
	}
	if v := Classify(dir, hooks.ProviderCodex, true); v.State != UpToDate || len(v.Unknown) != 0 {
		t.Fatalf("an unconnected Codex must not be classified, got %+v", v)
	}
	if slices.Contains(Connected(dir), hooks.ProviderCodex) {
		t.Fatal("Codex is not connected here")
	}
}

// Every agent the build installs is recognized as its own work (T1).
func TestKnownRecognizesWhatThisBuildWrites(t *testing.T) {
	dir := currentProject(t)
	for _, p := range AllProviders {
		if v := Classify(dir, p, true); v.State != UpToDate {
			t.Errorf("%s: %v, unknown %v", p, v.State, v.Unknown)
		}
	}
}

// C12: one leftover file of a retired release does not make an agent connected.
func TestClassifyLoneLeftoverIsNotAConnection(t *testing.T) {
	dir := t.TempDir()
	rel := ".claude/hooks/cerne-lifecycle-reminder.sh"
	if err := os.MkdirAll(filepath.Join(dir, ".claude", "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, rel), []byte("edited leftover\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if v := Classify(dir, hooks.ProviderClaude, true); v.State != UpToDate || len(v.Unknown) != 0 {
		t.Fatalf("a lone leftover file connected Claude: %+v", v)
	}
}

// Decision 3: with sync_skills on, the guide is left out of the verdict.
func TestClassifyWithoutTheGuide(t *testing.T) {
	dir := fixtureV0143(t)
	if err := os.WriteFile(filepath.Join(dir, ".claude/skills/cerne-authoring/SKILL.md"), []byte("edited guide\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if v := Classify(dir, hooks.ProviderClaude, true); v.State != Unrecognized {
		t.Fatalf("with the guide: %+v, want unrecognized", v)
	}
	if v := Classify(dir, hooks.ProviderClaude, false); v.State != Renewable || len(v.Unknown) != 0 {
		t.Fatalf("without the guide: %+v, want renewable", v)
	}
}
