package llm

import (
	"context"
	"testing"
)

func TestResolveMode(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ModeDelegation},
		{"delegation", ModeDelegation},
		{"byo-key", "byo-key"},
	}
	for _, c := range cases {
		if got := ResolveMode(c.in); got != c.want {
			t.Fatalf("ResolveMode(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestKnownModelsAndEfforts: F-0218 criterion 5 — the chat model/effort
// selector is served from these lists, never hardcoded in the UI.
func TestKnownModelsAndEfforts(t *testing.T) {
	if len(KnownModels) == 0 {
		t.Fatal("KnownModels is empty")
	}
	if len(KnownEfforts) == 0 {
		t.Fatal("KnownEfforts is empty")
	}
}

func TestFakeComplete(t *testing.T) {
	f := &Fake{Reply: "resposta"}
	got, err := f.Complete(context.Background(), "prompt-x")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got != "resposta" {
		t.Fatalf("Complete = %q, want %q", got, "resposta")
	}
	if len(f.Prompts) != 1 || f.Prompts[0] != "prompt-x" {
		t.Fatalf("recorded Prompts = %v, want [prompt-x]", f.Prompts)
	}
}
