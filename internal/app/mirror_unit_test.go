package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/tracker"
)

// TestPickTracker: "" is the first tracker; a name picks its own; an unknown
// name lists the configured ones; none configured says where to add one.
func TestPickTracker(t *testing.T) {
	cfg := project.MirrorConfig{Trackers: []tracker.Config{{Name: "a"}, {Name: "b"}}}
	for name, want := range map[string]string{"": "a", "b": "b"} {
		if got, err := pickTracker(cfg, name); err != nil || got.Name != want {
			t.Errorf("pickTracker(%q) = %q, %v; want %q", name, got.Name, err, want)
		}
	}
	if _, err := pickTracker(cfg, "c"); !errors.Is(err, ErrUnknownTracker) || !strings.Contains(err.Error(), `"c" (configured: a, b)`) {
		t.Errorf("pickTracker(c) = %v, want ErrUnknownTracker naming a, b", err)
	}
	if _, err := pickTracker(project.MirrorConfig{}, ""); !errors.Is(err, ErrUnknownTracker) {
		t.Errorf("pickTracker with no tracker = %v, want ErrUnknownTracker", err)
	}
}
