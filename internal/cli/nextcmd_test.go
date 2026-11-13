package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/cost"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestNext pins the work queue: active features grouped by the action that
// advances them, lint debts in their own section, terminal/implemented work
// hidden, and exit 0 even with findings (it is informational, not a gate).
func TestNext(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	now := time.Now().UTC()
	features := []store.Feature{
		{ID: "F-0001", Title: "Em dev", Module: "core", State: domain.StateInDevelopment},
		{ID: "F-0002", Title: "Aprovada", State: domain.StateApproved},
		{ID: "F-0003", Title: "Candidata", State: domain.StateCandidate},
		{ID: "F-0004", Title: "Feature encerrada", State: domain.StateImplemented},
		{ID: "F-0005", Title: "Feature antiga", State: domain.StateSuperseded, SupersededBy: "F-0004"},
	}
	for _, f := range features {
		f.CreatedAt, f.UpdatedAt = now, now
		if err := store.SaveFeature(repoDir, f); err != nil {
			t.Fatalf("SaveFeature %s: %v", f.ID, err)
		}
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed next"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}

	// No docs were written, so the gated features trip missing-gate-doc — and
	// next must still exit 0 (informational), unlike cerne lint.
	out, err := execCerne(t, "next", "--dir", projDir)
	if err != nil {
		t.Fatalf("next must not fail with findings: %v\noutput: %s", err, out)
	}
	for _, want := range []string{
		"IN PROGRESS (1) — finish",
		"READY TO START (1) — run 'cerne feature start'",
		// F-0308: rehearse with --check; the approve itself is a human act.
		"CANDIDATES (1) — write the spec, run 'cerne feature approve <id> --check', then ask a human to approve (a human act)",
		"DEBTS (cerne lint):",
		"MISSING-GATE-DOC",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("next missing %q:\n%s", want, out)
		}
	}
	// F-0371: an implemented feature owes the agent its delivery; a terminal one is not queued.
	if !strings.Contains(out, "FINISH DELIVERY (1)") || !strings.Contains(out, "cerne feature deliver F-0004 --mr <url>") {
		t.Errorf("next missing the implemented feature's delivery step:\n%s", out)
	}
	for _, banned := range []string{"Feature antiga"} {
		if strings.Contains(out, banned) {
			t.Errorf("next leaks non-active feature %q:\n%s", banned, out)
		}
	}
}

