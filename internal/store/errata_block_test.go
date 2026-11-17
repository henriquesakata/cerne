package store

import (
	"strings"
	"testing"
)

func TestErrataBlockRoundTrip(t *testing.T) {
	for _, body := range []string{"# T\n\ncorpo\n", "sem newline final", ""} {
		block := ErrataBlock("## errata 2026-01-01 — a\n\ncorrigido", nil)
		if !strings.Contains(block, "corrigido") {
			t.Fatalf("block lost the text: %q", block)
		}
		if got := string(StripErrataBlock([]byte(body + block))); got != body {
			t.Fatalf("strip(body+block) = %q, want %q", got, body)
		}
	}
	if ErrataBlock(" \n", nil) != "" {
		t.Fatal("blank errata must render nothing")
	}
	if got := StripErrataBlock([]byte("no block\n")); string(got) != "no block\n" {
		t.Fatalf("body without block changed: %q", got)
	}
}
