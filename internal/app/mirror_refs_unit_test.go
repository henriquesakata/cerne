package app

import (
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
	"github.com/henriquesakata/cerne/internal/tracker"
	"github.com/henriquesakata/cerne/internal/tracker/trackertest"
)

// F-0449 (audit finding 5): a forged issue id or URL becomes no idea at import
// and no trailer at stamping.
func TestMirrorForgedRefsNeverReachACommit(t *testing.T) {
	asHuman(t)
	repoDir, projDir, _ := newFeatureBackend(t)
	pub := &trackertest.Fake{Name: "pub"}
	mirrorProject(t, projDir, mirrorConf{}, pub)
	pub.Issues = map[string]*trackertest.Issue{"12\nCloses: #1": {Title: "Forged", Open: true}}
	rep, err := SyncMirror(repoDir, projDir)
	if err != nil {
		t.Fatalf("SyncMirror: %v", err)
	}
	if len(rep.Ideas) != 0 || ideaCount(t, repoDir) != 0 {
		t.Fatalf("a forged issue was imported: %+v", rep)
	}
	seedMirrored(t, repoDir, "F-0001", domain.StateInDevelopment, tracker.Ref{Tracker: "pub", ID: "12\nCloses: #1", URL: "https://fake.test/pub/issues/12\nCloses: #1"})
	seedMirrored(t, repoDir, "F-0002", domain.StateInDevelopment, tracker.Ref{Tracker: "pub", ID: "1", URL: "https://fake.test/pub/issues/1\nCo-authored-by: Mallory <m@evil.example>"})
	if got := MirrorTrailers(repoDir, projDir, []string{"F-0001", "F-0002"}); len(got) != 0 {
		t.Fatalf("MirrorTrailers = %q, want none for forged links", got)
	}
}

// F-0449 (audit finding 7): an outside issue's text is imported printable and
// bounded: no control character (the body keeps line breaks and tabs), the
// title at most 256 characters, the body at most 8 KiB.
func TestMirrorSyncCleansImportedText(t *testing.T) {
	asHuman(t)
	repoDir, projDir, _ := newFeatureBackend(t)
	pub := &trackertest.Fake{Name: "pub"}
	mirrorProject(t, projDir, mirrorConf{}, pub)
	pub.Open("Crash \x1b[31mred\x1b[0m on start", "line one\r\n\x1b[2Jline two\tend", "")
	pub.Open(strings.Repeat("é", 300), strings.Repeat("ã", 6000), "")
	rep, err := SyncMirror(repoDir, projDir)
	if err != nil || len(rep.Ideas) != 2 {
		t.Fatalf("SyncMirror = %+v, %v; want two ideas", rep, err)
	}
	esc, err := store.LoadIdea(repoDir, rep.Ideas[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsFunc(esc.Title+esc.Body, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' }) {
		t.Errorf("imported text keeps a control character: title %q, body %q", esc.Title, esc.Body)
	}
	if !strings.Contains(esc.Body, "line one\n") || !strings.Contains(esc.Body, "two\tend") {
		t.Errorf("body = %q, want its line break and tab kept", esc.Body)
	}
	big, err := store.LoadIdea(repoDir, rep.Ideas[1])
	if err != nil {
		t.Fatal(err)
	}
	if n := utf8.RuneCountInString(big.Title); n != 256 {
		t.Errorf("title is %d characters, want 256", n)
	}
	if len(big.Body) > 8<<10 || !utf8.ValidString(big.Body) {
		t.Errorf("body is %d bytes (valid UTF-8: %v), want at most 8 KiB", len(big.Body), utf8.ValidString(big.Body))
	}
}