// TestNextListsEpicsReadyToClose pins crit.2: an in_development epic whose
// children are all concluded (implemented) or dead (rejected) surfaces in its
// own block; an epic with a live child pending does not.
func TestNextListsEpicsReadyToClose(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	now := time.Now().UTC()
	features := []store.Feature{
		{ID: "F-0100", Title: "Épico pronto", State: domain.StateInDevelopment},
		{ID: "F-0101", Title: "Filha concluída", Parent: "F-0100", State: domain.StateImplemented},
		{ID: "F-0102", Title: "Filha rejeitada", Parent: "F-0100", State: domain.StateRejected, Reason: "não necessária"},
		{ID: "F-0200", Title: "Épico ainda em curso", State: domain.StateInDevelopment},
		{ID: "F-0201", Title: "Filha viva", Parent: "F-0200", State: domain.StateInDevelopment},
	}
	for _, f := range features {
		f.CreatedAt, f.UpdatedAt = now, now
		if err := store.SaveFeature(repoDir, f); err != nil {
			t.Fatalf("SaveFeature %s: %v", f.ID, err)
		}
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed next epics"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}

	out, err := execCerne(t, "next", "--dir", projDir)
	if err != nil {
		t.Fatalf("next: %v\noutput: %s", err, out)
	}
	i := strings.Index(out, "EPICS READY TO CLOSE (1)")
	if i < 0 {
		t.Fatalf("next missing epics-ready-to-close block:\n%s", out)
	}
	block := out[i:]
	if j := strings.Index(block, "\n\n"); j >= 0 {
		block = block[:j]
	}
	if !strings.Contains(block, "Épico pronto") {
		t.Errorf("epics-ready-to-close block missing F-0100:\n%s", block)
	}
	if strings.Contains(block, "Épico ainda em curso") {
		t.Errorf("epics-ready-to-close block lists epic with a live child pending:\n%s", block)
	}
}

// TestNextEmpty: a project with no active work and no debts says so, exit 0.
func TestNextEmpty(t *testing.T) {
	projDir, _, _ := setupProject(t)
	out, err := execCerne(t, "next", "--dir", projDir)
	if err != nil {
		t.Fatalf("next: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "Nothing pending. ✓") {
		t.Fatalf("empty project should report nothing pending:\n%s", out)
	}
}

// nextAfterCompleteProject seeds the implemented features the queue follows
// past complete (F-0371): which command is still missing for each.
func nextAfterCompleteProject(t *testing.T) (string, string) {
	t.Helper()
	projDir, _, repoDir := setupProject(t)
	now := time.Now().UTC()
	stamped, err := cost.Stamp("# Resultado\n", cost.Report{ID: "F-0012"})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []store.Feature{
		{ID: "F-0010", Title: "Sem custo e sem entrega", State: domain.StateImplemented},
		{ID: "F-0011", Title: "Sem vinculo e sem entrega", State: domain.StateImplemented},
		{ID: "F-0012", Title: "Completa", State: domain.StateImplemented, DeliveryCommitURL: "https://example.com/c/1"},
		{ID: "F-0013", Title: "Ja aceita", State: domain.StateDone},
		{ID: "F-0014", Title: "Sem custo, entregue por MR", State: domain.StateImplemented, DeliveryMRURL: "https://example.com/mr/1"},
	} {
		f.CreatedAt, f.UpdatedAt = now, now
		if err := store.SaveFeature(repoDir, f); err != nil {
			t.Fatalf("SaveFeature %s: %v", f.ID, err)
		}
	}
	dir := filepath.Join(repoDir, "content", "features", "F-0012")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "result.md"), []byte(stamped), 0o644); err != nil {
		t.Fatal(err)
	}
	// F-0010 and F-0014 have a bound session with a transcript; F-0012 is stamped.
	root := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	for i, id := range []string{"F-0010", "F-0014"} {
		writeTranscript(t, root, "sess-"+id+".jsonl", costLine("2026-11-01T10:02:00Z", "m"+strconv.Itoa(i), "claude-opus-5-5", "", 1, 2, 3, 4))
	}
	for _, id := range []string{"F-0010", "F-0012", "F-0014"} {
		costBind(t, filepath.Dir(repoDir), "sess-"+id, id, "2026-11-01T10:00:00Z")
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed next after complete"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}
	return projDir, repoDir
}

// nextBlock is the text block that starts at header, up to the blank line.
func nextBlock(out, header string) string {
	i := strings.Index(out, header)
	if i < 0 {
		return ""
	}
	b := out[i:]
	if j := strings.Index(b, "\n\n"); j >= 0 {
		b = b[:j]
	}
	return b
}

// TestNextFinishDelivery pins F-0371 crit.1-2, 5: an implemented feature
// missing its cost stamp (only with a session bound) or its delivery gets the
// exact missing command; a complete or done one is not in the group.
func TestNextFinishDelivery(t *testing.T) {
	projDir, _ := nextAfterCompleteProject(t)
	out, err := execCerne(t, "next", "--dir", projDir)
	if err != nil {
		t.Fatalf("next: %v\n%s", err, out)
	}
	blk := nextBlock(out, "FINISH DELIVERY (3)")
	if blk == "" {
		t.Fatalf("missing FINISH DELIVERY (3) group:\n%s", out)
	}
	for _, want := range []string{
		"cerne cost F-0010 --stamp", "cerne feature deliver F-0010 --mr <url>",
		"cerne feature deliver F-0011 --mr <url>",
		"cerne cost F-0014 --stamp",
	} {
		if !strings.Contains(blk, want) {
			t.Errorf("group missing %q:\n%s", want, blk)
		}
	}
	for _, banned := range []string{"cerne cost F-0011", "cerne feature deliver F-0014", "F-0012", "F-0013"} {
		if strings.Contains(blk, banned) {
			t.Errorf("group must not contain %q:\n%s", banned, blk)
		}
	}
	if strings.Index(out, "FINISH DELIVERY") > strings.Index(out, "DEBTS") && strings.Contains(out, "DEBTS") {
		t.Errorf("FINISH DELIVERY must precede the debts:\n%s", out)
	}

	js, err := execCerne(t, "next", "--dir", projDir, "--json")
	if err != nil {
		t.Fatalf("next --json: %v", err)
	}
	var q nextJSON
	if err := json.Unmarshal([]byte(js), &q); err != nil {
		t.Fatalf("json: %v\n%s", err, js)
	}
	for _, g := range q.Groups {
		if g.Queue != "finish_delivery" {
			continue
		}
		if g.State != domain.StateImplemented || len(g.Features) != 3 || len(g.Features[0].Commands) != 2 {
			t.Errorf("finish_delivery group = %+v", g)
		}
		return
	}
	t.Errorf("no finish_delivery group in json:\n%s", js)
}

// TestNextAwaitingAccept pins F-0371 crit.3, 5: an implemented feature with cost
// (or nothing to stamp) and delivery waits on a human, with no command for the
// agent; the group sits after CANDIDATES and before the debts.
func TestNextAwaitingAccept(t *testing.T) {
	projDir, _ := nextAfterCompleteProject(t)
	out, err := execCerne(t, "next", "--dir", projDir)
	if err != nil {
		t.Fatalf("next: %v\n%s", err, out)
	}
	blk := nextBlock(out, "WAITING FOR HUMAN ACCEPT (1)")
	if blk == "" {
		t.Fatalf("missing WAITING FOR HUMAN ACCEPT (1) group:\n%s", out)
	}
	if !strings.Contains(blk, "ask a human to run 'cerne feature accept <id>'") || !strings.Contains(blk, "F-0012") {
		t.Errorf("group = %s", blk)
	}
	if strings.Contains(blk, "F-0010") || strings.Contains(blk, "F-0013") || strings.Contains(blk, "\n    cerne ") {
		t.Errorf("group must hold only the complete feature, with no agent command:\n%s", blk)
	}
	if i := strings.Index(out, "WAITING FOR HUMAN ACCEPT"); i < strings.Index(out, "FINISH DELIVERY") {
		t.Errorf("WAITING FOR HUMAN ACCEPT must follow FINISH DELIVERY:\n%s", out)
	}

	js, err := execCerne(t, "next", "--dir", projDir, "--json")
	if err != nil {
		t.Fatalf("next --json: %v", err)
	}
	var q nextJSON
	if err := json.Unmarshal([]byte(js), &q); err != nil {
		t.Fatalf("json: %v", err)
	}
	for _, g := range q.Groups {
		if g.Queue == "awaiting_accept" {
			if len(g.Features) != 1 || g.Features[0].ID != "F-0012" || g.State != domain.StateImplemented {
				t.Errorf("awaiting_accept group = %+v", g)
			}
			return
		}
	}
	t.Errorf("no awaiting_accept group in json:\n%s", js)
}

func backendHead(t *testing.T, repoDir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", repoDir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

// TestNextByID pins F-0371 crit.1, 10-13: `cerne next <id>` prints the state and
// one step (text and --json), errors naming an unknown id, and never writes.
func TestNextByID(t *testing.T) {
	projDir, repoDir := nextAfterCompleteProject(t)
	cases := []struct{ id, want string }{
		{"F-0010", "F-0010 [implemented] — next: finish the delivery"},
		{"F-0012", "waiting on a human: no command for the agent"},
		{"F-0013", "F-0013 [done] — next: no next step"},
	}
	for _, tc := range cases {
		out, err := execCerne(t, "next", tc.id, "--dir", projDir)
		if err != nil || !strings.Contains(out, tc.want) {
			t.Errorf("next %s = %q, %v; want %q", tc.id, out, err, tc.want)
		}
	}
	out, _ := execCerne(t, "next", "F-0010", "--dir", projDir)
	if !strings.Contains(out, "run: cerne cost F-0010 --stamp") {
		t.Errorf("text must carry the run line:\n%s", out)
	}
	js, err := execCerne(t, "next", "F-0010", "--dir", projDir, "--json")
	if err != nil {
		t.Fatal(err)
	}
	var st map[string]string
	if err := json.Unmarshal([]byte(js), &st); err != nil || st["state"] != "implemented" || st["actor"] != "agent" || st["command"] != "cerne cost F-0010 --stamp" || st["step"] == "" {
		t.Errorf("json = %s (%v)", js, err)
	}
	head := backendHead(t, repoDir)
	for _, id := range []string{"F-0010", "F-0011", "F-0012", "F-0013", "F-0014"} {
		if _, err := execCerne(t, "next", id, "--dir", projDir); err != nil {
			t.Fatal(err)
		}
	}
	if backendHead(t, repoDir) != head {
		t.Error("next <id> moved the backend HEAD")
	}
	js, err = execCerne(t, "next", "F-0013", "--dir", projDir, "--json")
	if err != nil || json.Unmarshal([]byte(js), &st) != nil || st["actor"] != "none" || st["command"] != "" {
		t.Errorf("terminal json = %s (%v); want actor none", js, err)
	}
	if _, err := execCerne(t, "next", "F-9999", "--dir", projDir); err == nil || !strings.Contains(err.Error(), "F-9999") {
		t.Errorf("unknown id error = %v", err)
	}
}

// TestNextQueueAgreesWithNextByID (F-0371): each implemented feature sits in the
// group its own `next <id>` implies — an agent step in FINISH DELIVERY, a human
// one in WAITING FOR HUMAN ACCEPT, a terminal one in neither.
func TestNextQueueAgreesWithNextByID(t *testing.T) {
	projDir, _ := nextAfterCompleteProject(t)
	js, err := execCerne(t, "next", "--dir", projDir, "--json")
	if err != nil {
		t.Fatal(err)
	}
	var q nextJSON
	if err := json.Unmarshal([]byte(js), &q); err != nil {
		t.Fatal(err)
	}
	group := map[string]string{}
	for _, g := range q.Groups {
		if g.State == domain.StateImplemented {
			for _, f := range g.Features {
				group[f.ID] = g.Queue
			}
		}
	}
	want := map[string]string{"agent": "finish_delivery", "human": "awaiting_accept", "none": ""}
	for _, id := range []string{"F-0010", "F-0011", "F-0012", "F-0013", "F-0014"} {
		out, err := execCerne(t, "next", id, "--dir", projDir, "--json")
		if err != nil {
			t.Fatal(err)
		}
		var st map[string]string
		if err := json.Unmarshal([]byte(out), &st); err != nil {
			t.Fatal(err)
		}
		if group[id] != want[st["actor"]] {
			t.Errorf("%s: next <id> actor %q but the queue put it in %q", id, st["actor"], group[id])
		}
	}
}

// TestNextLedgerUnreadable (critic 5): a cost ledger that cannot be read shows in
// the queue and in `next <id>`; the feature is not sent to the human accept.
func TestNextLedgerUnreadable(t *testing.T) {
	projDir, repoDir := nextAfterCompleteProject(t)
	ledger := filepath.Join(filepath.Dir(repoDir), "costbind.ndjson")
	if err := os.Remove(ledger); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(ledger, 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := execCerne(t, "next", "F-0010", "--dir", projDir)
	if err != nil || !strings.Contains(out, "cost check failed") || strings.Contains(out, "waiting on a human") {
		t.Errorf("next F-0010 = %q, %v", out, err)
	}
}

// TestNextListsStaleDecision pins F-0385 criterion 11: a stale-decision
// finding is a debt in 'cerne next', like the other advisory rules.
func TestNextListsStaleDecision(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	old := time.Now().UTC().Add(-200 * 24 * time.Hour)
	if err := store.SaveFeature(repoDir, store.Feature{ID: "F-0001", Title: "Rejeitada", State: domain.StateRejected, CreatedAt: old, UpdatedAt: old}); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	if err := store.SaveDecision(repoDir, store.Decision{ID: "D-0001", Title: "Antiga", State: domain.DecisionActive, Facets: []string{"code"}, CreatedAt: old, UpdatedAt: old}); err != nil {
		t.Fatalf("SaveDecision: %v", err)
	}
	spec := filepath.Join(repoDir, "content", "features", "F-0001", "spec.md")
	if err := os.MkdirAll(filepath.Dir(spec), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(spec, []byte("---\ncerne_id: F-0001\n---\n\n# Entregue\n\nSegue a D-0001.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed stale decision"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}

	out, err := execCerne(t, "next", "--json", "--dir", projDir)
	if err != nil {
		t.Fatalf("next --json: %v\n%s", err, out)
	}
	var q struct {
		Debts []struct {
			Rule, Entity, Detail string
		} `json:"debts"`
	}
	if err := json.Unmarshal([]byte(out), &q); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	for _, d := range q.Debts {
		if d.Rule == "stale-decision" && d.Entity == "D-0001" && strings.Contains(d.Detail, "cerne decision keep D-0001") {
			return
		}
	}
	t.Fatalf("next debts lack stale-decision for D-0001: %+v", q.Debts)
}

// TestNextHonorsStaleDecisionDays (F-0385): a .cernerc that sets
// stale_decision_days = 30 flags a decision abandoned 31 days ago, which the
// 180-day default leaves alone.
func TestNextHonorsStaleDecisionDays(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	at := time.Now().UTC().Add(-31 * 24 * time.Hour)
	if err := store.SaveFeature(repoDir, store.Feature{ID: "F-0001", Title: "Rejeitada", State: domain.StateRejected, CreatedAt: at, UpdatedAt: at}); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	if err := store.SaveDecision(repoDir, store.Decision{ID: "D-0001", Title: "Recente", State: domain.DecisionActive, Facets: []string{"code"}, CreatedAt: at, UpdatedAt: at}); err != nil {
		t.Fatalf("SaveDecision: %v", err)
	}
	spec := filepath.Join(repoDir, "content", "features", "F-0001", "spec.md")
	if err := os.MkdirAll(filepath.Dir(spec), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(spec, []byte("---\ncerne_id: F-0001\n---\n\n# Rejeitada\n\nSegue a D-0001.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed abandoned decision"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}
	flagged := func() bool {
		t.Helper()
		out, err := execCerne(t, "next", "--json", "--dir", projDir)
		if err != nil {
			t.Fatalf("next --json: %v\n%s", err, out)
		}
		return strings.Contains(out, `"rule": "stale-decision"`)
	}
	if flagged() {
		t.Fatal("the 180-day default flagged a 31-day-old decision")
	}
	rc := filepath.Join(projDir, ".cernerc")
	data, err := os.ReadFile(rc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rc, append([]byte("stale_decision_days = 30\n"), data...), 0o644); err != nil {
		t.Fatal(err)
	}
	if !flagged() {
		t.Fatal("stale_decision_days = 30 did not flag a 31-day-old decision")
	}
	// The registry form wins over the top-level key (criterion 8).
	if err := project.SetSetting(projDir, "lint.stale_decision_days", project.ScopeProject, "400"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	if flagged() {
		t.Fatal("[lint] stale_decision_days = 400 did not win over the top-level 30")
	}
}
