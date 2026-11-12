package app

import "testing"

// Round 2, D: a temporary path is recognised by where it resolves, not by how
// its text starts.
func TestTempEvidenceForms(t *testing.T) {
	t.Setenv("TMPDIR", "/custom/tmpdir/")
	t.Setenv("HOME", "/tmp/home")
	t.Chdir("/tmp")
	for _, item := range []string{
		"/tmp/x",
		"/private/tmp/x",
		"/var/folders/zz/T/x",
		"/private/var/folders/zz/T/x",
		"//tmp/x",
		"/var/../tmp/x",
		"$TMPDIR/x",
		"${TMPDIR}/x",
		"/custom/tmpdir/x",
		"../../tmp/x",
		"file:///tmp/x",
		"file://localhost/tmp/x",
		"~/x",
	} {
		if got := tempEvidence([]string{"docs/ok.md", item}); got != item {
			t.Errorf("tempEvidence(%q) = %q, want it refused", item, got)
		}
	}
	for _, item := range []string{
		"https://example.com/tmp/x",
		"docs/x.md",
		"F-0001",
		"go test ./...",
		"/var/lib/x",
		"/Users/me/x",
		"/tmpfoo/x",
	} {
		if got := tempEvidence([]string{item}); got != "" {
			t.Errorf("tempEvidence(%q) = %q, want it accepted", item, got)
		}
	}
}
