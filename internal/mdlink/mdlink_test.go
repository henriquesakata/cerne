package mdlink

import (
	"reflect"
	"testing"
)

// TestContentLinks: parses distinct markdown targets, skips external/anchor/
// absolute, strips #fragments before resolving, dedups by raw target, and
// splits resolved (resolver id) from broken (raw target) in first-appearance
// order.
func TestContentLinks(t *testing.T) {
	content := "see [a](a.md), [dup](a.md), [ext](https://x), [anchor](#h), " +
		"[abs](/x), [bad](missing.md) and [frag](b.md#sec)"
	resolve := func(target string) (string, bool) {
		switch target {
		case "a.md":
			return "F-0001", true
		case "b.md":
			return "D-0002", true
		}
		return "", false
	}

	resolved, broken := ContentLinks(content, resolve)

	if want := []string{"F-0001", "D-0002"}; !reflect.DeepEqual(resolved, want) {
		t.Fatalf("resolved = %v, want %v", resolved, want)
	}
	if want := []string{"missing.md"}; !reflect.DeepEqual(broken, want) {
		t.Fatalf("broken = %v, want %v", broken, want)
	}
}

// TestContentLinksSkipsCode: link-looking syntax inside fenced code blocks and
// inline code spans is not a markdown link — a shell command such as
// [datetime](docker inspect ...) must not be reported as broken, while real
// links outside code still resolve or break.
func TestContentLinksSkipsCode(t *testing.T) {
	content := "Top [bad](missing.md).\n" +
		"```bash\n" +
		"docker inspect --format '[datetime](docker inspect x)' c\n" +
		"```\n" +
		"Mid `[inl](inline.md)` span and a ``[dbl](double.md)`` one.\n" +
		"End [ok](after.md)."
	resolve := func(target string) (string, bool) {
		if target == "after.md" {
			return "F-0009", true
		}
		return "", false
	}

	resolved, broken := ContentLinks(content, resolve)

	if want := []string{"F-0009"}; !reflect.DeepEqual(resolved, want) {
		t.Fatalf("resolved = %v, want %v", resolved, want)
	}
	if want := []string{"missing.md"}; !reflect.DeepEqual(broken, want) {
		t.Fatalf("broken = %v, want %v", broken, want)
	}
}

// TestWikiLinks (F-0167): extracts id-form wiki-links only — [[F-0001]],
// case-insensitive, alias form [[id|text]] — deduped by raw target in
// first-appearance order; [[anything else]] (titles, TOML [[table]] prose) is
// no wiki-link at all, and code fences/inline spans are excluded like inline
// links.
func TestWikiLinks(t *testing.T) {
	content := "cite [[F-0001]], [[f-0001]] again, [[N-0002|a note]], " +
		"[[X-9999]] missing, [[algum título]] and [[historical]] ignored"
	resolve := func(target string) (string, bool) {
		switch target {
		case "F-0001", "f-0001":
			return "F-0001", true
		case "N-0002":
			return "N-0002", true
		}
		return "", false
	}

	resolved, broken := WikiLinks(content, resolve)

	if want := []string{"F-0001", "F-0001", "N-0002"}; !reflect.DeepEqual(resolved, want) {
		t.Fatalf("resolved = %v, want %v", resolved, want)
	}
	if want := []string{"X-9999"}; !reflect.DeepEqual(broken, want) {
		t.Fatalf("broken = %v, want %v", broken, want)
	}
}

// TestWikiLinksSkipsCode: wiki-link syntax inside fenced blocks (e.g. TOML
// [[historical]] tables) and inline code spans is never extracted.
func TestWikiLinksSkipsCode(t *testing.T) {
	content := "Top [[X-1111]].\n" +
		"```toml\n" +
		"[[F-0001]]\n" +
		"name = \"x\"\n" +
		"```\n" +
		"Mid `[[F-0002]]` span.\n" +
		"End [[F-0003]]."
	resolve := func(target string) (string, bool) {
		if target == "F-0003" {
			return "F-0003", true
		}
		return "", false
	}

	resolved, broken := WikiLinks(content, resolve)

	if want := []string{"F-0003"}; !reflect.DeepEqual(resolved, want) {
		t.Fatalf("resolved = %v, want %v", resolved, want)
	}
	if want := []string{"X-1111"}; !reflect.DeepEqual(broken, want) {
		t.Fatalf("broken = %v, want %v", broken, want)
	}
}
