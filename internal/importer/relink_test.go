package importer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testIndex builds an index over a representative slice of the hub migration:
// two features, two ADRs (decisions) and one architecture reference.
func testIndex() *LinkIndex {
	cands := []Candidate{
		{Kind: "feature", Source: "docs/process/cases/0053_oidc/", Title: "OIDC Logout",
			Files: map[string]string{"spec.md": "x", "result.md": "x"}},
		{Kind: "feature", Source: "docs/process/cases/0010_catalog/", Title: "App Catalog",
			Files: map[string]string{"spec.md": "x"}},
		{Kind: "decision", Source: "docs/architecture/adr/adr-008-platform-paas-strategy.md", Title: "ADR-008: PaaS",
			Files: map[string]string{"doc.md": "x"}},
		{Kind: "decision", Source: "docs/architecture/adr/adr-012-privacy.md", Title: "ADR-012: Privacy",
			Files: map[string]string{"doc.md": "x"}},
		{Kind: "reference", Source: "docs/architecture/platform-knowledge-api.md", Title: "Platform Knowledge API",
			Files: map[string]string{"doc.md": "x"}},
	}
	ids := []string{"F-0053", "F-0010", "D-0008", "D-0012", "R-0011"}
	idx := NewLinkIndex()
	for i, c := range cands {
		idx.Add(c, ids[i])
	}
	return idx
}

func rewriteString(body, fromSource, fromContentRel string, idx *LinkIndex, pb PermalinkBase) string {
	return string(rewriteLinks([]byte(body), fromSource, fromContentRel, idx, pb))
}

