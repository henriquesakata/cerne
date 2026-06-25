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
