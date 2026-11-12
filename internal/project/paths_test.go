package project

import (
	"path/filepath"
	"runtime"
	"strings"
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

func TestBackendDataDir_EnvOverride(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CERNE_DATA_DIR", root)
	got, err := BackendDataDir("demo")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "backends", "demo.git"); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestBackendDataDir_OutsideCache(t *testing.T) {
	cache, data := t.TempDir(), t.TempDir()
	t.Setenv("CERNE_CACHE_DIR", cache)
	t.Setenv("CERNE_DATA_DIR", data)
	got, err := BackendDataDir("demo")
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(got, cache) {
		t.Fatalf("%q lives inside the cache %q", got, cache)
	}
}

func TestBackendDataDir_XDG(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("XDG data dir is the linux default")
	}
	xdg := t.TempDir()
	t.Setenv("CERNE_DATA_DIR", "")
	t.Setenv("XDG_DATA_HOME", xdg)
	got, err := BackendDataDir("demo")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(xdg, "cerne", "backends", "demo.git"); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
