package cli

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

func ideaCmd(t *testing.T, projDir string, args ...string) string {
	t.Helper()
	out, err := execCerne(t, append([]string{"idea"}, append(args, "--dir", projDir)...)...)
	if err != nil {
		t.Fatalf("idea %v: %v\noutput: %s", args, err, out)
	}
	return out
}

// Crit. 1: text only — in a multi-module project, with no --module, no work
// and no --context — prints the minted id.
func TestIdeaNewPrintsIDWithoutExtras(t *testing.T) {
	projDir, _, repoDir := setupProject(t, "core", "web")
	out := ideaCmd(t, projDir, "new", "talvez um dia X")
	if !strings.Contains(out, "I-0001") {
		t.Fatalf("output does not name the created id:\n%s", out)
	}
	i, err := store.LoadIdea(repoDir, "I-0001")
	if err != nil || i.State != domain.IdeaActive || i.Text != "talvez um dia X" || len(i.Facets)+len(i.Components) != 0 {
		t.Fatalf("idea = %+v, %v; want active, untagged", i, err)
	}
}

// Crit. 2: --facet/--component are optional and persisted; an unknown facet is
// refused with nothing written.
func TestIdeaNewPersistsOptionalTags(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	ideaCmd(t, projDir, "new", "com tags", "--facet", "code", "--component", "lint")
	i, _ := store.LoadIdea(repoDir, "I-0001")
	if !slices.Equal(i.Facets, []string{"code"}) || !slices.Equal(i.Components, []string{"lint"}) {
		t.Fatalf("tags = %v / %v, want [code] / [lint]", i.Facets, i.Components)
	}
	if out, err := execCerne(t, "idea", "new", "ruim", "--facet", "nope", "--dir", projDir); !errors.Is(err, domain.ErrUnknownFacet) {
		t.Fatalf("--facet nope err = %v, want ErrUnknownFacet\n%s", err, out)
	}
	if all, _ := store.LoadAllIdeas(repoDir); len(all) != 1 {
		t.Fatalf("a refused capture wrote: %d ideas", len(all))
	}
}

// Crit. 4: active only, newest first; --all shows the state and the reason;
// --search filters; --json is the agents' contract; empty says so.
func TestIdeaListActiveNewestFirstAllSearchJSON(t *testing.T) {
	projDir, _, _ := setupProject(t)
	if out := ideaCmd(t, projDir, "list"); strings.TrimSpace(out) != "No ideas." {
		t.Fatalf("empty list = %q, want No ideas.", out)
	}
	ideaCmd(t, projDir, "new", "primeira ideia")
	ideaCmd(t, projDir, "new", "conciliação do OFX")
	ideaCmd(t, projDir, "new", "terceira ideia")
	ideaCmd(t, projDir, "drop", "I-0002", "--reason", "duplicada")
	today := time.Now().UTC().Format(time.DateOnly)

	out := ideaCmd(t, projDir, "list")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 || lines[0] != "I-0003  "+today+"  active  terceira ideia" || !strings.HasPrefix(lines[1], "I-0001  ") {
		t.Fatalf("default list = %q, want I-0003 then I-0001, active only", lines)
	}
	if out := ideaCmd(t, projDir, "list", "--all"); !strings.Contains(out, "I-0002  "+today+"  dropped  conciliação do OFX  — duplicada") {
		t.Fatalf("--all lacks the dropped idea with its reason:\n%s", out)
	}
	if out := ideaCmd(t, projDir, "list", "--all", "--search", "CONCILIACAO"); !strings.Contains(out, "I-0002") || strings.Contains(out, "I-0001") {
		t.Fatalf("--search CONCILIACAO = %q, want only I-0002", out)
	}
	var doc struct {
		Ideas []store.Idea `json:"ideas"`
	}
	out = ideaCmd(t, projDir, "list", "--json")
	if err := json.Unmarshal([]byte(out), &doc); err != nil || len(doc.Ideas) != 2 {
		t.Fatalf("--json = %+v, %v\n%s", doc, err, out)
	}
	if i := doc.Ideas[0]; i.ID != "I-0003" || i.Text != "terceira ideia" || i.State != domain.IdeaActive || i.CreatedAt.IsZero() {
		t.Fatalf("--json first = %+v, want I-0003 active with created_at", i)
	}
}

