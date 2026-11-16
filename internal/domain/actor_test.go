package domain

import (
	"strings"
	"testing"
)

func TestMachinePseudonym(t *testing.T) {
	a := MachinePseudonym("alice@m1")
	if a != MachinePseudonym("Alice@M1") || a != MachinePseudonym("alice@m1") {
		t.Fatalf("not stable / case-insensitive: %s", a)
	}
	if a != "m-f6c9ba7c" { // golden vector: sha256("alice@m1")[:8], so no test mirrors the function
		t.Fatalf("golden: %s", a)
	}
	if len(a) != 10 || !strings.HasPrefix(a, "m-") {
		t.Fatalf("shape: %s", a)
	}
	seen := map[string]bool{}
	for _, s := range []string{"alice@m1", "alice@m2", "bob@m1"} {
		seen[MachinePseudonym(s)] = true
	}
	if len(seen) != 3 {
		t.Fatalf("not distinct: %v", seen)
	}
}

func TestActorLabel(t *testing.T) {
	p := MachinePseudonym("alice@mac.local")
	cases := map[string]string{
		"alice@mac.local":        p,
		"agent:alice@mac.local":  "agent:" + p,
		"fulano":                 "fulano",
		"agent":                  "agent",
		"human":                  "human",
		"agent:session-0123abcd": "agent:session-0123abcd",
		"agent:" + p:             "agent:" + p,
		"":                       "",
	}
	for in, want := range cases {
		if got := ActorLabel(in); got != want {
			t.Errorf("ActorLabel(%q)=%q want %q", in, got, want)
		}
	}
}
