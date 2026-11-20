package refresh

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// F-0477 crit. 8: what this build writes must be in the committed list, so a
// managed file changed without `go run ./tools/knowncontents` fails here. It
// needs no tags, so it never skips in CI.
func TestKnownCoversTheCurrentBuild(t *testing.T) {
	if len(Current()) == 0 {
		t.Fatal("this build's install read back no managed item")
	}
	if missing := missingContent(Current(), known); len(missing) > 0 {
		t.Fatalf("known_contents.json lacks this build's content for %s: run `go run ./tools/knowncontents` and commit it", strings.Join(missing, ", "))
	}
}

func TestKnownMissingContentFails(t *testing.T) {
	cur := map[string]string{".claude/hooks/a.sh": "new", "AGENTS.md": "same"}
	list := Contents{Known: map[string][]string{".claude/hooks/a.sh": {"old"}, "AGENTS.md": {"same"}}}
	got := missingContent(cur, list)
	if len(got) != 1 || got[0] != ".claude/hooks/a.sh" {
		t.Fatalf("missing = %v, want the changed script named by path", got)
	}
	list.Known[".claude/hooks/a.sh"] = append(list.Known[".claude/hooks/a.sh"], "new")
	if got := missingContent(cur, list); len(got) != 0 {
		t.Fatalf("a covered build still fails: %v", got)
	}
}

// C3: reading this build's install back writes nothing outside its scratch
// folder: not the home, not the XDG config.
func TestCurrentWritesOnlyInItsScratchFolder(t *testing.T) {
	home, xdg := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", xdg)
	if err := InstallAll(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{home, xdg} {
		if entries, _ := os.ReadDir(d); len(entries) > 0 {
			t.Errorf("the install wrote into %s: %v", filepath.Base(d), entries)
		}
	}
}
