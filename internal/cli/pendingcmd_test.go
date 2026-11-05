package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
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

// newOriginFeature registers a feature through the CLI to hang pendings from.
func newOriginFeature(t *testing.T, projDir, title string, extra ...string) {
	t.Helper()
	args := append([]string{"feature", "new", title, "--context", "ctx", "--dir", projDir}, extra...)
	if out, err := execCerne(t, args...); err != nil {
		t.Fatalf("feature new: %v\noutput: %s", err, out)
	}
}

func pendingNew(t *testing.T, projDir string, args ...string) string {
	t.Helper()
	out, err := execCerne(t, append([]string{"pending", "new"}, append(args, "--dir", projDir)...)...)
	if err != nil {
		t.Fatalf("pending new %v: %v\noutput: %s", args, err, out)
	}
	return out
}

// Crit. 1: new prints the created id; the entity is open with its origin.
func TestPendingNewPrintsID(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	newOriginFeature(t, projDir, "Feature de origem")
	out := pendingNew(t, projDir, "Tratar o caso Y", "--from", "F-0001")
	if !strings.Contains(out, "P-0001") {
		t.Fatalf("output does not name the created id:\n%s", out)
	}
	p, err := store.LoadPending(repoDir, "P-0001")
	if err != nil || p.State != domain.PendingOpen || p.From != "F-0001" {
		t.Fatalf("pending = %+v, %v; want open from F-0001", p, err)
	}
}

// Crit. 2: no --from or an unknown origin refuses naming the field / id.
func TestPendingNewRefusals(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	for _, tc := range []struct {
		args []string
		want string
		is   error
	}{
		{[]string{"pending", "new", "Tratar o caso Y"}, "--from", app.ErrPendingFromRequired},
		{[]string{"pending", "new", "Tratar o caso Y", "--from", "F-0042"}, "F-0042", store.ErrNotFound},
	} {
		out, err := execCerne(t, append(tc.args, "--dir", projDir)...)
		if !errors.Is(err, tc.is) || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%v: err = %v, want %v naming %q\noutput: %s", tc.args, err, tc.is, tc.want, out)
		}
	}
	if ps, _ := store.LoadAllPendings(repoDir); len(ps) != 0 {
		t.Fatalf("refusals persisted %d pending(s)", len(ps))
	}
}

// Crit. 4: the default list is open-only, one line per pending with id, age in
// days, origin and title; --state/--from/--component/--module filter; --json is
// the machine contract.
func TestPendingListDefaultOpenAndFilters(t *testing.T) {
	projDir, _, repoDir := setupProject(t, "core", "web")
	newOriginFeature(t, projDir, "Origem core", "--module", "core")
	newOriginFeature(t, projDir, "Origem web", "--module", "web")
	pendingNew(t, projDir, "Primeiro débito", "--from", "F-0001", "--component", "lint")
	pendingNew(t, projDir, "Segundo débito", "--from", "F-0002")
	pendingNew(t, projDir, "Terceiro débito", "--from", "F-0002")
	if out, err := execCerne(t, "pending", "resolve", "P-0003", "--dir", projDir); err != nil {
		t.Fatalf("resolve: %v\n%s", err, out)
	}
	// Age the first one synthetically: the list reports whole days.
	p, _ := store.LoadPending(repoDir, "P-0001")
	p.CreatedAt = time.Now().UTC().Add(-50 * time.Hour)
	if err := store.SavePending(repoDir, p); err != nil {
		t.Fatal(err)
	}
	if _, err := gitback.CommitAndPush(repoDir, "age P-0001"); err != nil {
		t.Fatal(err)
	}

	list := func(args ...string) string {
		t.Helper()
		out, err := execCerne(t, append([]string{"pending", "list", "--dir", projDir}, args...)...)
		if err != nil {
			t.Fatalf("pending list %v: %v\n%s", args, err, out)
		}
		return out
	}
	out := list()
	if !strings.Contains(out, "P-0001  2d  F-0001  Primeiro débito") || !strings.Contains(out, "P-0002  0d  F-0002  Segundo débito") {
		t.Fatalf("default list lines wrong:\n%s", out)
	}
	if strings.Contains(out, "P-0003") {
		t.Fatalf("default list shows a resolved pending:\n%s", out)
	}
	for _, tc := range []struct {
		args       []string
		want, deny string
	}{
		{[]string{"--state", "resolved"}, "P-0003", "P-0001"},
		{[]string{"--from", "F-0002"}, "P-0002", "P-0001"},
		{[]string{"--component", "Lint"}, "P-0001", "P-0002"},
		{[]string{"--module", "web"}, "P-0002", "P-0001"},
	} {
		got := list(tc.args...)
		if !strings.Contains(got, tc.want) || strings.Contains(got, tc.deny) {
			t.Fatalf("list %v: want %s, not %s:\n%s", tc.args, tc.want, tc.deny, got)
		}
	}
	if out, err := execCerne(t, "pending", "list", "--state", "bogus", "--dir", projDir); err == nil {
		t.Fatalf("an unknown --state must refuse:\n%s", out)
	}

	var doc struct {
		Pendings []store.Pending `json:"pendings"`
	}
	if err := json.Unmarshal([]byte(list("--json")), &doc); err != nil {
		t.Fatalf("--json: %v", err)
	}
	if len(doc.Pendings) != 2 || doc.Pendings[0].ID != "P-0001" || doc.Pendings[0].From != "F-0001" ||
		doc.Pendings[0].State != domain.PendingOpen || doc.Pendings[0].CreatedAt.IsZero() {
		t.Fatalf("--json pendings = %+v, want the 2 open with id/from/state/created_at", doc.Pendings)
	}
}

