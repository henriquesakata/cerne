package app

import "testing"

// F-0501 crit. 4: the hash is of the visible text, not of the raw HTML.
func TestReferenceTextHash(t *testing.T) {
	const ht = "text/html; charset=utf-8"
	base := referenceTextHash(ht, []byte(`<html><head><style>p{}</style></head><body><p>Hello   world</p><script>var t=1</script></body></html>`))
	if base == "" {
		t.Fatal("empty hash")
	}
	same := []string{
		`<html><head><style>p{color:red}</style></head><body><p>Hello world</p><script>var t=2</script><noscript>x</noscript></body></html>`,
		"<html><body>\n<p>Hello\n\tworld</p>\n</body></html>",
	}
	for _, s := range same {
		if got := referenceTextHash(ht, []byte(s)); got != base {
			t.Errorf("hash of %q differs; want same as base", s)
		}
	}
	if got := referenceTextHash(ht, []byte(`<p>Hello there</p>`)); got == base {
		t.Error("changed visible text must change the hash")
	}
	// non-HTML: raw body, so whitespace counts
	a := referenceTextHash("application/json", []byte(`{"a":1}`))
	b := referenceTextHash("application/json", []byte(`{"a": 1}`))
	if a == "" || a == b {
		t.Errorf("raw-body hash: a=%q b=%q; want distinct non-empty", a, b)
	}
}
