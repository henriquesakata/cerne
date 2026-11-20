package lint

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// E-0005 verification 10, the note part (criterion 14): the active notes with
// neither a trigger nor a link are ONE advisory line with the count and the
// two commands that reach them; a note with either, or a superseded one, is
// not counted, and none of them is no finding at all.
func TestNoteReachFindings(t *testing.T) {
	notes := []store.Note{
		{ID: "N-0001", State: domain.NoteActive, When: []string{"edit:internal/x.go"}},
		{ID: "N-0002", State: domain.NoteActive, Informs: []string{"F-0001"}},
		{ID: "N-0003", State: domain.NoteActive},
		{ID: "N-0004", State: domain.NoteActive},
		{ID: "N-0005", State: domain.NoteSuperseded},
	}
	got := noteReachFindings(notes)
	if len(got) != 1 || got[0].Rule != RuleNoteUnreachable || !Advisory(got[0].Rule) {
		t.Fatalf("findings = %+v, want one advisory %s", got, RuleNoteUnreachable)
	}
	for _, want := range []string{"2 active notes have no trigger and no link", "N-0003, N-0004", "cerne note when", "cerne note link"} {
		if !strings.Contains(got[0].Detail, want) {
			t.Errorf("detail %q lacks %q", got[0].Detail, want)
		}
	}
	if got := noteReachFindings(notes[:2]); len(got) != 0 {
		t.Fatalf("every note reachable: %+v", got)
	}
}