// Crit. 8: a resolved pending leaves the default list.
func TestPendingResolveLeavesDefaultList(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	newOriginFeature(t, projDir, "Origem")
	pendingNew(t, projDir, "Tratar o caso Y", "--from", "F-0001")
	out, err := execCerne(t, "pending", "resolve", "P-0001", "--dir", projDir)
	if err != nil || !strings.Contains(out, "P-0001: open -> resolved") {
		t.Fatalf("resolve: %v\n%s", err, out)
	}
	if p, _ := store.LoadPending(repoDir, "P-0001"); p.State != domain.PendingResolved || p.ClosedAt == nil {
		t.Fatalf("pending = %+v, want resolved with closed_at", p)
	}
	out, _ = execCerne(t, "pending", "list", "--dir", projDir)
	if strings.Contains(out, "P-0001") {
		t.Fatalf("resolved pending still in the default list:\n%s", out)
	}
}

// Crit. 9: drop refuses without --reason and persists it with one.
func TestPendingDropNeedsReason(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	newOriginFeature(t, projDir, "Origem")
	pendingNew(t, projDir, "Tratar o caso Y", "--from", "F-0001")
	if _, err := execCerne(t, "pending", "drop", "P-0001", "--dir", projDir); !errors.Is(err, app.ErrPendingReasonRequired) {
		t.Fatalf("drop without --reason: err = %v, want ErrPendingReasonRequired", err)
	}
	out, err := execCerne(t, "pending", "drop", "P-0001", "--reason", "fora de escopo", "--dir", projDir)
	if err != nil || !strings.Contains(out, "P-0001: open -> dropped") {
		t.Fatalf("drop: %v\n%s", err, out)
	}
	if p, _ := store.LoadPending(repoDir, "P-0001"); p.State != domain.PendingDropped || p.Reason != "fora de escopo" {
		t.Fatalf("pending = %+v, want dropped with the reason", p)
	}
}

