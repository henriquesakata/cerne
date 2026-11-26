package project

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// gatesBlock is a [gates] block holding keys the registry does not declare,
// which a write of approve or accept must keep as they are.
const gatesBlock = "[gates]\ngauntlet = \"off\"\nwork_gate = \"on\"\nwork_gate_allow = [\"docs/**\"]\n"

// TestSettingsRegistryGates: gates.approve and gates.accept are declared in
// the process group, project scope, human only, default human, with the
// values each takes, and a description saying what each value does and that
// an agent does not change the key through cerne settings.
// F-0515 crit. 1
func TestSettingsRegistryGates(t *testing.T) {
	for _, tc := range []struct {
		id     string
		values []string
	}{
		{"gates.approve", []string{"human", "auto"}},
		{"gates.accept", []string{"human", "merge", "auto"}},
	} {
		i := slices.IndexFunc(settingsRegistry, func(s Setting) bool { return s.ID == tc.id })
		if i < 0 {
			t.Errorf("registry lacks %s", tc.id)
			continue
		}
		s := settingsRegistry[i]
		if s.Group != "process" || s.Type != "string" || s.Default != "human" || !s.HumanOnly || !slices.Equal(s.Scopes, []string{ScopeProject}) {
			t.Errorf("%s = %+v, want process, string, default human, human only, scope [project]", tc.id, s)
		}
		if !slices.Equal(s.Enum, tc.values) {
			t.Errorf("%s enum = %v, want %v", tc.id, s.Enum, tc.values)
		}
		for _, v := range tc.values {
			if !strings.Contains(s.Description, "("+v+"):") {
				t.Errorf("%s description does not say what %s does: %q", tc.id, v, s.Description)
			}
		}
		if !strings.Contains(s.Description, "An agent cannot change this key through cerne settings") {
			t.Errorf("%s description does not say an agent cannot change it: %q", tc.id, s.Description)
		}
		// The guard's reach: the command and the UI's question, not the file
		// nor the local settings API.
		for _, limit := range []string{".cernerc by hand", "settings API"} {
			if !strings.Contains(s.Description, limit) {
				t.Errorf("%s description does not say %q still changes it: %q", tc.id, limit, s.Description)
			}
		}
	}
}

// TestSettingsSetGatesWritesCernerc: a write lands in [gates], keeps the
// block's other keys, and the gate accessors read it back.
// F-0515 crit. 2
func TestSettingsSetGatesWritesCernerc(t *testing.T) {
	for _, tc := range []struct {
		id, value           string
		wantApprove, wantAc string
	}{
		{"gates.approve", "auto", GateAuto, GateHuman},
		{"gates.approve", "human", GateHuman, GateHuman},
		{"gates.accept", "auto", GateHuman, GateAuto},
		{"gates.accept", "merge", GateHuman, GateMerge},
		{"gates.accept", "human", GateHuman, GateHuman},
	} {
		t.Run(tc.id+"="+tc.value, func(t *testing.T) {
			projDir, _ := settingsFixture(t, gatesBlock, "", false)
			if err := SetSetting(projDir, tc.id, ScopeProject, tc.value); err != nil {
				t.Fatalf("SetSetting: %v", err)
			}
			raw, err := os.ReadFile(filepath.Join(projDir, ".cernerc"))
			if err != nil {
				t.Fatal(err)
			}
			_, key, _ := strings.Cut(tc.id, ".")
			for _, line := range []string{"gauntlet = \"off\"", "work_gate = \"on\"", "work_gate_allow = [\"docs/**\"]", key + " = \"" + tc.value + "\""} {
				if !strings.Contains(string(raw), line) {
					t.Errorf(".cernerc lacks %q:\n%s", line, raw)
				}
			}
			cfg, _, err := LoadConfig(projDir)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Gates.ApproveGate() != tc.wantApprove || cfg.Gates.AcceptGate() != tc.wantAc {
				t.Errorf("gates = approve %s, accept %s; want %s, %s", cfg.Gates.ApproveGate(), cfg.Gates.AcceptGate(), tc.wantApprove, tc.wantAc)
			}
			if cfg.Gates.Gauntlet != "off" || !cfg.Gates.WorkGateEnabled() || !slices.Equal(cfg.Gates.WorkGateAllow, []string{"docs/**"}) {
				t.Errorf("the other [gates] keys changed: %+v", cfg.Gates)
			}
		})
	}
}

// TestSettingsUnsetGates: unset hands both keys back to the human default.
// F-0515 crit. 2
func TestSettingsUnsetGates(t *testing.T) {
	projDir, _ := settingsFixture(t, gatesBlock+"approve = \"auto\"\naccept = \"merge\"\n", "", false)
	for _, id := range []string{"gates.approve", "gates.accept"} {
		if err := UnsetSetting(projDir, id, ScopeProject); err != nil {
			t.Fatalf("UnsetSetting %s: %v", id, err)
		}
		if s := findSetting(t, ResolveSettings(projDir), id); s.Value != "human" || s.Origin != OriginDefault {
			t.Errorf("%s after unset = %v (%s), want human (default)", id, s.Value, s.Origin)
		}
	}
	cfg, _, err := LoadConfig(projDir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Gates.ApproveGate() != GateHuman || cfg.Gates.AcceptGate() != GateHuman || cfg.Gates.Gauntlet != "off" {
		t.Errorf("gates after unset = %+v, want human on both ends and gauntlet kept", cfg.Gates)
	}
}

// TestSettingsSetGatesInvalidValue: a value outside the key's list is the
// enum refusal, naming the accepted values, and writes nothing.
// F-0515 crit. 4
func TestSettingsSetGatesInvalidValue(t *testing.T) {
	projDir, _ := settingsFixture(t, gatesBlock, "", false)
	rc := filepath.Join(projDir, ".cernerc")
	before, _ := os.ReadFile(rc)
	for _, tc := range []struct{ id, value, want string }{
		{"gates.approve", "merge", "want one of: human, auto"},
		{"gates.accept", "always", "want one of: human, merge, auto"},
	} {
		err := SetSetting(projDir, tc.id, ScopeProject, tc.value)
		if !errors.Is(err, ErrInvalidSettingValue) || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("SetSetting %s=%s: err = %v, want ErrInvalidSettingValue naming %q", tc.id, tc.value, err, tc.want)
		}
	}
	if after, _ := os.ReadFile(rc); string(after) != string(before) {
		t.Errorf("a refused write changed .cernerc:\n%s", after)
	}
}
