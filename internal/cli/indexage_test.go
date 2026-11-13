package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/index"
)

var indexAgeClock = time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)

const indexAgeSuffix = "; entities written on other machines may be missing (run cerne pull)\n"

// ageFixture is a project whose index was last synced `age` ago per the
// injected clock (zero age = no recorded time). remote swaps the backend in
// .cernerc for an ssh URL, so the local-path silence (crit. 8) does not apply.
func ageFixture(t *testing.T, age time.Duration, remote bool) (projDir string) {
	t.Helper()
	projDir, origin, repoDir := setupProject(t)
	id := newFeature(t, projDir, "Parcelamento", 1)
	writeDoc(t, repoDir, id, "spec.md", realDoc(id, "Dividir compras em parcelas mensais sem juros."))
	if _, err := gitback.CommitAndPush(repoDir, "spec F-0001"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}
	var at time.Time
	if age > 0 {
		at = indexAgeClock.Add(-age)
	}
	if err := index.Rebuild(repoDir, indexPathFor(repoDir), at); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if remote {
		rc := filepath.Join(projDir, ".cernerc")
		b, err := os.ReadFile(rc)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(rc, []byte(strings.ReplaceAll(string(b), origin, "git@example.com:o/r.git")), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	now = func() time.Time { return indexAgeClock }
	t.Cleanup(func() { now = time.Now })
	return projDir
}

func TestIndexAgeNoteOnSearchAndAsk(t *testing.T) {
	cases := []struct {
		name    string
		age     time.Duration
		remote  bool
		wantErr string // exact stderr
	}{
		{"25h", 25 * time.Hour, true, "note: search index last synced 25h ago" + indexAgeSuffix},
		{"1h", time.Hour, true, ""},
		{"unknown", 0, true, "note: search index last synced: unknown" + indexAgeSuffix},
		{"25h local backend", 25 * time.Hour, false, ""},
		{"unknown local backend", 0, false, ""},
	}
	for _, c := range cases {
		for _, verb := range []string{"search", "ask"} {
			t.Run(verb+"/"+c.name, func(t *testing.T) {
				projDir := ageFixture(t, c.age, c.remote)
				stdout, stderr, err := execCerneSplit(t, verb, "parcelas", "--dir", projDir)
				if err != nil {
					t.Fatalf("%s: %v\n%s", verb, err, stderr)
				}
				if stderr != c.wantErr {
					t.Fatalf("stderr = %q, want %q", stderr, c.wantErr)
				}
				if strings.Contains(stdout, "note: search index") {
					t.Fatalf("the note leaked into stdout:\n%s", stdout)
				}
			})
		}
	}
}

func TestIndexSyncedAtInJSON(t *testing.T) {
	for _, verb := range []string{"search", "ask"} {
		for _, c := range []struct {
			name   string
			age    time.Duration
			remote bool
			want   any
		}{
			{"25h", 25 * time.Hour, true, "2026-11-12T11:00:00Z"},
			{"unknown", 0, true, nil},
			{"25h local backend", 25 * time.Hour, false, "2026-11-12T11:00:00Z"},
		} {
			t.Run(verb+"/"+c.name, func(t *testing.T) {
				projDir := ageFixture(t, c.age, c.remote)
				stdout, stderr, err := execCerneSplit(t, verb, "parcelas", "--json", "--dir", projDir)
				if err != nil {
					t.Fatalf("%s --json: %v\n%s", verb, err, stderr)
				}
				if stderr != "" {
					t.Fatalf("--json must not write the note; stderr = %q", stderr)
				}
				var m map[string]any
				if err := json.Unmarshal([]byte(stdout), &m); err != nil {
					t.Fatalf("not JSON: %v\n%s", err, stdout)
				}
				got, present := m["index_synced_at"]
				if !present || got != c.want {
					t.Fatalf("index_synced_at = %v (present %v), want %v", got, present, c.want)
				}
			})
		}
	}
}

// F-0394 crit. 2: the CLI root turns IndexNotice into the stderr warning and
// the write still exits 0.
func TestIndexRefreshFailureWarnsOnStderr(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	if err := os.MkdirAll(indexPathFor(repoDir), 0o755); err != nil { // a directory where index.db should be
		t.Fatal(err)
	}
	_, stderr, err := execCerneSplit(t, "feature", "new", "Login", "--context", "ctx", "--dir", projDir)
	if err != nil {
		t.Fatalf("feature new must succeed: %v\n%s", err, stderr)
	}
	if !strings.Contains(stderr, "warning: search index not refreshed: ") || !strings.Contains(stderr, "; run cerne pull\n") {
		t.Fatalf("stderr lacks the index warning:\n%s", stderr)
	}
}
