package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// F-0419 crit. 5 and 7: idea new takes the title as the words and the body from
// --file or piped stdin; show prints both; a multiline title is refused with
// nothing written.
func TestIdeaNewTitleAndBody(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	body := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(body, []byte("o **corpo** da ideia\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ideaCmd(t, projDir, "new", "um título", "--file", body)
	i, _ := store.LoadIdea(repoDir, "I-0001")
	if i.Title != "um título" || i.Body != "o **corpo** da ideia" {
		t.Fatalf("idea = %+v, want title and body from --file", i)
	}
	if out, err := execCerneStdin(t, "corpo por stdin\n", "idea", "new", "outro título", "--dir", projDir); err != nil {
		t.Fatalf("idea new with stdin: %v\n%s", err, out)
	}
	if j, _ := store.LoadIdea(repoDir, "I-0002"); j.Title != "outro título" || j.Body != "corpo por stdin" {
		t.Fatalf("idea = %+v, want the body from stdin", j)
	}
	if out, err := execCerne(t, "idea", "new", "duas\nlinhas", "--dir", projDir); !errors.Is(err, app.ErrIdeaTitleMultiline) {
		t.Fatalf("multiline title err = %v, want ErrIdeaTitleMultiline\n%s", err, out)
	}
	if all, _ := store.LoadAllIdeas(repoDir); len(all) != 2 {
		t.Fatalf("a refused capture wrote: %d ideas", len(all))
	}
	for _, args := range [][]string{{"show", "I-0001"}, {"idea", "show", "I-0001"}} {
		out, err := execCerne(t, append(args, "--dir", projDir)...)
		if err != nil || !strings.Contains(out, "um título") || !strings.Contains(out, "o **corpo** da ideia") {
			t.Fatalf("%v = %q, %v; want title and body", args, out, err)
		}
	}
}

// F-0419 crit. 1: idea resolve closes an active idea with the optional reason;
// a closed one is refused with the drop's sentinel.
func TestIdeaResolveCommand(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	ideaCmd(t, projDir, "new", "já entregue")
	out := ideaCmd(t, projDir, "resolve", "I-0001", "--reason", "feito na F-0001")
	if !strings.Contains(out, "I-0001: active -> resolved") {
		t.Fatalf("resolve output = %q", out)
	}
	if i, _ := store.LoadIdea(repoDir, "I-0001"); i.State != domain.IdeaResolved || i.Reason != "feito na F-0001" {
		t.Fatalf("idea = %+v, want resolved with the reason", i)
	}
	if out, err := execCerne(t, "idea", "resolve", "I-0001", "--dir", projDir); !errors.Is(err, app.ErrIdeaNotActive) {
		t.Fatalf("second resolve err = %v, want ErrIdeaNotActive\n%s", err, out)
	}
	if out := ideaCmd(t, projDir, "list", "--state", "resolved"); !strings.Contains(out, "I-0001") || !strings.Contains(out, "resolved") {
		t.Fatalf("list --state resolved = %q", out)
	}
}

// F-0419 crit. 4: idea link points an active idea at a live feature, unlink
// takes it back; a feature that is not live is refused, and the accept of the
// feature resolves the idea and says so.
func TestIdeaLinkUnlinkCommands(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	newOriginFeature(t, projDir, "Entrega a ligar")
	ideaCmd(t, projDir, "new", "ideia a ligar")
	out := ideaCmd(t, projDir, "link", "I-0001", "F-0001")
	if !strings.Contains(out, "I-0001: active -> promoted (F-0001)") {
		t.Fatalf("link output = %q", out)
	}
	if i, _ := store.LoadIdea(repoDir, "I-0001"); i.State != domain.IdeaPromoted || i.PromotedTo != "F-0001" {
		t.Fatalf("idea = %+v, want promoted to F-0001", i)
	}
	out = ideaCmd(t, projDir, "unlink", "I-0001")
	if !strings.Contains(out, "I-0001: promoted -> active") {
		t.Fatalf("unlink output = %q", out)
	}
	if i, _ := store.LoadIdea(repoDir, "I-0001"); i.State != domain.IdeaActive || i.PromotedTo != "" {
		t.Fatalf("idea = %+v, want active and unlinked", i)
	}
	if out, err := execCerne(t, "idea", "link", "I-0001", "F-0999", "--dir", projDir); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("link to an unknown feature err = %v\n%s", err, out)
	}
}