func TestRewriteWikilinks(t *testing.T) {
	idx := testIndex()
	from := "docs/architecture/adr/adr-019-manifest.md"
	rel := "content/decisions/D-0019.md"
	cases := []struct{ in, want string }{
		{"[[adr-008-platform-paas-strategy]]", "[ADR-008: PaaS](D-0008.md)"},
		{"[[../../process/cases/0053_oidc/spec]]", "[OIDC Logout](../features/F-0053/spec.md)"},
		{"[[adr-012-privacy|privacy tier]]", "[privacy tier](D-0012.md)"},
		{"[[../platform-knowledge-api]]", "[Platform Knowledge API](../references/R-0011.md)"},
		{"[[adr-008-platform-paas-strategy#motivação]]", "[ADR-008: PaaS](D-0008.md#motivação)"},
		// A bare case-folder reference resolves to the feature's spec.
		{"[[../../process/cases/0010_catalog]]", "[App Catalog](../features/F-0010/spec.md)"},
		// Unknown target is left untouched.
		{"[[does-not-exist]]", "[[does-not-exist]]"},
		// Same-page anchor is left untouched.
		{"[[#seção]]", "[[#seção]]"},
	}
	for _, c := range cases {
		got := rewriteString(c.in, from, rel, idx, PermalinkBase{})
		if got != c.want {
			t.Errorf("rewrite(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRewriteInlineLinks(t *testing.T) {
	idx := testIndex()
	from := "docs/process/cases/0029_discovery/"
	rel := "content/features/F-0029/spec.md"
	cases := []struct{ in, want string }{
		{"[ADR-008](../../../architecture/adr/adr-008-platform-paas-strategy.md)",
			"[ADR-008](../../decisions/D-0008.md)"},
		{"see [the case](../0053_oidc/spec.md)", "see [the case](../F-0053/spec.md)"},
		{"[anchor](../0053_oidc/spec.md#go-live)", "[anchor](../F-0053/spec.md#go-live)"},
		{"[ext](https://example.com)", "[ext](https://example.com)"},
		{"[missing](../nope/spec.md)", "[missing](../nope/spec.md)"},
	}
	for _, c := range cases {
		got := rewriteString(c.in, from, rel, idx, PermalinkBase{})
		if got != c.want {
			t.Errorf("rewrite(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRewriteCodePermalink(t *testing.T) {
	srcRoot := t.TempDir()
	javaRel := "apps/identity/api/src/main/java/Foo.java"
	p := filepath.Join(srcRoot, filepath.FromSlash(javaRel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("class Foo {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	pb := PermalinkBase{BlobBase: "https://gitlab.com/rockt/hub/-/blob/abc123", SrcRoot: srcRoot}
	idx := testIndex()
	from := "docs/architecture/adr/adr-008-platform-paas-strategy.md"
	rel := "content/decisions/D-0008.md"

	got := rewriteString("[Foo]("+javaRel+":80)", from, rel, idx, pb)
	want := "[Foo](https://gitlab.com/rockt/hub/-/blob/abc123/apps/identity/api/src/main/java/Foo.java#L80)"
	if got != want {
		t.Errorf("code permalink = %q, want %q", got, want)
	}
	// A code path that does not exist in the source tree is left untouched.
	got = rewriteString("[Gone](apps/identity/Gone.java)", from, rel, idx, pb)
	if got != "[Gone](apps/identity/Gone.java)" {
		t.Errorf("missing code ref = %q, want unchanged", got)
	}
	// With no BlobBase, code refs are never rewritten.
	got = rewriteString("[Foo]("+javaRel+":80)", from, rel, idx, PermalinkBase{})
	if got != "[Foo]("+javaRel+":80)" {
		t.Errorf("disabled permalink = %q, want unchanged", got)
	}
}

func TestRewriteSkipsFencedBlocks(t *testing.T) {
	idx := testIndex()
	body := "ver [[adr-008-platform-paas-strategy]]\n\n```md\n[[adr-008-platform-paas-strategy]]\n```\n"
	got := rewriteString(body, "docs/architecture/adr/adr-019-x.md", "content/decisions/D-0019.md", idx, PermalinkBase{})
	if !strings.Contains(got, "ver [ADR-008: PaaS](D-0008.md)") {
		t.Errorf("prose wikilink not rewritten:\n%s", got)
	}
	if !strings.Contains(got, "```md\n[[adr-008-platform-paas-strategy]]\n```") {
		t.Errorf("fenced wikilink should stay literal:\n%s", got)
	}
}

func TestBlobBase(t *testing.T) {
	cases := []struct {
		remote, sha, want string
	}{
		{"git@gitlab.com:rockt/hub.git", "abc123", "https://gitlab.com/rockt/hub/-/blob/abc123"},
		{"https://gitlab.com/rockt/hub.git", "abc123", "https://gitlab.com/rockt/hub/-/blob/abc123"},
		{"git@github.com:owner/repo.git", "deadbee", "https://github.com/owner/repo/blob/deadbee"},
		{"https://github.com/owner/repo", "deadbee", "https://github.com/owner/repo/blob/deadbee"},
		{"ssh://git@gitlab.example.com:2222/grp/proj.git", "f00", "https://gitlab.example.com/grp/proj/-/blob/f00"},
		{"git@bitbucket.org:o/r.git", "abc", ""}, // unrecognised host
		{"git@gitlab.com:rockt/hub.git", "", ""}, // no sha
		{"", "abc", ""},                          // no remote
	}
	for _, c := range cases {
		if got := BlobBase(c.remote, c.sha); got != c.want {
			t.Errorf("BlobBase(%q, %q) = %q, want %q", c.remote, c.sha, got, c.want)
		}
	}
}

func TestRewritePreservesCRLF(t *testing.T) {
	idx := testIndex()
	body := "linha 1\r\nver [[adr-008-platform-paas-strategy]]\r\nfim\r\n"
	got := rewriteString(body, "docs/architecture/adr/adr-019-x.md", "content/decisions/D-0019.md", idx, PermalinkBase{})
	if !strings.Contains(got, "\r\n") {
		t.Errorf("CRLF not preserved:\n%q", got)
	}
	if strings.Contains(strings.ReplaceAll(got, "\r\n", ""), "\n") {
		t.Errorf("introduced lone LF:\n%q", got)
	}
	if !strings.Contains(got, "ver [ADR-008: PaaS](D-0008.md)\r\n") {
		t.Errorf("rewrite under CRLF wrong:\n%q", got)
	}
}
