package project

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestManifestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := map[string]string{
		"CONTEXT.md":                    "aa11",
		"current/features/F-0001/spec.md": "bb22",
	}
	if err := SaveManifest(dir, want); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}
	// The sidecar lives under the gitignored .docs/, not next to docs.lock.
	if _, err := os.Stat(filepath.Join(dir, ".docs", ".manifest.json")); err != nil {
		t.Fatalf("manifest sidecar not at .docs/.manifest.json: %v", err)
	}
	got, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("LoadManifest = %+v, want %+v", got, want)
	}
}

func TestLoadManifestMissingIsEmpty(t *testing.T) {
	got, err := LoadManifest(t.TempDir())
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("LoadManifest = %+v, want empty", got)
	}
}

// A pre-0030 docs.lock embedded the manifest as `materialized`. With no sidecar
// yet, LoadManifest must fall back to that map so the first push after upgrade
// still sees what was tracked. Written as raw JSON so it survives the later
// removal of Lock.Materialized.
func TestLoadManifestFallsBackToLock(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"version":1,"backend":"/b","snapshot":"abc","materialized":{"CONTEXT.md":"deadbeef"}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, lockFilename), []byte(legacy), 0o644); err != nil {
		t.Fatalf("write legacy docs.lock: %v", err)
	}
	got, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if want := map[string]string{"CONTEXT.md": "deadbeef"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("LoadManifest = %+v, want fallback %+v", got, want)
	}
}

// The sidecar is authoritative: when both exist, the legacy lock field is
// ignored.
func TestLoadManifestPrefersSidecar(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"version":1,"backend":"/b","snapshot":"abc","materialized":{"CONTEXT.md":"stale"}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, lockFilename), []byte(legacy), 0o644); err != nil {
		t.Fatalf("write legacy docs.lock: %v", err)
	}
	if err := SaveManifest(dir, map[string]string{"CONTEXT.md": "fresh"}); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}
	got, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if want := map[string]string{"CONTEXT.md": "fresh"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("LoadManifest = %+v, want sidecar %+v", got, want)
	}
}
