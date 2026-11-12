package project

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// F-0355 crit. 1 and 6: the forge and the PR template round-trip; an unknown
// forge is refused at load with the message the spec fixes.
func TestConfigDeliveryForge(t *testing.T) {
	for _, forge := range []string{"github", "gitlab", "gitea"} {
		dir := t.TempDir()
		want := Config{ProjectID: "c", Backend: "/b",
			Delivery: DeliveryConfig{CommitURL: "https://h/o/r/commit/{sha}", Forge: forge, PRURL: "https://h/o/r/pull/{n}"}}
		if err := SaveConfig(dir, want); err != nil {
			t.Fatalf("SaveConfig: %v", err)
		}
		got, _, err := LoadConfig(dir)
		if err != nil {
			t.Fatalf("forge %s: LoadConfig: %v", forge, err)
		}
		if got.Delivery != want.Delivery {
			t.Fatalf("forge %s: got %+v, want %+v", forge, got.Delivery, want.Delivery)
		}
	}
	dir := t.TempDir()
	rc := "project_id = \"c\"\nbackend = \"/b\"\n[delivery]\nforge = \"bitbucket\"\n"
	if err := os.WriteFile(filepath.Join(dir, ".cernerc"), []byte(rc), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := LoadConfig(dir)
	if !errors.Is(err, ErrDeliveryForge) {
		t.Fatalf("err = %v, want ErrDeliveryForge", err)
	}
	const msg = `unknown [delivery] forge "bitbucket" (supported: github, gitlab, gitea)`
	if !strings.Contains(err.Error(), msg) {
		t.Fatalf("err = %q, want it to contain %q", err, msg)
	}
}
