package store

import (
	"errors"
	"strings"
	"testing"
)

func TestErrataBlockRoundTrip(t *testing.T) {
	for _, body := range []string{"# T\n\ncorpo\n", "sem newline final", ""} {
		block := ErrataBlock("## errata 2026-01-01 — a\n\ncorrigido", nil)
		if !strings.Contains(block, "corrigido") {
			t.Fatalf("block lost the text: %q", block)
		}
		if got := string(mustStrip(t, body+block)); got != body {
			t.Fatalf("strip(body+block) = %q, want %q", got, body)
		}
	}
	if ErrataBlock(" \n", nil) != "" {
		t.Fatal("blank errata must render nothing")
	}
	if got := mustStrip(t, "no block\n"); string(got) != "no block\n" {
		t.Fatalf("body without block changed: %q", got)
	}
}

func mustStrip(t *testing.T, s string) []byte {
	t.Helper()
	got, err := StripErrataBlock([]byte(s))
	if err != nil {
		t.Fatalf("strip: %v", err)
	}
	return got
}

func TestStripErrataBlockTolerance(t *testing.T) {
	block := ErrataBlock("## errata 2026-01-01 — a\n\ncorrigido", nil)
	crlf := strings.ReplaceAll("corpo\n"+block, "\n", "\r\n")
	cases := []struct{ name, in, want string }{
		{"text after the block is kept", "corpo\n" + block + "\nnota do usuario\n", "corpo\n\nnota do usuario\n"},
		{"no trailing newline", "corpo\n" + strings.TrimSuffix(block, "\n"), "corpo\n"},
		{"crlf", crlf, "corpo\r\n"},
		{"a body that already ends with a block", "corpo\n" + ErrataBlock("antiga", nil) + block, "corpo\n" + ErrataBlock("antiga", nil)},
	}
	for _, c := range cases {
		if got := string(mustStrip(t, c.in)); got != c.want {
			t.Errorf("%s: strip = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestErrataBlockDefangsMarkers(t *testing.T) {
	block := ErrataBlock("cita <!-- cerne:errata:begin x --> e <!-- cerne:errata:end --> no meio", nil)
	if got := string(mustStrip(t, "corpo\n"+block)); got != "corpo\n" {
		t.Fatalf("a quoted marker leaked into the body: %q", got)
	}
}

func TestStripErrataBlockBrokenFailsClosed(t *testing.T) {
	_, err := StripErrataBlock([]byte("corpo\n\n" + errataBlockBegin + " — x -->\n## Errata\n"))
	if !errors.Is(err, ErrBrokenErrataBlock) {
		t.Fatalf("err = %v, want ErrBrokenErrataBlock", err)
	}
}
