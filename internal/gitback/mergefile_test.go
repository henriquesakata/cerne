package gitback

import "testing"

func TestMergeFile(t *testing.T) {
	requireGitBin(t)
	base := []byte("P1\n\nP2\n\nP3\n")
	merged, ok := MergeFile(base, []byte("P1\n\nP2 mine\n\nP3\n"), []byte("P1\n\nP2\n\nP3 theirs\n"))
	if !ok || string(merged) != "P1\n\nP2 mine\n\nP3 theirs\n" {
		t.Fatalf("disjoint hunks: merged=%q ok=%v, want both edits", merged, ok)
	}
	if merged, ok := MergeFile(base, []byte("P1\n\nP2 mine\n\nP3\n"), []byte("P1\n\nP2 theirs\n\nP3\n")); ok {
		t.Fatalf("overlapping hunks merged: %q", merged)
	}
}

func TestMergeFileWithoutGitBinIsNotOK(t *testing.T) {
	withoutGitBin(t)
	if merged, ok := MergeFile([]byte("a\n"), []byte("b\n"), []byte("a\n")); ok {
		t.Fatalf("merge without git binary: %q", merged)
	}
}
