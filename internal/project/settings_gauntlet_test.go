package project

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func gauntletCernerc(t *testing.T, projDir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(projDir, ".cernerc"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestGauntletSettingDeclared pins F-0392 criterion 1: gates.gauntlet is a
// project-scope string with values on/off, default on, marked human-only.
func TestGauntletSettingDeclared(t *testing.T) {
	var s Setting
	for _, r := range settingsRegistry {
		if r.ID == "gates.gauntlet" {
			s = r
		}
	}
	if s.Type != "string" || s.Default != "on" || !slices.Equal(s.Scopes, []string{ScopeProject}) ||
		!slices.Equal(s.Enum, []string{"on", "off"}) || s.Title == "" || s.Description == "" || !s.HumanOnly {
		t.Fatalf("gates.gauntlet = %+v, want string, default on, scopes [project], enum [on off], human-only", s)
	}
	if !SettingHumanOnly("gates.gauntlet") || SettingHumanOnly("graph.enabled") || SettingHumanOnly("nope") {
		t.Error("SettingHumanOnly must be true only for gates.gauntlet")
	}
	for _, r := range settingsRegistry {
		if r.HumanOnly && r.ID != "gates.gauntlet" {
			t.Errorf("%s is human-only; only gates.gauntlet should be", r.ID)
		}
	}
}

// TestGauntletSettingResolvesFromGates: the existing [gates] gauntlet line is
// the stored value, next to other [gates] keys.
func TestGauntletSettingResolvesFromGates(t *testing.T) {
	projDir, _ := settingsFixture(t, "", "", false)
	writeCernerc(t, projDir, baseCernerc+"[gates]\napprove = \"human\"\ngauntlet = \"off\"\n")
	got := findSetting(t, ResolveSettings(projDir), "gates.gauntlet")
	if got.Value != "off" || got.Origin != ScopeProject {
		t.Errorf("gates.gauntlet = %v from %s, want off from project", got.Value, got.Origin)
	}
	d, _ := settingsFixture(t, "", "", false)
	if got := findSetting(t, ResolveSettings(d), "gates.gauntlet"); got.Value != "on" || got.Origin != OriginDefault {
		t.Errorf("unset = %v from %s, want on from default", got.Value, got.Origin)
	}
}

// TestGauntletSettingWrites pins criterion 2: only the gauntlet line moves.
func TestGauntletSettingWrites(t *testing.T) {
	projDir, _ := settingsFixture(t, "[gates]\napprove = \"human\"\naccept = \"human\"\n", "", false)
	if err := SetSetting(projDir, "gates.gauntlet", ScopeProject, "off"); err != nil {
		t.Fatal(err)
	}
	if got := gauntletCernerc(t, projDir); !strings.Contains(got, "approve = \"human\"") || !strings.Contains(got, "accept = \"human\"") || !strings.Contains(got, "gauntlet = \"off\"") {
		t.Errorf("after set: %s", got)
	}
	if err := SetSetting(projDir, "gates.gauntlet", ScopeProject, "on"); err != nil {
		t.Fatal(err)
	}
	if got := gauntletCernerc(t, projDir); !strings.Contains(got, "gauntlet = \"on\"") || strings.Contains(got, "\"off\"") {
		t.Errorf("after set on: %s", got)
	}
	if err := UnsetSetting(projDir, "gates.gauntlet", ScopeProject); err != nil {
		t.Fatal(err)
	}
	if got := gauntletCernerc(t, projDir); strings.Contains(got, "gauntlet") || !strings.Contains(got, "approve = \"human\"") || !strings.Contains(got, "accept = \"human\"") {
		t.Errorf("after unset: %s", got)
	}

	bare, _ := settingsFixture(t, "", "", false)
	if err := SetSetting(bare, "gates.gauntlet", ScopeProject, "off"); err != nil {
		t.Fatal(err)
	}
	if got := gauntletCernerc(t, bare); !strings.Contains(got, "[gates]") {
		t.Errorf("no [gates] created: %s", got)
	}
	if cfg, _, err := LoadConfig(bare); err != nil || cfg.Gates.GauntletEnabled() {
		t.Errorf("config after set off: err=%v enabled=%v", err, cfg.Gates.GauntletEnabled())
	}
	if err := SetSetting(bare, "gates.gauntlet", ScopeHost, "off"); !errors.Is(err, ErrSettingScope) {
		t.Errorf("host scope: err = %v, want ErrSettingScope", err)
	}
}

// TestGauntletSettingInvalid pins criterion 3: "of" shows as a problem, the
// effective value is on, and the gate agrees.
func TestGauntletSettingInvalid(t *testing.T) {
	projDir, _ := settingsFixture(t, "[gates]\ngauntlet = \"of\"\n", "", false)
	got := findSetting(t, ResolveSettings(projDir), "gates.gauntlet")
	if got.Value != "on" || len(got.Invalid) != 1 {
		t.Errorf("gates.gauntlet = %v invalid %v, want on with one problem", got.Value, got.Invalid)
	}
	cfg, _, err := LoadConfig(projDir)
	if err != nil || !cfg.Gates.GauntletEnabled() {
		t.Errorf("gate for \"of\": err=%v enabled=%v, want on", err, cfg.Gates.GauntletEnabled())
	}
}
