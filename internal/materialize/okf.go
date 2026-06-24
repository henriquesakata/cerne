package materialize

import (
	"bytes"
	"strings"
	"time"

	"gitlab.com/rockt/cerne/internal/doccheck"
)

// okfMeta carries the structured fields projected into a concept's Open
// Knowledge Format (OKF) frontmatter. `type` is the only field OKF requires;
// title/timestamp/resource/tags are OKF reserved fields; state/parent/
// external/informs are cerne-specific extras that an OKF-only consumer simply
// ignores. Empty string / nil / false fields are omitted from the block.
type okfMeta struct {
	Type      string   // required: feature|spec|plan|result|decision|reference|note|runbook
	Title     string   // OKF reserved
	Timestamp string   // OKF reserved (RFC3339); use okfTime to format
	Resource  string   // OKF reserved (e.g. a reference URL)
	Tags      []string // OKF reserved
	State     string   // extra
	Parent    string   // extra (epic)
	External  bool     // extra
	Informs   []string // extra (F-/D- ids)
}

// okfTime formats an entity timestamp as the RFC3339 instant OKF's `timestamp`
// field expects (e.g. 2026-05-28T14:30:00Z). The zero time yields "" so an
// unset timestamp is omitted rather than emitted as the Go zero value.
func okfTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// okfFrontmatter rewrites body so it carries an OKF-valid frontmatter block: it
// drops any existing leading "---" block (preserving its cerne_id), re-emits the
// merged fields in a fixed key order, then appends the original body. The fixed
// order keeps the output — and thus the materialize manifest hash — deterministic.
func okfFrontmatter(meta okfMeta, body []byte) []byte {
	cerneID := doccheck.FrontmatterValue(body, "cerne_id")
	rest := stripFrontmatter(body)

	var b strings.Builder
	b.WriteString("---\n")
	writeKV(&b, "type", meta.Type)
	writeKV(&b, "title", meta.Title)
	writeKV(&b, "timestamp", meta.Timestamp)
	writeKV(&b, "resource", meta.Resource)
	writeList(&b, "tags", meta.Tags)
	writeKV(&b, "state", meta.State)
	writeKV(&b, "parent", meta.Parent)
	if meta.External {
		b.WriteString("external: true\n")
	}
	writeList(&b, "informs", meta.Informs)
	writeKV(&b, "cerne_id", cerneID)
	b.WriteString("---\n")
	if len(rest) > 0 {
		b.WriteString("\n")
		b.Write(rest)
	}
	return []byte(b.String())
}

// stripFrontmatter returns body with a leading "---"-delimited frontmatter block
// removed (plus the blank lines right after it), or body unchanged (BOM-trimmed)
// when there is none. It mirrors doccheck's block skipping so authored
// frontmatter is never emitted twice. An unterminated block is left as-is.
func stripFrontmatter(data []byte) []byte {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	lines := strings.SplitAfter(string(data), "\n") // keep the newlines for exact rejoin
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return data
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			rest := strings.Join(lines[i+1:], "")
			return []byte(strings.TrimLeft(rest, "\n"))
		}
	}
	return data
}

// writeKV emits "key: value\n" (value YAML-escaped), or nothing when value is "".
func writeKV(b *strings.Builder, key, val string) {
	if val == "" {
		return
	}
	b.WriteString(key)
	b.WriteString(": ")
	b.WriteString(yamlScalar(val))
	b.WriteString("\n")
}

// writeList emits "key: [a, b]\n" (each item YAML-escaped), or nothing when empty.
func writeList(b *strings.Builder, key string, vals []string) {
	if len(vals) == 0 {
		return
	}
	b.WriteString(key)
	b.WriteString(": [")
	for i, v := range vals {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(yamlScalar(v))
	}
	b.WriteString("]\n")
}

// yamlScalar renders s as a YAML scalar: bare when it is a safe plain string,
// double-quoted (with \ and " escaped) otherwise — so titles with ":", accents,
// or other YAML-significant characters stay valid.
func yamlScalar(s string) string {
	if plainScalar(s) {
		return s
	}
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + r.Replace(s) + `"`
}

// plainScalar reports whether s is safe to emit unquoted: non-empty, no leading/
// trailing space, and only [A-Za-z0-9 _./-]. Anything else (":", "#", "[", quotes,
// accented runes, …) forces quoting.
func plainScalar(s string) bool {
	if s == "" || strings.TrimSpace(s) != s {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == ' ' || r == '_' || r == '.' || r == '/' || r == '-':
		default:
			return false
		}
	}
	return true
}
