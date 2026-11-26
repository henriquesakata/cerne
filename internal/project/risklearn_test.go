package project

import (
	"errors"
	"slices"
	"testing"
)

// TestSettingsRegistryRiskCalibration: gates.risk_calibration is the owner's
// project key, 20 by default; zero turns calibration off and is stored, a
// negative number is refused, and [gates] reads what was set.
// F-0518 crit. 12
func TestSettingsRegistryRiskCalibration(t *testing.T) {
	s := declared("gates.risk_calibration")
	if s.Type != "int" || s.Default != int64(20) || !s.HumanOnly || !slices.Equal(s.Scopes, []string{ScopeProject}) || s.Group != "process" {
		t.Fatalf("gates.risk_calibration = %+v, want a human-only project int of the process group, 20 by default", s)
	}
	if got := (GatesConfig{}).RiskCalibration(); got != 20 {
		t.Errorf("RiskCalibration() unset = %d, want 20", got)
	}
	projDir, _ := settingsFixture(t, gatesBlock, "", false)
	if err := SetSetting(projDir, "gates.risk_calibration", ScopeProject, "-1"); !errors.Is(err, ErrInvalidSettingValue) {
		t.Errorf("set -1: err = %v, want ErrInvalidSettingValue", err)
	}
	for _, c := range []struct {
		raw  string
		want int
	}{{"0", 0}, {"5", 5}} {
		if err := SetSetting(projDir, "gates.risk_calibration", ScopeProject, c.raw); err != nil {
			t.Fatalf("set %s: %v", c.raw, err)
		}
		cfg, _, err := LoadConfig(projDir)
		if err != nil || cfg.Gates.RiskCalibration() != c.want {
			t.Errorf("after set %s: RiskCalibration() = %d (%v), want %d", c.raw, cfg.Gates.RiskCalibration(), err, c.want)
		}
		if v := settingValue(t, projDir, "gates.risk_calibration"); v != int64(c.want) {
			t.Errorf("after set %s: resolved %v, want %d", c.raw, v, c.want)
		}
	}
}

// settingValue is id's resolved value for projDir.
func settingValue(t *testing.T, projDir, id string) any {
	t.Helper()
	for _, s := range ResolveSettings(projDir).Settings {
		if s.ID == id {
			return s.Value
		}
	}
	t.Fatalf("%s is not declared", id)
	return nil
}