// Crit. 5: cerne show resolves I- (and idea show delegates to it).
func TestShowResolvesIdeaPrefix(t *testing.T) {
	projDir, _, _ := setupProject(t)
	ideaCmd(t, projDir, "new", "exportar o kanban", "--facet", "code", "--component", "kanban")
	today := time.Now().UTC().Format(time.DateOnly)
	for _, args := range [][]string{{"show", "I-0001"}, {"idea", "show", "I-0001"}} {
		out, err := execCerne(t, append(args, "--dir", projDir)...)
		if err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
		for _, want := range []string{"I-0001  idea  [active]", "created: " + today, "facets: code", "components: kanban", "exportar o kanban"} {
			if !strings.Contains(out, want) {
				t.Fatalf("%v lacks %q:\n%s", args, want, out)
			}
		}
	}
	out, err := execCerne(t, "show", "I-0001", "--json", "--dir", projDir)
	var doc struct {
		Kind   string     `json:"kind"`
		Entity store.Idea `json:"entity"`
	}
	if err != nil || json.Unmarshal([]byte(out), &doc) != nil || doc.Kind != "idea" || doc.Entity.Text != "exportar o kanban" {
		t.Fatalf("show --json = %+v, %v\n%s", doc, err, out)
	}
	if out, err := execCerne(t, "idea", "show", "F-0001", "--dir", projDir); err == nil {
		t.Fatalf("idea show F-0001 must refuse a non-idea id:\n%s", out)
	}
}

// Crit. 8: drop with or without a reason; the record stays, with the reason.
func TestIdeaDropWithAndWithoutReason(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	ideaCmd(t, projDir, "new", "uma")
	ideaCmd(t, projDir, "new", "outra")
	if out := ideaCmd(t, projDir, "drop", "I-0001"); !strings.Contains(out, "I-0001: active -> dropped") {
		t.Fatalf("drop output = %q", out)
	}
	ideaCmd(t, projDir, "drop", "I-0002", "--reason", "não vale")
	if out := ideaCmd(t, projDir, "list"); strings.TrimSpace(out) != "No ideas." {
		t.Fatalf("dropped ideas still listed as active:\n%s", out)
	}
	if out := ideaCmd(t, projDir, "list", "--all"); !strings.Contains(out, "dropped  outra  — não vale") || !strings.Contains(out, "dropped  uma") {
		t.Fatalf("--all lacks the dropped records:\n%s", out)
	}
	out, err := execCerne(t, "show", "I-0002", "--dir", projDir)
	if err != nil || !strings.Contains(out, "reason: não vale") || !strings.Contains(out, "closed: ") {
		t.Fatalf("show I-0002 = %v\n%s", err, out)
	}
	if all, _ := store.LoadAllIdeas(repoDir); len(all) != 2 {
		t.Fatalf("drop deleted records: %d left", len(all))
	}
}

// Crit. 5 and 6: promote prints both ids; the pair is traceable both ways and
// the back-link is not a broken wiki-link.
func TestIdeaPromotePrintsBothIDs(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	ideaCmd(t, projDir, "new", "Exportar o kanban em CSV", "--facet", "code")
	out := ideaCmd(t, projDir, "promote", "I-0001")
	features, _ := store.LoadAllFeatures(repoDir)
	if len(features) != 1 || !strings.Contains(out, "I-0001: active -> promoted ("+features[0].ID+")") {
		t.Fatalf("promote output = %q, features = %+v", out, features)
	}
	fid := features[0].ID
	if out := ideaCmd(t, projDir, "list"); strings.TrimSpace(out) != "No ideas." {
		t.Fatalf("a promoted idea is still listed as active:\n%s", out)
	}
	if out := ideaCmd(t, projDir, "list", "--all"); !strings.Contains(out, "promoted  Exportar o kanban em CSV  → "+fid) {
		t.Fatalf("--all lacks the promoted idea and its feature:\n%s", out)
	}
	if out, err := execCerne(t, "show", "I-0001", "--dir", projDir); err != nil || !strings.Contains(out, "promoted to: "+fid) {
		t.Fatalf("show I-0001 = %v\n%s", err, out)
	}
	if out, err := execCerne(t, "show", fid, "--doc", "spec", "--dir", projDir); err != nil || !strings.Contains(out, "[[I-0001]]") {
		t.Fatalf("show %s --doc spec lacks the back-link: %v\n%s", fid, err, out)
	}
	out, _ = execCerne(t, "lint", "--dir", projDir)
	if strings.Contains(out, "I-0001") {
		t.Fatalf("lint reports the back-link:\n%s", out)
	}
}

