package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// B-0052: an unwritable cache (agent sandbox) must be explained as a
// permission problem naming the folders to open, never as "delete the cache".
func TestUnwritableCacheIsExplainedNotDeleted(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	projDir, origin, repoDir := setupProject(t)
	cacheDir := filepath.Dir(repoDir)
	if err := os.Chmod(cacheDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(cacheDir, 0o755) })

	for _, args := range [][]string{
		{"next", "--dir", projDir},
		{"pull", "--dir", projDir},
		{"search", "x", "--dir", projDir},
	} {
		out, err := execCerne(t, args...)
		msg := out
		if err != nil {
			msg += err.Error()
		}
		if err == nil {
			t.Errorf("%v: expected a failure on a read-only cache", args)
			continue
		}
		for _, want := range []string{"does not accept writes", cacheDir, origin} {
			if !strings.Contains(msg, want) {
				t.Errorf("%v: message lacks %q:\n%s", args, want, msg)
			}
		}
		if strings.Contains(msg, "delete that") {
			t.Errorf("%v: message must not advise deleting:\n%s", args, msg)
		}
	}
}
