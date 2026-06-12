package project

import (
	"path/filepath"
	"testing"
)

func TestCacheDirEnvOverride(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CERNE_CACHE_DIR", root)
	got, err := CacheDir("demo")
	if err != nil {
		t.Fatalf("CacheDir: %v", err)
	}
	if want := filepath.Join(root, "demo"); got != want {
		t.Fatalf("CacheDir = %q, want %q", got, want)
	}
}

func TestIndexPath(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CERNE_CACHE_DIR", root)
	got, err := IndexPath("demo")
	if err != nil {
		t.Fatalf("IndexPath: %v", err)
	}
	if want := filepath.Join(root, "demo", "index.db"); got != want {
		t.Fatalf("IndexPath = %q, want %q (sibling of repo/, not inside it)", got, want)
	}
}
