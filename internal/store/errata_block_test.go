package store

import (
	"errors"
	"strings"
	"testing"
)

func strip(t *testing.T, in, block string) string {
	t.Helper()
	got, err := StripErrataBlock([]byte(in), block)
	if err != nil {
		t.Fatalf("strip: %v", err)
	}
	return string(got)
}

func TestErrataBlockRoundTrip(t *testing.T) {
	block := ErrataBlock("## errata 2026-01-01 — a\n\ncorrigido", nil)
	if !strings.Contains(block, "corrigido") || ErrataBlock(" \n", nil) != "" {
		t.Fatalf("render: %q", block)
	}
	for _, body := range []string{"# T\n\ncorpo\n", "sem newline final", ""} {
		if got := strip(t, body+block, block); got != body {
			t.Fatalf("strip(body+block) = %q, want %q", got, body)
		}
	}
}

func TestStripErrataBlockExact(t *testing.T) {
	block := ErrataBlock("corrigido", nil)
	example := "```\n" + errataBlockBegin + " — x -->\n## Errata\n\nexemplo\n" + errataBlockEnd + "\n```\n"
	cases := []struct{ name, in, blk, want string }{
		{"no block recorded: authored markers are the author's", "corpo\n" + block, "", "corpo\n" + block},
		{"text after the block is kept", "corpo\n" + block + "\nnota\n", block, "corpo\n\nnota\n"},
		{"final newline dropped at EOF", "corpo\n" + strings.TrimSuffix(block, "\n"), block, "corpo\n"},
		{"crlf file", strings.ReplaceAll("corpo\n"+block, "\n", "\r\n"), block, "corpo\n"},
		{"a user example after the generated block stays", "corpo\n" + block + "\n" + example, block, "corpo\n\n" + example},
		{"block removed entirely: nothing to strip", "corpo\n", block, "corpo\n"},
	}
	for _, c := range cases {
		if got := strip(t, c.in, c.blk); got != c.want {
			t.Errorf("%s: strip = %q, want %q", c.name, got, c.want)
		}
	}
}

// Fail closed: the recorded block is not there verbatim but a marker is.
func TestStripErrataBlockAlteredFailsClosed(t *testing.T) {
	block := ErrataBlock("corrigido", nil)
	i := strings.Index(block, "<!-- cerne:errata:begin")
	j := strings.Index(block, "## Errata")
	for name, in := range map[string]string{
		"begin line removed":  "corpo\n" + block[:i] + block[j:],
		"end marker removed":  "corpo\n" + strings.Replace(block, errataBlockEnd+"\n", "", 1),
		"text inside edited":  "corpo\n" + strings.Replace(block, "corrigido", "mexido", 1),
		"begin line reindent": "corpo\n" + strings.Replace(block, "\n<!-- cerne:errata:begin", "\n  <!-- cerne:errata:begin", 1),
	} {
		if _, err := StripErrataBlock([]byte(in), block); !errors.Is(err, ErrBrokenErrataBlock) {
			t.Errorf("%s: err = %v, want ErrBrokenErrataBlock", name, err)
		}
	}
}

func TestErrataBlockDefangsMarkers(t *testing.T) {
	block := ErrataBlock("cita\n<!-- cerne:errata:begin x -->\n<!-- cerne:errata:end -->\nfim", nil)
	if got := strip(t, "corpo\n"+block, block); got != "corpo\n" {
		t.Fatalf("a quoted marker leaked: %q", got)
	}
}

func TestCheckNoNewErrataMarker(t *testing.T) {
	blk := "x\n" + errataBlockBegin + " -->\ny\n"
	for name, c := range map[string]struct {
		out, cur string
		bad      bool
	}{
		"more markers than the backend": {"a\n> " + errataBlockBegin, "a\n", true},
		"same count (authored)":         {blk + "edit", blk, false},
		"none":                          {"a", "a", false},
	} {
		err := CheckNoNewErrataMarker([]byte(c.out), []byte(c.cur))
		if c.bad != errors.Is(err, ErrBrokenErrataBlock) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

// The generated block is appended last: a verbatim copy the user pasted before
// it stays, the trailing one goes.
func TestStripErrataBlockPrefersTheTrailingOccurrence(t *testing.T) {
	block := ErrataBlock("corrigido", nil)
	in := "corpo\n" + block + "\ntexto\n" + block
	want := "corpo\n" + block + "\ntexto\n"
	if got := strip(t, in, block); got != want {
		t.Fatalf("strip = %q, want %q", got, want)
	}
}

func TestSaveErrataBlocksReplacesAndRemoves(t *testing.T) {
	dir := t.TempDir()
	if err := SaveErrataBlocks(dir, map[string]string{"a.md": "B"}); err != nil {
		t.Fatal(err)
	}
	if err := SaveErrataBlocks(dir, map[string]string{}); err != nil {
		t.Fatal(err)
	}
	if m, err := LoadErrataBlocks(dir); err != nil || len(m) != 0 {
		t.Fatalf("stale sidecar survived an empty save: %v %v", m, err)
	}
}