// Crit. 5: show resolves a P- id, in text and --json.
func TestShowPending(t *testing.T) {
	projDir, _, _ := setupProject(t)
	newOriginFeature(t, projDir, "Origem", "--component", "lint")
	out, err := execCerneStdin(t, "o caso Y ficou de fora\n", "pending", "new", "Tratar o caso Y", "--from", "F-0001", "--dir", projDir)
	if err != nil {
		t.Fatalf("pending new: %v\n%s", err, out)
	}
	out, err = execCerne(t, "show", "P-0001", "--dir", projDir)
	if err != nil {
		t.Fatalf("show P-0001: %v\n%s", err, out)
	}
	for _, want := range []string{"P-0001  pending  [open]  Tratar o caso Y", "from: F-0001", "components: lint", "o caso Y ficou de fora"} {
		if !strings.Contains(out, want) {
			t.Fatalf("show P-0001 lacks %q:\n%s", want, out)
		}
	}
	out, err = execCerne(t, "show", "P-0001", "--json", "--dir", projDir)
	if err != nil {
		t.Fatalf("show --json: %v\n%s", err, out)
	}
	var doc struct {
		Kind   string        `json:"kind"`
		Entity store.Pending `json:"entity"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil || doc.Kind != "pending" || doc.Entity.From != "F-0001" {
		t.Fatalf("show --json = %+v, %v; want kind pending from F-0001\n%s", doc, err, out)
	}
}

// Crit. 6: show of the origin lists its OPEN pendings (id + title), text and json.
func TestShowFeatureListsOpenPendings(t *testing.T) {
	projDir, _, _ := setupProject(t)
	newOriginFeature(t, projDir, "Origem")
	pendingNew(t, projDir, "Débito aberto", "--from", "F-0001")
	pendingNew(t, projDir, "Débito pago", "--from", "F-0001")
	if out, err := execCerne(t, "pending", "resolve", "P-0002", "--dir", projDir); err != nil {
		t.Fatalf("resolve: %v\n%s", err, out)
	}
	out, err := execCerne(t, "show", "F-0001", "--dir", projDir)
	if err != nil {
		t.Fatalf("show F-0001: %v\n%s", err, out)
	}
	if !strings.Contains(out, "## pending\nP-0001  Débito aberto") || strings.Contains(out, "P-0002") {
		t.Fatalf("show F-0001 must list only the open P-0001:\n%s", out)
	}
	out, err = execCerne(t, "show", "F-0001", "--json", "--dir", projDir)
	if err != nil || !strings.Contains(out, `"name": "pending"`) || !strings.Contains(out, "P-0001  Débito aberto") {
		t.Fatalf("show --json lacks the pending backlink: %v\n%s", err, out)
	}
}

// Crit. 7 and 8: open pendings get their own block in next, identified by id
// and origin, oldest first and capped; a resolved one leaves it; no block at
// all without open pendings.
func TestNextShowsOpenPendings(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	newOriginFeature(t, projDir, "Origem")
	next := func() string {
		t.Helper()
		out, err := execCerne(t, "next", "--dir", projDir)
		if err != nil {
			t.Fatalf("next: %v\n%s", err, out)
		}
		return out
	}
	if out := next(); strings.Contains(out, "OPEN PENDINGS") {
		t.Fatalf("no open pending, yet next prints the block:\n%s", out)
	}
	pendingNew(t, projDir, "Débito novo", "--from", "F-0001")
	out := next()
	if !strings.Contains(out, "OPEN PENDINGS (1)") || !strings.Contains(out, "\n  P-0001  0d  F-0001  Débito novo") {
		t.Fatalf("next lacks the open pending block:\n%s", out)
	}
	if out, err := execCerne(t, "pending", "resolve", "P-0001", "--dir", projDir); err != nil {
		t.Fatalf("resolve: %v\n%s", err, out)
	}
	if out := next(); strings.Contains(out, "OPEN PENDINGS") || strings.Contains(out, "P-0001") {
		t.Fatalf("a resolved pending must leave next:\n%s", out)
	}

	// Oldest first, capped at nextPendingCap with the remainder counted.
	n := nextPendingCap + 2
	base := time.Now().UTC().Add(-time.Hour)
	for i := 2; i <= n+1; i++ {
		p := store.Pending{ID: fmt.Sprintf("P-%04d", i), Title: fmt.Sprintf("Débito %d", i), From: "F-0001",
			State: domain.PendingOpen, CreatedAt: base.Add(-time.Duration(i) * 24 * time.Hour)}
		if err := store.SavePending(repoDir, p); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed pendings"); err != nil {
		t.Fatal(err)
	}
	out = next()
	if !strings.Contains(out, fmt.Sprintf("OPEN PENDINGS (%d)", n)) || !strings.Contains(out, "… and 2 more ('cerne pending list')") {
		t.Fatalf("next must count all and cap the listing:\n%s", out)
	}
	oldest, newer := fmt.Sprintf("P-%04d", n+1), fmt.Sprintf("P-%04d", 4)
	if i, j := strings.Index(out, oldest), strings.Index(out, newer); i < 0 || j < 0 || i > j {
		t.Fatalf("next must list the oldest (%s) before the newer %s:\n%s", oldest, newer, out)
	}
	if strings.Contains(out, "P-0002 ") || strings.Contains(out, "P-0003 ") {
		t.Fatalf("the two newest pendings must fall beyond the cap:\n%s", out)
	}
}

// Crit. 11 ("severidade warning"): stale-pending alone keeps 'cerne lint' at
// exit 0 and labels the section as a warning; with a blocking finding too, the
// lint still fails.
func TestLintAdvisoryKeepsExitCode(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	newOriginFeature(t, projDir, "Origem", "--facet", "code")
	pendingNew(t, projDir, "Débito velho", "--from", "F-0001")
	p, _ := store.LoadPending(repoDir, "P-0001")
	p.CreatedAt = time.Now().UTC().Add(-lint.StalePendingAfter - time.Hour)
	if err := store.SavePending(repoDir, p); err != nil {
		t.Fatal(err)
	}
	if _, err := gitback.CommitAndPush(repoDir, "age P-0001"); err != nil {
		t.Fatal(err)
	}
	out, err := execCerne(t, "lint", "--dir", projDir)
	if err != nil {
		t.Fatalf("lint with only an advisory finding must exit 0: %v\n%s", err, out)
	}
	if !strings.Contains(out, "STALE-PENDING (1) (warning)") || !strings.Contains(out, "P-0001 open for") ||
		!strings.Contains(out, "Total: 1 problem(s), all advisory — lint passes.") {
		t.Fatalf("lint output lacks the warning section or the advisory total:\n%s", out)
	}
	if out, err := execCerne(t, "lint", "--feature", "F-0001", "--dir", projDir); err != nil || !strings.Contains(out, "P-0001") {
		t.Fatalf("lint --feature on the origin must show the finding and exit 0: %v\n%s", err, out)
	}

	old := time.Now().UTC().Add(-48 * time.Hour)
	w := store.Work{ID: "W-0001", Title: "Esquecida", State: domain.WorkActive, Actor: "a@b", CreatedAt: old, UpdatedAt: old}
	if err := store.SaveWork(repoDir, w); err != nil {
		t.Fatal(err)
	}
	if _, err := gitback.CommitAndPush(repoDir, "stale work"); err != nil {
		t.Fatal(err)
	}
	out, err = execCerne(t, "lint", "--dir", projDir)
	if err == nil || err.Error() != "1 problem(s) found" || !strings.Contains(out, "Total: 2 problem(s), 1 advisory.") {
		t.Fatalf("advisory + blocking: err = %v, want '1 problem(s) found' and a total naming the advisory one:\n%s", err, out)
	}
}

// ageInBackend commits a pending older than StalePendingAfter and, when
// staleWork, a forgotten active work (a blocking stale-work finding).
func ageInBackend(t *testing.T, repoDir, id string, staleWork bool) {
	t.Helper()
	p, _ := store.LoadPending(repoDir, id)
	p.CreatedAt = time.Now().UTC().Add(-lint.StalePendingAfter - 26*time.Hour)
	if err := store.SavePending(repoDir, p); err != nil {
		t.Fatal(err)
	}
	if staleWork {
		old := time.Now().UTC().Add(-48 * time.Hour)
		w := store.Work{ID: "W-0001", Title: "Esquecida", State: domain.WorkActive, Actor: "a@b", CreatedAt: old, UpdatedAt: old}
		if err := store.SaveWork(repoDir, w); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := gitback.CommitAndPush(repoDir, "age "+id); err != nil {
		t.Fatal(err)
	}
}

// A stale pending shows once in next — in OPEN PENDINGS, with its age — not
// again under DEBTS; and the pendings block comes before the lint debts.
func TestNextShowsStalePendingOnceBeforeDebts(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	newOriginFeature(t, projDir, "Origem", "--facet", "code")
	pendingNew(t, projDir, "Débito velho", "--from", "F-0001")
	ageInBackend(t, repoDir, "P-0001", true)
	out, err := execCerne(t, "next", "--dir", projDir)
	if err != nil {
		t.Fatalf("next: %v\n%s", err, out)
	}
	p, d := strings.Index(out, "OPEN PENDINGS"), strings.Index(out, "DEBTS")
	if p < 0 || d < 0 || p > d || !strings.Contains(out, "STALE-WORK") {
		t.Fatalf("want OPEN PENDINGS before a DEBTS block holding STALE-WORK:\n%s", out)
	}
	if strings.Contains(out, "STALE-PENDING") || strings.Count(out, "P-0001") != 1 {
		t.Fatalf("the stale pending must show once, in OPEN PENDINGS only:\n%s", out)
	}
}

// Crit. 10 through the CLI: promote --as feature prints the transition and the
// created id; a missing --as refuses.
func TestPendingPromoteCmd(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	newOriginFeature(t, projDir, "Origem")
	pendingNew(t, projDir, "Configurar o limiar", "--from", "F-0001")
	if _, err := execCerne(t, "pending", "promote", "P-0001", "--dir", projDir); !errors.Is(err, app.ErrPendingPromoteKind) {
		t.Fatalf("promote without --as: err = %v, want ErrPendingPromoteKind", err)
	}
	out, err := execCerne(t, "pending", "promote", "P-0001", "--as", "feature", "--dir", projDir)
	if err != nil || !strings.Contains(out, "P-0001: open -> promoted (F-0002)") {
		t.Fatalf("promote: %v\n%s", err, out)
	}
	if p, _ := store.LoadPending(repoDir, "P-0001"); p.PromotedTo != "F-0002" {
		t.Fatalf("pending = %+v, want promoted_to F-0002", p)
	}
	out, err = execCerne(t, "show", "P-0001", "--dir", projDir)
	if err != nil || !strings.Contains(out, "promoted to: F-0002") {
		t.Fatalf("show P-0001 lacks the forward link: %v\n%s", err, out)
	}
}

// The spec's CLI surface lists 'pending show' (with --json): it is the generic
// show, scoped to P- ids.
func TestPendingShowAliasesShow(t *testing.T) {
	projDir, _, _ := setupProject(t)
	newOriginFeature(t, projDir, "Origem")
	pendingNew(t, projDir, "Tratar o caso Y", "--from", "F-0001")
	out, err := execCerne(t, "pending", "show", "P-0001", "--dir", projDir)
	if err != nil || !strings.Contains(out, "P-0001  pending  [open]  Tratar o caso Y") {
		t.Fatalf("pending show: %v\n%s", err, out)
	}
	if out, err := execCerne(t, "pending", "show", "P-0001", "--json", "--dir", projDir); err != nil || !strings.Contains(out, `"kind": "pending"`) {
		t.Fatalf("pending show --json: %v\n%s", err, out)
	}
	if _, err := execCerne(t, "pending", "show", "F-0001", "--dir", projDir); err == nil {
		t.Fatal("pending show must refuse a non-P- id")
	}
}

// --module on promote overrides the module inherited from the origin.
func TestPendingPromoteModuleFlag(t *testing.T) {
	projDir, _, repoDir := setupProject(t, "core", "web")
	newOriginFeature(t, projDir, "Origem", "--module", "core")
	pendingNew(t, projDir, "Tratar o caso Y", "--from", "F-0001")
	if out, err := execCerne(t, "pending", "promote", "P-0001", "--as", "feature", "--module", "web", "--dir", projDir); err != nil {
		t.Fatalf("promote --module: %v\n%s", err, out)
	}
	if f, _ := store.LoadFeature(repoDir, "F-0002"); f.Module != "web" {
		t.Fatalf("F-0002 module = %q, want web", f.Module)
	}
}

// push --dry-run is a pre-push gate with the lint's exit policy: only advisory
// findings pass, advisory plus blocking fails.
func TestPushDryRunAdvisoryPolicy(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	newOriginFeature(t, projDir, "Origem", "--facet", "code")
	pendingNew(t, projDir, "Débito velho", "--from", "F-0001")
	ageInBackend(t, repoDir, "P-0001", false)
	if out, err := execCerne(t, "pull", "--dir", projDir); err != nil {
		t.Fatalf("pull: %v\n%s", err, out)
	}
	specPath := filepath.Join(projDir, ".docs", "current", "features", "F-0001-origem", "spec.md")
	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, specPath, string(data)+"\nMais contexto.\n")

	out, err := execCerne(t, "push", "--dry-run", "--dir", projDir)
	if err != nil || !strings.Contains(out, "STALE-PENDING (1) (warning)") || !strings.Contains(out, "all advisory") {
		t.Fatalf("dry-run with only an advisory finding must pass: %v\n%s", err, out)
	}

	old := time.Now().UTC().Add(-48 * time.Hour)
	w := store.Work{ID: "W-0001", Title: "Esquecida", State: domain.WorkActive, Actor: "a@b", CreatedAt: old, UpdatedAt: old}
	if err := store.SaveWork(repoDir, w); err != nil {
		t.Fatal(err)
	}
	if _, err := gitback.CommitAndPush(repoDir, "stale work"); err != nil {
		t.Fatal(err)
	}
	if out, err := execCerne(t, "push", "--dry-run", "--dir", projDir); err == nil || err.Error() != "1 problem(s) found" {
		t.Fatalf("dry-run with advisory + blocking: err = %v, want '1 problem(s) found'\n%s", err, out)
	}
}
