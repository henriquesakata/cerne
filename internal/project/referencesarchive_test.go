package project

import (
	"slices"
	"testing"
)

// F-0503 crit. 1: references.archive is a bool, off by default, settable in
// the project and the host scope, in the integrations group, human-only.
func TestReferencesArchiveSettingDeclared(t *testing.T) {
	projDir, _ := settingsFixture(t, "", "", false)
	s := findSetting(t, ResolveSettings(projDir), "references.archive")
	if s.Type != "bool" || s.Default != false || s.Value != false || s.Origin != OriginDefault {
		t.Errorf("references.archive = type %q, default %v, value %v from %s; want bool, false, false from default", s.Type, s.Default, s.Value, s.Origin)
	}
	if !slices.Equal(s.Scopes, []string{ScopeProject, ScopeHost}) || s.Group != "integrations" || s.Title == "" || s.Description == "" {
		t.Errorf("references.archive = scopes %v, group %q, title %q; want [project host], integrations, a title and a description", s.Scopes, s.Group, s.Title)
	}
	if !s.HumanOnly || !SettingHumanOnly("references.archive") {
		t.Error("references.archive must be human-only")
	}
}
