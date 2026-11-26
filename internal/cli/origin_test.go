package cli

import (
	"encoding/json"
	"errors"
	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/store"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShowPrintsOrigin(t *testing.T) {
	t.Setenv("CERNE_ACTOR", "")
	projDir, _, _ := setupProject(t)
	newOriginFeature(t, projDir, "Pedida por humano")
	out, err := execCerne(t, "show", "F-0001", "--dir", projDir)
	if err != nil || !strings.Contains(out, "origin: human") {
		t.Fatalf("show text: %v\n%s", err, out)
	}
	out, err = execCerne(t, "show", "F-0001", "--json", "--dir", projDir)
	if err != nil || !strings.Contains(out, `"origin": "human"`) {
		t.Fatalf("show json: %v\n%s", err, out)
	}
}

func TestFeatureListFiltersOrigin(t *testing.T) {
	projDir, _, _ := setupProject(t)
	t.Setenv("CERNE_ACTOR", "")
	newOriginFeature(t, projDir, "Do humano")
	t.Setenv("CERNE_ACTOR", "agent")
	newOriginFeature(t, projDir, "Do agente", "--origin", "agent")
	newOriginFeature(t, projDir, "Repassada", "--on-behalf", "o dono pediu")

	ids := func(origin string) string {
		out, err := execCerne(t, "feature", "list", "--origin", origin, "--json", "--dir", projDir)
		if err != nil {
			t.Fatalf("list --origin %s: %v\n%s", origin, err, out)
		}
		var r struct {
			Features []struct{ ID, Origin string } `json:"features"`
		}
		if err := json.Unmarshal([]byte(out), &r); err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, f := range r.Features {
			if f.Origin != origin {
				t.Fatalf("%s has origin %q under --origin %s", f.ID, f.Origin, origin)
			}
			got = append(got, f.ID)
		}
		return strings.Join(got, ",")
	}
	if g := ids("human"); g != "F-0001,F-0003" {
		t.Fatalf("human = %s", g)
	}
	if g := ids("agent"); g != "F-0002" {
		t.Fatalf("agent = %s", g)
	}
	if _, err := execCerne(t, "feature", "list", "--origin", "bot", "--dir", projDir); err == nil {
		t.Fatal("invalid --origin accepted")
	}
}

func TestLegacyCardOriginUnknown(t *testing.T) {
	t.Setenv("CERNE_ACTOR", "")
	projDir, _, repoDir := setupProject(t)
	newOriginFeature(t, projDir, "Antigo")
	p := filepath.Join(repoDir, "entities", "features", "F-0001.json")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	delete(m, "origin")
	b, _ = json.Marshal(m)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := execCerne(t, "show", "F-0001", "--json", "--dir", projDir)
	if err != nil || strings.Contains(out, `"origin"`) {
		t.Fatalf("legacy show: %v\n%s", err, out)
	}
	out, err = execCerne(t, "show", "F-0001", "--dir", projDir)
	if err != nil || strings.Contains(out, "origin:") {
		t.Fatalf("legacy text must not print an origin: %v\n%s", err, out)
	}
}

func TestOnBehalfExplicitEmptyRefused(t *testing.T) {
	t.Setenv("CERNE_ACTOR", "agent")
	projDir, _, repoDir := setupProject(t)
	for _, args := range [][]string{
		{"feature", "new", "A"},
		{"epic", "new", "B"},
		{"task", "new", "C", "--parent", "F-0001"},
	} {
		args = append(args, "--context", "ctx", "--on-behalf", "", "--dir", projDir)
		if _, err := execCerne(t, args...); !errors.Is(err, app.ErrOnBehalfEmpty) {
			t.Fatalf("%v: err = %v, want ErrOnBehalfEmpty", args[:2], err)
		}
	}
	if fs, _ := store.LoadAllFeatures(repoDir); len(fs) != 0 {
		t.Fatalf("%d cards written despite the refusal", len(fs))
	}
}
