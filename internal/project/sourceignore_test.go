package project

import (
	"slices"
	"testing"
)

func TestSourceIgnoreAddsRepoPrefix(t *testing.T) {
	for tmpl, want := range map[string]string{
		"https://github.com/henriquesakata/cerne/commit/{sha}": "https://github.com/henriquesakata/cerne/",
		"https://gitlab.com/rockt/cerne/-/commit/{sha}":        "https://gitlab.com/rockt/cerne/",
		"https://git.rockt.com.br/rockt/hub/commit/{sha}":      "https://git.rockt.com.br/rockt/hub/",
	} {
		c := Config{UnregisteredSourceIgnore: []string{"keep.me"}, Delivery: DeliveryConfig{CommitURL: tmpl}}
		got := c.SourceIgnore()
		if !slices.Equal(got, []string{"keep.me", want}) {
			t.Errorf("%s: got %v", tmpl, got)
		}
		if len(c.UnregisteredSourceIgnore) != 1 {
			t.Errorf("the configured list must not be mutated: %v", c.UnregisteredSourceIgnore)
		}
	}
	for _, tmpl := range []string{"", "not a url", "https://example.com/no-commit-here"} {
		if got := (Config{Delivery: DeliveryConfig{CommitURL: tmpl}}).SourceIgnore(); len(got) != 0 {
			t.Errorf("%q: got %v, want none", tmpl, got)
		}
	}
}
