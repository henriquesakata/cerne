package app

import (
	"errors"
	"testing"
)

// B-0082: the unknown-capability error names the id and where to find the
// valid ones, and stays matchable by errors.Is.
func TestUnknownCapabilityError(t *testing.T) {
	err := unknownCapability("C-9999")
	if !errors.Is(err, ErrUnknownCapability) {
		t.Fatalf("err = %v, want ErrUnknownCapability", err)
	}
	if got, want := err.Error(), "unknown capability: C-9999 (see 'cerne capability list --tree')"; got != want {
		t.Fatalf("err = %q, want %q", got, want)
	}
}