// The promote goes through the feature core: a multi-module project needs
// --module, and a refusal leaves the idea active with no feature created.
func TestIdeaPromoteNeedsModuleInMultiModuleProject(t *testing.T) {
	projDir, _, repoDir := setupProject(t, "core", "web")
	ideaCmd(t, projDir, "new", "Atalho de teclado")
	if out, err := execCerne(t, "idea", "promote", "I-0001", "--dir", projDir); err == nil || !strings.Contains(err.Error(), "--module") {
		t.Fatalf("promote without --module err = %v, want the module requirement\n%s", err, out)
	}
	if i, _ := store.LoadIdea(repoDir, "I-0001"); i.State != domain.IdeaActive {
		t.Fatalf("a refused promote moved the idea to %s", i.State)
	}
	if fs, _ := store.LoadAllFeatures(repoDir); len(fs) != 0 {
		t.Fatalf("a refused promote created %d feature(s)", len(fs))
	}
	if out := ideaCmd(t, projDir, "promote", "I-0001", "--module", "core"); !strings.Contains(out, "promoted") {
		t.Fatalf("promote --module core = %q", out)
	}
	if fs, _ := store.LoadAllFeatures(repoDir); len(fs) != 1 || fs[0].Module != "core" {
		t.Fatalf("features = %+v, want one in core", fs)
	}
}

// C6/C16: every idea verb refuses a non-idea id with "not an idea id" — a
// traversal never reaches the disk — and accepts a lowercase i-.
func TestIdeaVerbsRefuseNonIdeaIDs(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	if out, err := execCerne(t, "work", "start", "x", "--dir", projDir); err != nil {
		t.Fatalf("work start: %v\n%s", err, out)
	}
	for _, args := range [][]string{
		{"idea", "drop", "../works/W-0001", "--reason", "t"},
		{"idea", "promote", "F-0001"},
		{"idea", "drop", "F-0001"},
		{"idea", "show", "F-0001"},
	} {
		out, err := execCerne(t, append(args, "--dir", projDir)...)
		if err == nil || !strings.Contains(err.Error(), "not an idea id") {
			t.Fatalf("%v: err = %v, want 'not an idea id'\n%s", args, err, out)
		}
	}
	if all, _ := store.LoadAllIdeas(repoDir); len(all) != 0 {
		t.Fatalf("a refused id planted %d idea(s): %+v", len(all), all)
	}
	ideaCmd(t, projDir, "new", "minúscula")
	if out := ideaCmd(t, projDir, "show", "i-0001"); !strings.Contains(out, "I-0001  idea  [active]") {
		t.Fatalf("idea show i-0001 = %q", out)
	}
}

// C16: cerne show takes a lowercase i- id, and the --doc refusal reads "an idea".
func TestShowIdeaLowercaseAndArticle(t *testing.T) {
	projDir, _, _ := setupProject(t)
	ideaCmd(t, projDir, "new", "minúscula")
	out, err := execCerne(t, "show", "i-0001", "--dir", projDir)
	if err != nil || !strings.Contains(out, "I-0001  idea  [active]") {
		t.Fatalf("show i-0001 = %v\n%s", err, out)
	}
	_, err = execCerne(t, "show", "I-0001", "--doc", "spec", "--dir", projDir)
	if err == nil || !strings.Contains(err.Error(), "this is an idea") {
		t.Fatalf("show --doc on an idea err = %v, want 'this is an idea'", err)
	}
}

// C3: the feature→idea link is data on the feature (from_idea), shown by
// cerne show — rewriting the spec, which erases the stub's prose line, keeps it.
func TestPromotedFeatureKeepsFromIdeaAfterSpecRewrite(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	ideaCmd(t, projDir, "new", "Exportar o kanban em CSV")
	ideaCmd(t, projDir, "promote", "I-0001")
	fs, _ := store.LoadAllFeatures(repoDir)
	if len(fs) != 1 || fs[0].FromIdea != "I-0001" {
		t.Fatalf("features = %+v, want one with from_idea I-0001", fs)
	}
	fid := fs[0].ID
	if err := app.SaveFeatureDoc(repoDir, projDir, fid, "spec", "---\ncerne_id: "+fid+"\n---\n\n## Problema\n\nReescrita, sem a linha de origem.\n"); err != nil {
		t.Fatalf("SaveFeatureDoc: %v", err)
	}
	out, err := execCerne(t, "show", fid, "--dir", projDir)
	if err != nil || !strings.Contains(out, "from idea: I-0001") {
		t.Fatalf("show %s after a spec rewrite = %v\n%s", fid, err, out)
	}
	var doc struct {
		Entity store.Feature `json:"entity"`
	}
	out, err = execCerne(t, "show", fid, "--json", "--dir", projDir)
	if err != nil || json.Unmarshal([]byte(out), &doc) != nil || doc.Entity.FromIdea != "I-0001" {
		t.Fatalf("show --json = %+v, %v\n%s", doc, err, out)
	}
}
