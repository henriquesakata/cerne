package cli

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/store"
)

// F-0875 fixture: three implemented features (results in internal/app/testdata/accept,
// never the real .docs) plus one in development. F-0902 is the oldest, F-0903 the newest.
func acceptFixture(t *testing.T) (projDir, repoDir string) {
	t.Helper()
	projDir, _, repoDir = setupProject(t)
	base := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	seed := []struct {
		id    string
		state domain.FeatureState
		at    time.Time
	}{
		{"F-0901", domain.StateImplemented, base.Add(24 * time.Hour)},
		{"F-0902", domain.StateImplemented, base},
		{"F-0903", domain.StateImplemented, base.Add(48 * time.Hour)},
		{"F-0904", domain.StateInDevelopment, base.Add(-24 * time.Hour)},
	}
	for _, s := range seed {
		if err := store.SaveFeature(repoDir, store.Feature{ID: s.id, Title: "Feat " + s.id, State: s.state, CreatedAt: s.at, UpdatedAt: s.at}); err != nil {
			t.Fatal(err)
		}
		src, err := os.ReadFile(filepath.Join("..", "app", "testdata", "accept", s.id+".md"))
		if err != nil {
			continue // F-0904 has no result: not in the queue anyway
		}
		dst := filepath.Join(repoDir, "content", "features", s.id, "result.md")
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatal(err)
		}
		mustWrite(t, dst, string(src))
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed accept queue"); err != nil {
		t.Fatal(err)
	}
	return projDir, repoDir
}

// treeHash fingerprints every file of the backend outside .git.
func treeHash(t *testing.T, root string) string {
	t.Helper()
	h := sha256.New()
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".git" {
			return fs.SkipDir
		}
		if !d.IsDir() {
			b, _ := os.ReadFile(p)
			fmt.Fprintf(h, "%s\x00%x\n", p, sha256.Sum256(b))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// crit. 1: only implemented features, oldest first.
func TestAcceptQueueOrderOldestFirst(t *testing.T) {
	projDir, repoDir := acceptFixture(t)
	rows, err := app.AcceptQueue(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	if got := strings.Join(ids, ","); got != "F-0902,F-0901,F-0903" {
		t.Fatalf("order = %s, want F-0902,F-0901,F-0903 (F-0904 is in development)", got)
	}
	out := rb(t, projDir, "accept", "queue")
	if i, j, k := strings.Index(out, "F-0902  2026"), strings.Index(out, "F-0901  2026"), strings.Index(out, "F-0903  2026"); !(0 <= i && i < j && j < k) {
		t.Fatalf("text order wrong:\n%s", out)
	}
	if strings.Contains(out, "F-0904  ") {
		t.Fatalf("an in-development feature is in the queue:\n%s", out)
	}
}

// crit. 2: the row carries the verdict, the production evidence and the pendings.
func TestAcceptQueueRowFields(t *testing.T) {
	_, repoDir := acceptFixture(t)
	rows, _ := app.AcceptQueue(repoDir)
	by := map[string]app.AcceptRow{}
	for _, r := range rows {
		by[r.ID] = r
	}
	full := by["F-0901"]
	if full.Gauntlet != "vitória" { // the last round decides: round 1 derrota, round 2 vitória
		t.Errorf("F-0901 gauntlet = %q", full.Gauntlet)
	}
	if !strings.Contains(full.Production, "conferido em 2026-11-05") {
		t.Errorf("F-0901 production = %q", full.Production)
	}
	if !full.PendingReported || len(full.Pending) != 3 { // one PENDENTE line + two débitos
		t.Errorf("F-0901 pending = %v (reported %v), want 3", full.Pending, full.PendingReported)
	}
	if g := by["F-0903"].Gauntlet; g != "empate" {
		t.Errorf("F-0903 gauntlet = %q", g)
	}
	if p := by["F-0902"].Production; !strings.Contains(p, "aplicado pelo dono") {
		t.Errorf("F-0902 production = %q", p)
	}
}

// crit. 3: a missing field reads "não informado", never inferred.
func TestAcceptQueueMissingIsNotInformed(t *testing.T) {
	projDir, repoDir := acceptFixture(t)
	rows, _ := app.AcceptQueue(repoDir)
	for _, r := range rows {
		switch r.ID {
		case "F-0902": // no Gauntlet section, no pendings section
			if r.Gauntlet != app.NotInformed || r.PendingReported {
				t.Errorf("F-0902 = gauntlet %q, pending reported %v", r.Gauntlet, r.PendingReported)
			}
		case "F-0903": // no production evidence; "Nenhum" débitos is reported, zero pendings
			if r.Production != app.NotInformed || !r.PendingReported || len(r.Pending) != 0 {
				t.Errorf("F-0903 = production %q, pending %v reported %v", r.Production, r.Pending, r.PendingReported)
			}
		}
	}
	out := rb(t, projDir, "accept", "queue")
	if n := strings.Count(out, "não informado"); n != 3 { // F-0902: gauntlet+pending; F-0903: production
		t.Fatalf("want 3 'não informado' in the text, got %d:\n%s", n, out)
	}
}

// crit. 4: read-only; the backend is byte-identical after the command.
func TestAcceptQueueReadOnly(t *testing.T) {
	projDir, repoDir := acceptFixture(t)
	before := treeHash(t, repoDir)
	rb(t, projDir, "accept", "queue")
	stdout, _, err := execCerneSplit(t, "accept", "queue", "--json", "--dir", projDir)
	if err != nil || stdout == "" {
		t.Fatalf("json: %v", err)
	}
	if after := treeHash(t, repoDir); after != before {
		t.Fatal("the backend changed after 'accept queue'")
	}
	rows, _ := app.AcceptQueue(repoDir)
	for _, r := range rows {
		if f, _ := store.LoadFeature(repoDir, r.ID); f.State != domain.StateImplemented {
			t.Fatalf("%s left implemented: %s", r.ID, f.State)
		}
	}
}

// crit. 5 + contract of the output: the batch is individual accept commands; the JSON keeps the shape.
func TestAcceptQueueBatchCommands(t *testing.T) {
	projDir, _ := acceptFixture(t)
	out := rb(t, projDir, "accept", "queue")
	tail := out[strings.Index(out, "To accept"):]
	want := "  cerne feature accept F-0902\n  cerne feature accept F-0901\n  cerne feature accept F-0903\n"
	if !strings.HasSuffix(strings.TrimRight(tail, "\n")+"\n", want) || strings.Contains(strings.ToLower(tail), "all") {
		t.Fatalf("batch tail = %q", tail)
	}
	stdout, _, err := execCerneSplit(t, "accept", "queue", "--json", "--dir", projDir)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Queue []map[string]any `json:"queue"`
		Cmds  []string         `json:"accept_commands"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("json: %v\n%s", err, stdout)
	}
	if len(got.Queue) != 3 || len(got.Cmds) != 3 || got.Cmds[0] != "cerne feature accept F-0902" {
		t.Fatalf("json = %+v", got)
	}
	for _, k := range []string{"id", "title", "implemented_at", "gauntlet", "production", "pending", "pending_reported", "accept_command"} {
		if _, ok := got.Queue[0][k]; !ok {
			t.Errorf("json row lacks %q", k)
		}
	}
	if got.Queue[0]["gauntlet"] != "não informado" {
		t.Errorf("json gauntlet of F-0902 = %v", got.Queue[0]["gauntlet"])
	}
}
