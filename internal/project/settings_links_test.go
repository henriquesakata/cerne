package project

import (
	"slices"
	"testing"
)

// TestLinksSuggestSetting pins F-0502 crit. 1: links.suggest is a bool, off by
// default, project and host scoped, human-only; LinksSuggest reads it and is
// false unless a scope turns it on.
func TestLinksSuggestSetting(t *testing.T) { // F-0502 crit. 1
	var s Setting
	for _, r := range settingsRegistry {
		if r.ID == "links.suggest" {
			s = r
		}
	}
	if s.Type != "bool" || s.Default != false || !s.HumanOnly || s.Title == "" || s.Description == "" ||
		!slices.Equal(s.Scopes, []string{ScopeProject, ScopeHost}) {
		t.Fatalf("links.suggest = %+v, want bool, default false, scopes [project host], human-only", s)
	}
	if !SettingHumanOnly("links.suggest") {
		t.Error("SettingHumanOnly(links.suggest) = false")
	}
	projDir, _ := settingsFixture(t, "", "", false)
	if on, err := LinksSuggest(projDir); err != nil || on {
		t.Fatalf("default LinksSuggest = %v, %v; want false", on, err)
	}
	if err := SetSetting(projDir, "links.suggest", ScopeProject, "true"); err != nil {
		t.Fatal(err)
	}
	if on, err := LinksSuggest(projDir); err != nil || !on {
		t.Fatalf("after set LinksSuggest = %v, %v; want true", on, err)
	}
	if err := UnsetSetting(projDir, "links.suggest", ScopeProject); err != nil {
		t.Fatal(err)
	}
	if on, _ := LinksSuggest(projDir); on {
		t.Error("after unset LinksSuggest = true")
	}
}
