package mdlink

import (
	"reflect"
	"testing"
)

// TestExternalURLs: collects distinct http(s) URLs — whether written as a
// markdown link [x](url) or bare in prose — in first-appearance order, ignoring
// URLs inside fenced or inline code, trimming trailing sentence punctuation,
// deduping, and skipping non-http schemes and relative/anchor targets.
func TestExternalURLs(t *testing.T) {
	content := "" +
		"veja [RFC 6749](https://tools.ietf.org/html/rfc6749) e bare https://example.com/spec aqui.\n" +
		"dup de novo https://example.com/spec.\n" +
		"[rel](./x.md), [anchor](#h), [mail](mailto:a@b.com), http-not://nope\n" +
		"```\nhttps://fenced.example/ignored\n```\n" +
		"inline `https://inline.example/ignored` tambem.\n" +
		"fim: https://foo.com/bar, ponto."

	got := ExternalURLs(content)
	want := []string{
		"https://tools.ietf.org/html/rfc6749",
		"https://example.com/spec",
		"https://foo.com/bar",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ExternalURLs = %v, want %v", got, want)
	}
}

// TestExternalURLs_none: a doc with no external http(s) URL yields nil.
func TestExternalURLs_none(t *testing.T) {
	if got := ExternalURLs("só [rel](./a.md) e [anchor](#h) aqui"); got != nil {
		t.Fatalf("ExternalURLs = %v, want nil", got)
	}
}
