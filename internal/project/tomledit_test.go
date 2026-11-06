package project

import (
	"errors"
	"strings"
	"testing"
)

// editFixture mirrors the shape of a real .cernerc: root keys, comments,
// tables out of order, a [component_paths] wiki key and an indented key with
// an end-of-line comment.
const editFixture = `project_id = "p"
backend = "b"
# top comment
compile_on_push = true

# drift axis
[component_paths]
wiki = ["internal/wiki/**"]
lint = ["internal/lint/**"]

[llm]
mode = "delegation" # x

[mindmap]
    enabled = true   # keep me

[delivery]
commit_url = "https://x/{sha}"
`

func crlf(s string) string { return strings.ReplaceAll(s, "\n", "\r\n") }

// TestEditTOML pins the minimal-diff editor behind F-0314 criterion 4: every
// line but the edited one comes out byte-identical, and a layout the line
// editor cannot rewrite safely is refused, never re-rendered.
func TestEditTOML(t *testing.T) {
	insertedLLM := strings.Replace(editFixture, "[llm]\n", "[llm]\nprovider = \"anthropic\"\n", 1)
	noEOL := strings.TrimSuffix(editFixture, "\n")
	cases := []struct {
		name        string
		in          string
		table, key  string
		value       any
		want        string
		wantErrorIs error
	}{
		{"a replace keeps indent and comment", editFixture, "mindmap", "enabled", false,
			strings.Replace(editFixture, "    enabled = true   # keep me", "    enabled = false   # keep me", 1), nil},
		{"b absent key goes right after the header", editFixture, "llm", "provider", "anthropic", insertedLLM, nil},
		{"c absent table is appended", editFixture, "graph", "enabled", false,
			editFixture + "\n[graph]\nenabled = false\n", nil},
		{"c absent table, no final newline", noEOL, "graph", "enabled", false,
			noEOL + "\n\n[graph]\nenabled = false\n", nil},
		{"d wiki.enabled leaves component_paths.wiki alone", editFixture, "wiki", "enabled", false,
			editFixture + "\n[wiki]\nenabled = false\n", nil},
		{"e CRLF insert", crlf(editFixture), "llm", "provider", "anthropic", crlf(insertedLLM), nil},
		{"e CRLF append", crlf(editFixture), "graph", "enabled", false,
			crlf(editFixture + "\n[graph]\nenabled = false\n"), nil},
		{"f string value", editFixture, "llm", "mode", "claude-cli",
			strings.Replace(editFixture, `mode = "delegation" # x`, `mode = "claude-cli" # x`, 1), nil},
		{"g empty input", "", "mindmap", "enabled", false, "[mindmap]\nenabled = false\n", nil},
		{"h dotted root key", baseCernerc + "mindmap.enabled = true\n", "mindmap", "enabled", false, "", ErrConfigLayout},
		{"h spaced header", baseCernerc + "[ mindmap ]\nenabled = true\n", "mindmap", "enabled", false, "", ErrConfigLayout},
		{"h inline table", baseCernerc + "mindmap = { enabled = true }\n", "mindmap", "enabled", false, "", ErrConfigLayout},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := editTOML([]byte(tc.in), tc.table, tc.key, tc.value)
			if tc.wantErrorIs != nil {
				if !errors.Is(err, tc.wantErrorIs) {
					t.Fatalf("err = %v, want %v", err, tc.wantErrorIs)
				}
				return
			}
			if err != nil {
				t.Fatalf("editTOML: %v", err)
			}
			if string(got) != tc.want {
				removed, added := changedLines(tc.want, string(got))
				t.Errorf("output differs from want: want-only %q, got-only %q", removed, added)
			}
		})
	}
}

// changedLines reports the lines of a and of b outside their common line
// prefix and suffix: the hunk a minimal edit leaves.
func changedLines(a, b string) (onlyA, onlyB []string) {
	la, lb := strings.SplitAfter(a, "\n"), strings.SplitAfter(b, "\n")
	p := 0
	for p < len(la) && p < len(lb) && la[p] == lb[p] {
		p++
	}
	s := 0
	for s < len(la)-p && s < len(lb)-p && la[len(la)-1-s] == lb[len(lb)-1-s] {
		s++
	}
	return la[p : len(la)-s], lb[p : len(lb)-s]
}

// TestEditTOMLRemove: a nil value removes the key's line and nothing else
// (the table header stays); an absent key or table is a byte-identical no-op.
func TestEditTOMLRemove(t *testing.T) {
	cases := []struct {
		name, in, table, key, want string
		wantErrorIs                error
	}{
		{"last key of a bare table drops the header and its separator", editFixture, "mindmap", "enabled",
			strings.Replace(editFixture, "\n[mindmap]\n    enabled = true   # keep me\n", "", 1), nil},
		{"bare table at the start of the file", "[wiki]\nenabled = false\n", "wiki", "enabled", "", nil},
		{"other keys keep the header", "[wiki]\nenabled = true\nother = 1\n", "wiki", "enabled", "[wiki]\nother = 1\n", nil},
		{"a comment inside keeps the header", "a = 1\n\n[wiki]\n# why\nenabled = true\n", "wiki", "enabled", "a = 1\n\n[wiki]\n# why\n", nil},
		{"a comment above keeps the header", "a = 1\n\n# toggle\n[wiki]\nenabled = true\n", "wiki", "enabled", "a = 1\n\n# toggle\n[wiki]\n", nil},
		{"a header comment keeps the header", "a = 1\n\n[wiki] # note\nenabled = true\n", "wiki", "enabled", "a = 1\n\n[wiki] # note\n", nil},
		{"bare table between tables", "[a]\nx = 1\n\n[wiki]\nenabled = true\n\n[b]\ny = 2\n", "wiki", "enabled", "[a]\nx = 1\n\n[b]\ny = 2\n", nil},
		{"CRLF", crlf(editFixture), "component_paths", "lint", crlf(strings.Replace(editFixture, "lint = [\"internal/lint/**\"]\n", "", 1)), nil},
		{"absent key", editFixture, "llm", "provider", editFixture, nil},
		{"absent table", editFixture, "graph", "enabled", editFixture, nil},
		{"empty input", "", "graph", "enabled", "", nil},
		{"dotted root key", baseCernerc + "mindmap.enabled = true\n", "mindmap", "enabled", "", ErrConfigLayout},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := editTOML([]byte(tc.in), tc.table, tc.key, nil)
			if tc.wantErrorIs != nil {
				if !errors.Is(err, tc.wantErrorIs) {
					t.Fatalf("err = %v, want %v", err, tc.wantErrorIs)
				}
				return
			}
			if err != nil {
				t.Fatalf("editTOML: %v", err)
			}
			if string(got) != tc.want {
				removed, added := changedLines(tc.want, string(got))
				t.Errorf("output differs from want: want-only %q, got-only %q", removed, added)
			}
		})
	}
}
