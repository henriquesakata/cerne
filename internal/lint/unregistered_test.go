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
	got := unregisteredSourceFindings(doc, textWith, nil)
	if len(got) != 1 {
		t.Fatalf("C1: got %d findings, want 1: %+v", len(got), got)
	}
	if got[0].Rule != RuleUnregisteredSource || got[0].Entity != "F-0001" || got[0].File != doc.rel {
		t.Fatalf("C1: finding shape wrong: %+v", got[0])
	}

	// C2a: referência atual com a mesma URL (barra final difere) → 0 (URL-match normalizada).
	refURL := []store.Reference{current(store.Reference{ID: "R-1", URL: url + "/", Informs: []string{"F-0999"}})}
	if got := unregisteredSourceFindings(doc, textWith, refURL); len(got) != 0 {
		t.Fatalf("C2a: got %d findings, want 0: %+v", len(got), got)
	}

	// C2b: referência atual que informa a entidade do doc (sem casar URL) → 0 (Informs-match).
	refInforms := []store.Reference{current(store.Reference{ID: "R-2", URL: "https://outra.fonte/x", Informs: []string{"F-0001"}})}
	if got := unregisteredSourceFindings(doc, textWith, refInforms); len(got) != 0 {
		t.Fatalf("C2b: got %d findings, want 0: %+v", len(got), got)
	}

	// C3: URL apenas dentro de bloco de código → 0.
	if got := unregisteredSourceFindings(doc, textFenced, nil); len(got) != 0 {
		t.Fatalf("C3: got %d findings, want 0: %+v", len(got), got)
	}

	// C6: URL removida da prosa → 0.
	if got := unregisteredSourceFindings(doc, textNone, nil); len(got) != 0 {
		t.Fatalf("C6: got %d findings, want 0: %+v", len(got), got)
	}

	// Referência apenas OBSOLETA com a URL não respalda (exige atual) → 1 finding.
	refObsolete := []store.Reference{{ID: "R-3", URL: url, State: domain.ReferenceObsolete}}
	if got := unregisteredSourceFindings(doc, textWith, refObsolete); len(got) != 1 {
		t.Fatalf("obsolete: got %d findings, want 1: %+v", len(got), got)
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
