package cli

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/lint"
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

func gitHead(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("/usr/bin/git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
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
	// The text proves it field by field: "não informado" exactly where the
	// result lacks the field, and a value where it has one.
	fields := map[string]map[string]string{}
	cur := ""
	for _, line := range strings.Split(rb(t, projDir, "accept", "queue"), "\n") {
		if len(line) > 6 && strings.HasPrefix(line, "F-09") {
			cur = line[:6]
			fields[cur] = map[string]string{}
		} else if k, v, ok := strings.Cut(strings.TrimSpace(line), ": "); ok && cur != "" {
			fields[cur][k] = v
		}
	}
	want := map[string]map[string]bool{ // true = "não informado"
		"F-0901": {"gauntlet": false, "production": false, "pending": false},
		"F-0902": {"gauntlet": true, "production": false, "pending": true},
		"F-0903": {"gauntlet": false, "production": true, "pending": false},
	}
	for id, fs := range want {
		for field, absent := range fs {
			if got := fields[id][field] == "não informado"; got != absent {
				t.Errorf("%s %s = %q, 'não informado' want %v", id, field, fields[id][field], absent)
			}
		}
	}
}

// crit. 4: read-only; the backend is byte-identical after the command.
func TestAcceptQueueReadOnly(t *testing.T) {
	projDir, repoDir := acceptFixture(t)
	before, head := treeHash(t, repoDir), gitHead(t, repoDir)
	rb(t, projDir, "accept", "queue")
	stdout, _, err := execCerneSplit(t, "accept", "queue", "--json", "--dir", projDir)
	if err != nil || stdout == "" {
		t.Fatalf("json: %v", err)
	}
	if after := treeHash(t, repoDir); after != before {
		t.Fatal("the backend changed after 'accept queue'")
	}
	if gitHead(t, repoDir) != head {
		t.Fatal("'accept queue' made a commit")
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

// Refusals: every bad call is an explicit error, never ignored.
func TestAcceptQueueRefusals(t *testing.T) {
	projDir, _ := acceptFixture(t)
	for _, args := range [][]string{
		{"accept", "queue", "F-0901"},          // an id: the queue takes none
		{"accept", "queue", "--bogus"},         // unknown flag
		{"accept", "queue", "--json=maybe"},    // invalid flag value
		{"accept", "queue", "--json", "--all"}, // "accept all" does not exist (spec: no batch accept)
		{"accept", "all"},                      // unknown subcommand
	} {
		out, err := execCerne(t, append(args, "--dir", projDir)...)
		if err == nil {
			t.Errorf("%v: want an error, got output:\n%s", args, out)
		}
	}
}

// A missing, empty or garbage result is "não informado", never a crash or a guess.
func TestAcceptQueueMalformedResult(t *testing.T) {
	_, repoDir := acceptFixture(t)
	for name, body := range map[string]string{
		"F-0901": "",                                       // empty
		"F-0902": "\x00\x01\xff## Gauntlet\n```\nunclosed", // binary + unclosed fence
	} {
		mustWrite(t, filepath.Join(repoDir, "content", "features", name, "result.md"), body)
	}
	if err := os.Remove(filepath.Join(repoDir, "content", "features", "F-0903", "result.md")); err != nil { // missing
		t.Fatal(err)
	}
	rows, err := app.AcceptQueue(repoDir)
	if err != nil || len(rows) != 3 {
		t.Fatalf("rows=%d err=%v", len(rows), err)
	}
	for _, r := range rows {
		if r.Gauntlet != app.NotInformed || r.Production != app.NotInformed || r.PendingReported {
			t.Errorf("%s = %+v, want everything not informed", r.ID, r)
		}
	}
}

// Markers are anchored (heading or line start, outside fences): loose prose that
// contains them is not captured.
func TestAcceptQueueProseIsNotCaptured(t *testing.T) {
	_, repoDir := acceptFixture(t)
	prose := "## Resultado\n\nO dono disse que Em produção: tudo certo e que ficou PENDENTE o resto.\n" +
		"Ele repetiu: veredito: Rodada 1 — vitória: \"sim\".\n\n```\nProdução: dentro de código\n- critério 1 — PENDENTE\n```\n"
	mustWrite(t, filepath.Join(repoDir, "content", "features", "F-0901", "result.md"), prose)
	rows, _ := app.AcceptQueue(repoDir)
	for _, r := range rows {
		if r.ID == "F-0901" && (r.Production != app.NotInformed || r.PendingReported || r.Gauntlet != app.NotInformed) {
			t.Fatalf("prose captured: %+v", r)
		}
	}
}

// queueRow writes body as F-0901's result.md and returns that feature's row.
func queueRow(t *testing.T, repoDir, body string) app.AcceptRow {
	t.Helper()
	mustWrite(t, filepath.Join(repoDir, "content", "features", "F-0901", "result.md"), body)
	rows, err := app.AcceptQueue(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.ID == "F-0901" {
			return r
		}
	}
	t.Fatal("F-0901 not in the queue")
	return app.AcceptRow{}
}

// r1: the pendings count is first-level bullets of the section; "Nenhum" in any
// spelling, prose, sub-bullets and tables are not pendings; subsections stay in.
func TestAcceptQueuePendingCount(t *testing.T) {
	_, repoDir := acceptFixture(t)
	for name, tc := range map[string]struct {
		body string
		want int
	}{
		"nenhum bullet":       {"## Débitos\n\n- Nenhum.\n", 0},
		"nenhum débito":       {"## Débitos e follow-ups\n\n- Nenhum débito.\n", 0},
		"nenhum lower":        {"## Débitos\n\n* nenhum\n", 0},
		"n/a":                 {"## Débitos\n\n- n/a\n", 0},
		"dash":                {"## Débitos\n\n- —\n", 0},
		"nenhuma but real":    {"## Débitos\n\n- Nenhuma cobertura do caminho X; falta o teste Y.\n", 1},
		"prose lead-in":       {"## Débitos\n\nOs débitos são:\n\n- a\n- b\n- c\n", 3},
		"sub-bullets":         {"## Débitos\n\n- a\n  - a1\n  - a2\n- b\n    - b1\n", 2},
		"continuation":        {"## Débitos\n\n- a\n  continua aqui\n- b\n", 2},
		"nenhum teste real":   {"## Débitos\n\n- Nenhum teste de carga foi escrito\n", 1},
		"nenhum long":         {"## Débitos\n\n- Nenhum débito aberto além dos já listados no corpo do Resultado.\n", 1}, // F-0887: a continuation past the qualifier declares a debt
		"não há débitos":      {"## Débitos\n\n- Não há débitos.\n", 0},
		"h3 inside":           {"## Débitos\n\n### Segurança\n\n- a\n\n### Testes\n\n- b\n- c\n", 3},
		"h2 closes":           {"## Débitos\n\n- a\n\n## Gauntlet\n\n- x\n", 1},
		"h1 closes":           {"## Débitos\n\n- a\n\n# Outro\n\n- x\n", 1},
		"hash line in body":   {"## Débitos\n\n- a\n#123 ainda é corpo\n- b\n", 2},
		"hash line in fence":  {"## Débitos\n\n- a\n```\n# comentário\n```\n- b\n", 2},
		"h3 start not opened": {"### Débitos\n\n- a\n\n#### Sub\n\n- b\n\n### Outro\n\n- x\n", 2},
	} {
		if r := queueRow(t, repoDir, tc.body); len(r.Pending) != tc.want || !r.PendingReported {
			t.Errorf("%s: pending %v (reported %v), want %d", name, r.Pending, r.PendingReported, tc.want)
		}
	}
}

// r1: a negative production line is not evidence; only a positive one is.
func TestAcceptQueueProductionNegativeIsNotEvidence(t *testing.T) {
	_, repoDir := acceptFixture(t)
	for name, body := range map[string]string{
		"nao verificado":       "- Em produção: não verificado\n",
		"pendente":             "Produção: PENDENTE\n",
		"bold nao verificado":  "**Em produção:** não verificado.\n",
		"prose with colon":     "Produção ainda não foi tocada; ver: X\n",
		"empty value":          "Produção:\n",
		"title nao verificado": "## Não verificado em produção\n\n- o prazo\n",
		"title only":           "## Produção\n\n",
		"não foi verificado":   "- Em produção: não foi verificado\n",
		"não se aplica":        "Produção: não se aplica\n",
		"aguardando":           "Produção: aguardando o dono\n",
		"section two lines":    "## Produção\n\nNão verificado.\n\nO deploy foi adiado.\n",
		"title then negative":  "## Produção\n\nNão verificado.\n",
	} {
		if r := queueRow(t, repoDir, body); r.Production != app.NotInformed {
			t.Errorf("%s: production = %q, want %q", name, r.Production, app.NotInformed)
		}
	}
	if r := queueRow(t, repoDir, "## Não verificado em produção\n\n- o prazo\n"); !r.PendingReported || len(r.Pending) != 1 {
		t.Errorf("the 'não verificado' section must be a pending: %+v", r)
	}
	for name, body := range map[string]string{
		"line":     "- Em produção: conferido em 2026-11-05\n",
		"bold":     "**Produção:** aplicado pelo dono\n",
		"section":  "## Produção\n\nAplicado em 2026-11-06, health verde.\n",
		"na sexta": "Produção: na sexta o dono aplicou, health verde\n",
		"sem erro": "- Em produção: sem erro nos logs por 24h\n",
	} {
		if r := queueRow(t, repoDir, body); r.Production == app.NotInformed {
			t.Errorf("%s: positive evidence read as not informed", name)
		}
	}
}

// F-0887 crit. 4 (P-0019): "nada" and "tbd" on the production line are no
// evidence (mutants B and E of F-0875 round 3).
func TestProductionNadaTbdIsNotInformed(t *testing.T) {
	_, repoDir := acceptFixture(t)
	for name, body := range map[string]string{
		"nada":       "Produção: nada\n",
		"nada ainda": "- Em produção: nada ainda\n",
		"tbd":        "Produção: TBD\n",
		"tbd bold":   "**Produção:** tbd.\n",
		"section":    "## Produção\n\nTBD\n",
	} {
		if r := queueRow(t, repoDir, body); r.Production != app.NotInformed {
			t.Errorf("%s: production = %q, want %q", name, r.Production, app.NotInformed)
		}
	}
}

// F-0887: one synthetic result per criterion (testdata/accept/F-0905..7), read
// end to end by 'accept queue --json'.
func TestAcceptQueueF0887Fixtures(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	for _, id := range []string{"F-0905", "F-0906", "F-0907"} {
		if err := store.SaveFeature(repoDir, store.Feature{ID: id, Title: "Feat " + id, State: domain.StateImplemented, CreatedAt: at, UpdatedAt: at}); err != nil {
			t.Fatal(err)
		}
		src, err := os.ReadFile(filepath.Join("..", "app", "testdata", "accept", id+".md"))
		if err != nil {
			t.Fatal(err)
		}
		dst := filepath.Join(repoDir, "content", "features", id, "result.md")
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatal(err)
		}
		mustWrite(t, dst, string(src))
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed F-0887 fixtures"); err != nil {
		t.Fatal(err)
	}
	stdout, _, err := execCerneSplit(t, "accept", "queue", "--json", "--dir", projDir)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Queue []app.AcceptRow `json:"queue"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("%v: %s", err, stdout)
	}
	got := map[string]string{}
	for _, r := range doc.Queue {
		got[r.ID] = fmt.Sprintf("production=%s pending=%d reported=%v", r.Production, len(r.Pending), r.PendingReported)
	}
	for id, want := range map[string]string{
		"F-0905": "production=não informado pending=3 reported=true", // crit. 1
		"F-0906": "production=não informado pending=0 reported=true", // crit. 2
		"F-0907": "production=não informado pending=2 reported=true", // crit. 3
	} {
		if got[id] != want {
			t.Errorf("%s: %s, want %s", id, got[id], want)
		}
	}
}

// r1: the verdict on a line of its own reads as the lint reads it.
func TestAcceptQueueVerdictLineOfItsOwn(t *testing.T) {
	_, repoDir := acceptFixture(t)
	if r := queueRow(t, repoDir, "## Gauntlet\n\nBarra: o fluxo do cerne.\n\nVeredito: derrota\n"); r.Gauntlet != "derrota" {
		t.Fatalf("gauntlet = %q, want derrota", r.Gauntlet)
	}
}

// r2: Débitos as a table or prose declares no bullet list: "não informado",
// never "0"; an HTML comment (template scaffold) is no evidence and no pending.
func TestAcceptQueueProseTableAndCommentsAreNotInformed(t *testing.T) {
	_, repoDir := acceptFixture(t)
	for name, body := range map[string]string{
		"table": "## Débitos\n\n| Débito | Dono |\n|---|---|\n| a | x |\n| b | y |\n",
		"prose": "## Débitos\n\nLimpar o seed antigo.\n",
	} {
		if r := queueRow(t, repoDir, body); r.PendingReported || len(r.Pending) != 0 {
			t.Errorf("%s: reported %v pending %v, want not reported", name, r.PendingReported, r.Pending)
		}
	}
	r := queueRow(t, repoDir, "<!--\nProdução: descreva aqui\n- critério 1 — PENDENTE\n-->\n")
	if r.Production != app.NotInformed || len(r.Pending) != 0 || r.PendingReported {
		t.Errorf("comment read as record: %+v", r)
	}
}

// r2: an empty queue is "queue": [] in the JSON, never null.
func TestAcceptQueueEmptyJSON(t *testing.T) {
	projDir, _, _ := setupProject(t)
	stdout, _, err := execCerneSplit(t, "accept", "queue", "--json", "--dir", projDir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, `"queue": []`) || !strings.Contains(stdout, `"accept_commands": []`) {
		t.Fatalf("empty queue json = %s", stdout)
	}
}

// r2: the queue and the lint read the same record: "derrota" in the queue iff
// the lint counts a derrota. "VEREDITO: A" (the blind judge's letter) is no
// outcome word for either: the queue says "não informado", the lint counts none.
func TestAcceptQueueAgreesWithLintOnDerrota(t *testing.T) {
	_, repoDir := acceptFixture(t)
	for name, body := range map[string]string{
		"derrota":   "## Gauntlet\n\nBarra: x.\n\nRodada 1 — derrota: \"a\"\n",
		"vitória":   "## Gauntlet\n\nBarra: x.\n\nRodada 1 — vitória: \"a\"\n",
		"line":      "## Gauntlet\n\nBarra: x.\n\nVeredito: derrota\n",
		"letter":    "## Gauntlet\n\nBarra: x.\n\nVEREDITO: A\n",
		"commented": "<!--\n## Gauntlet\n\nRodada 1 — derrota: \"a\"\n-->\n",
	} {
		_, derrotas := lint.GauntletRounds([]byte(body))
		if got := queueRow(t, repoDir, body).Gauntlet == "derrota"; got != (derrotas > 0) {
			t.Errorf("%s: queue derrota %v, lint derrotas %d", name, got, derrotas)
		}
	}
	if g := queueRow(t, repoDir, "## Gauntlet\n\nBarra: x.\n\nVEREDITO: A\n").Gauntlet; g != app.NotInformed {
		t.Errorf("letter verdict = %q, want %q", g, app.NotInformed)
	}
}
