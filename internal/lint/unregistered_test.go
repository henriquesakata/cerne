package lint

import (
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// TestNormalizeURL pins D2: trailing slash and #fragment are ignored when
// comparing sources, but the scheme is preserved (http ≠ https).
func TestNormalizeURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://x.com/", "https://x.com"},
		{"https://x.com/a#frag", "https://x.com/a"},
		{"https://x.com/a/", "https://x.com/a"},
		{"http://x.com", "http://x.com"},
	}
	for _, c := range cases {
		if got := normalizeURL(c.in); got != c.want {
			t.Errorf("normalizeURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if normalizeURL("http://x.com") == normalizeURL("https://x.com") {
		t.Errorf("http and https must not normalize equal")
	}
	if normalizeURL("https://x.com/a/") != normalizeURL("https://x.com/a#s") {
		t.Errorf("trailing-slash and fragment variants of one URL must normalize equal")
	}
}

// TestUnregisteredSourceFindings pins the rule: a live doc citing an external
// URL with no backing registered reference is flagged (C1); the finding is
// silenced by a current reference whose URL matches (C2a, normalized) or whose
// Informs backlinks the doc's entity (C2b, union — D1); a URL only inside code
// is ignored (C3); removing the URL clears it (C6); an obsolete reference does
// not back (spec says "atual").
func TestUnregisteredSourceFindings(t *testing.T) {
	doc := scannedDoc{entity: "F-0001", rel: "content/features/F-0001/spec.md"}
	const url = "https://tools.ietf.org/html/rfc6749"
	textWith := "Decidimos usar OAuth conforme [RFC 6749](" + url + ")."
	textFenced := "```\nveja " + url + "\n```\nsem fontes na prosa."
	textNone := "Decidimos usar OAuth, sem citar fonte."

	current := func(r store.Reference) store.Reference { r.State = domain.ReferenceCurrent; return r }

	// C1: URL citada, nenhuma referência → 1 finding com a forma esperada.
	got := unregisteredSourceFindings(doc, textWith, nil, nil)
	if len(got) != 1 {
		t.Fatalf("C1: got %d findings, want 1: %+v", len(got), got)
	}
	if got[0].Rule != RuleUnregisteredSource || got[0].Entity != "F-0001" || got[0].File != doc.rel {
		t.Fatalf("C1: finding shape wrong: %+v", got[0])
	}

	// C2a: referência atual com a mesma URL (barra final difere) → 0 (URL-match normalizada).
	refURL := []store.Reference{current(store.Reference{ID: "R-1", URL: url + "/", Informs: []string{"F-0999"}})}
	if got := unregisteredSourceFindings(doc, textWith, refURL, nil); len(got) != 0 {
		t.Fatalf("C2a: got %d findings, want 0: %+v", len(got), got)
	}

	// C2b: referência atual que informa a entidade do doc (sem casar URL) → 0 (Informs-match).
	refInforms := []store.Reference{current(store.Reference{ID: "R-2", URL: "https://outra.fonte/x", Informs: []string{"F-0001"}})}
	if got := unregisteredSourceFindings(doc, textWith, refInforms, nil); len(got) != 0 {
		t.Fatalf("C2b: got %d findings, want 0: %+v", len(got), got)
	}

	// C3: URL apenas dentro de bloco de código → 0.
	if got := unregisteredSourceFindings(doc, textFenced, nil, nil); len(got) != 0 {
		t.Fatalf("C3: got %d findings, want 0: %+v", len(got), got)
	}

	// C6: URL removida da prosa → 0.
	if got := unregisteredSourceFindings(doc, textNone, nil, nil); len(got) != 0 {
		t.Fatalf("C6: got %d findings, want 0: %+v", len(got), got)
	}

	// Referência apenas OBSOLETA com a URL não respalda (exige atual) → 1 finding.
	refObsolete := []store.Reference{{ID: "R-3", URL: url, State: domain.ReferenceObsolete}}
	if got := unregisteredSourceFindings(doc, textWith, refObsolete, nil); len(got) != 1 {
		t.Fatalf("obsolete: got %d findings, want 1: %+v", len(got), got)
	}
}

// TestIgnoredSourceURL pins C2/C3 (F-0107): internal/local hosts and private IPs
// are always ignored; a configured ignore substring matches; real external URLs
// are not ignored.
func TestIgnoredSourceURL(t *testing.T) {
	internal := []string{
		"https://svc.local", "http://localhost:9000", "http://10.0.0.1/x",
		"http://192.168.1.5", "http://172.16.0.1", "http://172.31.255.1", "http://127.0.0.1:8080",
	}
	for _, u := range internal {
		if !ignoredSourceURL(u, nil) {
			t.Errorf("ignoredSourceURL(%q, nil) = false, want true (internal/local)", u)
		}
	}
	external := []string{"https://ok.example.com", "http://172.32.0.1", "https://agentskills.io/spec"}
	for _, u := range external {
		if ignoredSourceURL(u, nil) {
			t.Errorf("ignoredSourceURL(%q, nil) = true, want false (external)", u)
		}
	}
	ignore := []string{"gitlab.com/rockt/hub"}
	if !ignoredSourceURL("https://gitlab.com/rockt/hub/-/blob/abc/x.go", ignore) {
		t.Errorf("ignore-list substring should match")
	}
	if ignoredSourceURL("https://gitlab.com/other/repo", ignore) {
		t.Errorf("unrelated URL should not match ignore-list")
	}
}

// TestUnregisteredSource_InRuleOrderAndRank pins C5: the rule participates in the
// deterministic ordering.
func TestUnregisteredSource_InRuleOrderAndRank(t *testing.T) {
	found := false
	for _, r := range RuleOrder {
		if r == RuleUnregisteredSource {
			found = true
		}
	}
	if !found {
		t.Fatalf("RuleUnregisteredSource ausente de RuleOrder")
	}
	if _, ok := ruleRank[RuleUnregisteredSource]; !ok {
		t.Fatalf("RuleUnregisteredSource ausente de ruleRank")
	}
}

// TestInUnregisteredSourceScope pins C1 (F-0107): only decisions and feature
// spec/plan/result are in scope; runbooks, references, notes and errata are not.
func TestInUnregisteredSourceScope(t *testing.T) {
	in := []string{
		"content/features/F-0001/spec.md", "content/features/F-0001/plan.md",
		"content/features/F-0001/result.md", "content/decisions/D-0001.md",
	}
	for _, r := range in {
		if !inUnregisteredSourceScope(r) {
			t.Errorf("%q should be in scope", r)
		}
	}
	out := []string{
		"content/runbooks/RB-0001.md", "content/references/R-0001.md",
		"content/notes/N-0001.md", "content/errata/F-0001.md",
	}
	for _, r := range out {
		if inUnregisteredSourceScope(r) {
			t.Errorf("%q should be out of scope", r)
		}
	}
}

// TestUnregisteredSourceFindings_ignored pins C2/C3 at the rule level: internal
// hosts and configured-ignore URLs are dropped, a real external URL is still flagged.
func TestUnregisteredSourceFindings_ignored(t *testing.T) {
	doc := scannedDoc{entity: "D-0001", rel: "content/decisions/D-0001.md"}
	if got := unregisteredSourceFindings(doc, "veja https://auth.rockt.local e http://localhost:9000", nil, nil); len(got) != 0 {
		t.Fatalf("C2: internal URLs should be ignored, got %+v", got)
	}
	if got := unregisteredSourceFindings(doc, "veja https://gitlab.com/rockt/hub/-/blob/x", nil, []string{"gitlab.com/rockt/hub"}); len(got) != 0 {
		t.Fatalf("C3: ignore-list URL should be ignored, got %+v", got)
	}
	if got := unregisteredSourceFindings(doc, "veja https://real.example.com/spec", nil, nil); len(got) != 1 {
		t.Fatalf("C4: real external URL should be flagged, got %+v", got)
	}
}
