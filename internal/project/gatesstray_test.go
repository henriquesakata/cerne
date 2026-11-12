package project

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadConfig_WarnsStrayKeyUnderGates (F-0364): init now ends .cernerc
// with [gates], so a top-level key appended at the end lands inside it. The
// config still loads, and a warning names the key and where it belongs.
func TestLoadConfig_WarnsStrayKeyUnderGates(t *testing.T) {
	var buf bytes.Buffer
	old := configWarnings
	configWarnings = &buf
	t.Cleanup(func() { configWarnings = old })

	dir := t.TempDir()
	body := "project_id = \"demo\"\nbackend = \"b\"\n\n[gates]\ngauntlet = \"off\"\ncompile_on_push = true\n"
	if err := os.WriteFile(filepath.Join(dir, ".cernerc"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig refused the config: %v", err)
	}
	if cfg.Gates.Gauntlet != "off" {
		t.Fatalf("gauntlet = %q, want off", cfg.Gates.Gauntlet)
	}
	if w := buf.String(); !strings.Contains(w, "compile_on_push") || !strings.Contains(w, "above [gates]") {
		t.Fatalf("warning = %q, want it to name compile_on_push and say to move it above [gates]", w)
	}

	buf.Reset()
	clean := "project_id = \"demo\"\nbackend = \"b\"\ncompile_on_push = true\n\n[gates]\ngauntlet = \"off\"\n"
	if err := os.WriteFile(filepath.Join(dir, ".cernerc"), []byte(clean), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadConfig(dir); err != nil || buf.Len() != 0 {
		t.Fatalf("clean config: err = %v, warning = %q", err, buf.String())
	}
}
