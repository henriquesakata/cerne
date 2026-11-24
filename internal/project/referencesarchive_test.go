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

// F-0503 crit. 2: ReferencesArchive is on only when the setting resolves to
// true; unset, false or an invalid value is off.
func TestReferencesArchiveOnlyWhenTrue(t *testing.T) {
	for _, tc := range []struct {
		name, project, host string
		want                bool
	}{
		{"unset", "", "", false},
		{"project on", "[references]\narchive = true\n", "", true},
		{"host on", "", "[references]\narchive = true\n", true},
		{"project off over host on", "[references]\narchive = false\n", "[references]\narchive = true\n", false},
		{"invalid", "[references]\narchive = \"yes\"\n", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			projDir, _ := settingsFixture(t, tc.project, tc.host, false)
			if got := ReferencesArchive(projDir); got != tc.want {
				t.Errorf("ReferencesArchive = %v, want %v", got, tc.want)
			}
		})
	}
}
