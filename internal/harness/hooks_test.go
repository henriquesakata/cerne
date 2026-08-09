package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/hooks"
)

// findItem returns the first item of class whose name matches.
func findItem(inv Inventory, class, name string) (Item, bool) {
	for _, c := range inv.Classes {
		if c.Name != class {
			continue
		}
		for _, it := range c.Items {
			if it.Name == name {
				return it, true
			}
		}
	}
	return Item{}, false
}

func TestManagedHookCapabilityStates(t *testing.T) {
	proj := t.TempDir()
	if _, err := hooks.Install(proj); err != nil {
		t.Fatal(err)
	}
	first := hooks.Assets()[0].Script

	inv := Inspect(Options{ProjDir: proj})
	it, ok := findItem(inv, "hooks", first)
	if !ok {
		t.Fatalf("managed capability %s absent from inventory", first)
	}
	if it.State != StateOK {
		t.Errorf("installed capability = %s (%s), want ok", it.State, it.Detail)
	}
	if it.OutsideRepo {
		t.Error("project hook marked outside repo")
	}

	// One flipped byte → divergent, with the repair command.
	path := filepath.Join(proj, ".claude", "hooks", first)
	raw, _ := os.ReadFile(path)
	raw[0] ^= 1
	os.WriteFile(path, raw, 0o644)
	inv = Inspect(Options{ProjDir: proj})
	it, _ = findItem(inv, "hooks", first)
	if it.State != StateDivergent || it.Fix != "cerne hook install" {
		t.Errorf("divergent capability = %s fix=%q, want divergent + cerne hook install", it.State, it.Fix)
	}

	// Script gone → capability missing even though wiring stays.
	os.Remove(path)
	inv = Inspect(Options{ProjDir: proj})
	it, _ = findItem(inv, "hooks", first)
	if it.State != StateMissing {
		t.Errorf("script removed = %s, want missing", it.State)
	}
}

func TestWiringHalfMissing(t *testing.T) {
	proj := t.TempDir()
	if _, err := hooks.Install(proj); err != nil {
		t.Fatal(err)
	}
	// Script on disk, wiring gone: capability is missing, detail names wiring.
	if err := os.Remove(filepath.Join(proj, ".claude", "settings.json")); err != nil {
		t.Fatal(err)
	}
	inv := Inspect(Options{ProjDir: proj})
	it, ok := findItem(inv, "hooks", hooks.Assets()[0].Script)
	if !ok {
		t.Fatal("capability absent")
	}
	if it.State != StateMissing {
		t.Errorf("wiring removed = %s, want missing", it.State)
	}
	if it.Detail == "" {
		t.Error("missing wiring reported without detail naming the half")
	}
}

func TestExternalBinaryBrokenAndConfigured(t *testing.T) {
	proj := t.TempDir()
	writeFile(t, filepath.Join(proj, ".claude", "settings.json"),
		`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"rtk hook-rewrite"}]}]}}`)

	empty := t.TempDir()
	t.Setenv("PATH", empty)
	inv := Inspect(Options{ProjDir: proj})
	it, ok := findItem(inv, "hooks", "rtk")
	if !ok {
		t.Fatal("external hook binary absent from inventory")
	}
	if it.State != StateBroken {
		t.Errorf("unresolvable binary = %s, want broken", it.State)
	}
	if !strings.Contains(it.Detail, "rtk") {
		t.Errorf("detail %q does not name the missing binary", it.Detail)
	}

	// Stub on PATH → configured.
	stub := filepath.Join(empty, "rtk")
	os.WriteFile(stub, []byte("#!/bin/sh\n"), 0o755)
	inv = Inspect(Options{ProjDir: proj})
	it, _ = findItem(inv, "hooks", "rtk")
	if it.State != StateConfigured {
		t.Errorf("resolvable binary = %s, want configured", it.State)
	}
}

func TestUnmanagedHookScript(t *testing.T) {
	proj := t.TempDir()
	writeFile(t, filepath.Join(proj, ".claude", "hooks", "no-new-case-dir.sh"), "#!/bin/sh\nexit 0\n")
	inv := Inspect(Options{ProjDir: proj})
	it, ok := findItem(inv, "hooks", "no-new-case-dir.sh")
	if !ok {
		t.Fatal("unmanaged hook script absent from inventory")
	}
	if it.State != StateUnmanaged {
		t.Errorf("manual hook = %s, want unmanaged", it.State)
	}
	if it.Fix != "" {
		t.Errorf("unmanaged item suggests fix %q, want none", it.Fix)
	}
}

func TestUserOriginHookOutsideRepo(t *testing.T) {
	proj := t.TempDir()
	home := t.TempDir()
	writeFile(t, filepath.Join(home, ".claude", "settings.json"),
		`{"hooks":{"PostToolUse":[{"hooks":[{"type":"command","command":"mytool observe"}]}]}}`)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "mytool"), []byte("#!/bin/sh\n"), 0o755)
	t.Setenv("PATH", dir)

	inv := Inspect(Options{ProjDir: proj, Home: home})
	it, ok := findItem(inv, "hooks", "mytool")
	if !ok {
		t.Fatal("user-layer hook absent")
	}
	if it.Origin != OriginUser || !it.OutsideRepo {
		t.Errorf("user hook origin=%s outside=%v, want user/true", it.Origin, it.OutsideRepo)
	}
}
