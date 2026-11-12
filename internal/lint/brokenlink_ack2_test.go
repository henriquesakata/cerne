package lint

import "testing"

// F-0352 A3: the marker also clears a path target (fragment ignored on both
// sides) and an id written in another case/padding.
func TestBrokenLinkAckPathAndLooseID(t *testing.T) {
	dir := brokenLinkBackend(t, "# Spec\n\ntexto\n")
	appendErr(t, dir, 1, "cita [x](./nada.md) e [y](./a.md#s) e [[f-12]]")
	if got := errataBrokenLinks(t, dir); len(got) != 3 {
		t.Fatalf("before the marker: %+v, want 3", got)
	}
	appendErr(t, dir, 2, "<!-- lint:ok-broken-link ./nada.md --> histórico")
	appendErr(t, dir, 3, "<!-- lint:ok-broken-link ./a.md --> histórico")
	appendErr(t, dir, 4, "<!-- lint:ok-broken-link F-0012 , f-12 --> histórico")
	if got := errataBrokenLinks(t, dir); len(got) != 0 {
		t.Fatalf("markers must clear path and loose-id targets, got %+v", got)
	}
}
