package cli

import (
	"strings"
	"testing"
)

// E-0005 criterion 14 (owner's adjustment, T-0017): cerne note link adds and
// removes what a note informs and prints the final list.
func TestNoteLinkCmd(t *testing.T) {
	projDir, _, _ := setupProject(t)
	newFeature(t, projDir, "Feature A", 1)
	mustCerne(t, "note", "new", "Uma regra solta", "--dir", projDir)
	if out := mustCerne(t, "note", "link", "N-0001", "--informs", "F-0001", "--dir", projDir); !strings.Contains(out, "N-0001 informs: F-0001\n") {
		t.Fatalf("link:\n%s", out)
	}
	if out := mustCerne(t, "note", "link", "N-0001", "--unlink", "F-0001", "--dir", projDir); !strings.Contains(out, "N-0001 informs: (none)\n") {
		t.Fatalf("unlink:\n%s", out)
	}
	if _, err := execCerne(t, "note", "link", "N-0001", "--dir", projDir); err == nil || !strings.Contains(err.Error(), "--informs or --unlink") {
		t.Fatalf("no flag: %v", err)
	}
}
