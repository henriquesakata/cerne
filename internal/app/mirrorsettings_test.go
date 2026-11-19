package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/project"
)

// F-0449 crit. 3: mirror.enabled and mirror.trackers refuse an agent actor
// through the F-0392 human-only guard (CERNE_ACTOR and the Copilot shell
// marker alike); mirror.close_on and the delivery keys stay open to it, and a
// human passes on every key.
func TestMirrorSettingsHumanOnly(t *testing.T) {
	for _, tc := range []struct {
		name, actor, copilot, id string
		refused                  bool
	}{
		{"agent on mirror.enabled", "agent", "", "mirror.enabled", true},
		{"agent on mirror.trackers", "agent", "", "mirror.trackers", true},
		{"prefixed agent on mirror.trackers", "agent:dev", "", "mirror.trackers", true},
		{"Copilot shell on mirror.enabled", "", "1", "mirror.enabled", true},
		{"agent on mirror.close_on", "agent", "", "mirror.close_on", false},
		{"agent on delivery.commit_url", "agent", "", "delivery.commit_url", false},
		{"human on mirror.enabled", "", "", "mirror.enabled", false},
		{"human on mirror.trackers", "", "", "mirror.trackers", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CERNE_ACTOR", tc.actor)
			t.Setenv("COPILOT_CLI", tc.copilot)
			t.Setenv("CURSOR_AGENT", "")
			err := CheckHumanSetting(tc.id)
			if tc.refused != errors.Is(err, ErrHumanGate) || (!tc.refused && err != nil) {
				t.Fatalf("CheckHumanSetting(%s) as %q = %v, refused want %v", tc.id, tc.actor, err, tc.refused)
			}
			if tc.refused && !strings.Contains(err.Error(), tc.id) {
				t.Fatalf("refusal %q does not name %s", err, tc.id)
			}
		})
	}
	for _, id := range []string{"mirror.enabled", "mirror.trackers"} {
		if !project.SettingHumanOnly(id) {
			t.Errorf("%s is not declared human-only", id)
		}
	}
}
