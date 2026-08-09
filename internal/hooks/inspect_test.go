package hooks

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScriptStateOKDivergentMissing(t *testing.T) {
	proj := t.TempDir()
	if _, err := Install(proj); err != nil {
		t.Fatal(err)
	}
	for _, a := range Assets() {
		if got := ScriptState(proj, a.Script); got != FileOK {
			t.Errorf("%s after install = %s, want ok", a.Script, got)
		}
	}
	target := filepath.Join(proj, ".claude", "hooks", Assets()[0].Script)
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	raw[0] ^= 1
	if err := os.WriteFile(target, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ScriptState(proj, Assets()[0].Script); got != FileDivergent {
		t.Errorf("one flipped byte = %s, want divergent", got)
	}
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if got := ScriptState(proj, Assets()[0].Script); got != FileMissing {
		t.Errorf("removed = %s, want missing", got)
	}
}

func TestGitHookStates(t *testing.T) {
	proj := t.TempDir()
	if err := os.MkdirAll(filepath.Join(proj, ".git", "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	states := map[string]string{}
	for _, s := range GitHookStates(proj) {
		states[s.Name] = s.State
	}
	if states[trailerHookName] != FileMissing || states[guardHookName] != FileMissing {
		t.Errorf("empty .git/hooks = %v, want both missing", states)
	}
	if _, err := Install(proj); err != nil {
		t.Fatal(err)
	}
	for _, s := range GitHookStates(proj) {
		if s.State != FileOK {
			t.Errorf("%s after install = %s, want ok", s.Name, s.State)
		}
	}
	// Foreign content in the slot is divergent with the manual snippet.
	if err := os.WriteFile(filepath.Join(proj, ".git", "hooks", trailerHookName), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, s := range GitHookStates(proj) {
		if s.Name == trailerHookName {
			if s.State != FileDivergent || s.Detail == "" {
				t.Errorf("foreign trailer = %+v, want divergent with detail", s)
			}
		}
	}
}
