package cli

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

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
