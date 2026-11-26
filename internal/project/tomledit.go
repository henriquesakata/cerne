package project

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// ErrConfigLayout: the file's TOML layout is outside what the line editor
// rewrites safely. The edit is refused and the file left alone; the user
// edits it by hand.
var ErrConfigLayout = errors.New("unsupported config layout")

// editTOML sets key in [table] of raw, or removes it when value is nil,
// touching only that line: it replaces the value token in place, inserts the
// key right after an existing header, appends the table at the end, or drops
// the key's line. Removing the last key of a bare table (no other key, no
// comment in it, above it or on its header) drops the header and the blank
// line before it too, so set then unset gives the file back byte for byte, as
// git config --unset does. Comments, order, indentation and line
// endings survive. The result is verified to decode to raw's document with
// only that key changed; any other outcome is ErrConfigLayout (F-0106's
// splice-with-safety-net, applied to TOML).
//
// ponytail: line editor for "[table] key = scalar"; dotted/inline/multi-line layouts are refused by the safety net, not parsed — move to a CST TOML lib if users hit it.
// ponytail: a records value is one inline array on one line, matched as \[[^#]*\]; a multi-line array, or a "#" inside one (a URL fragment), falls to the safety net as ErrConfigLayout — same CST upgrade path.
func editTOML(raw []byte, table, key string, value any) ([]byte, error) {
	want := map[string]any{}
	if err := toml.Unmarshal(raw, &want); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrConfigParse, err)
	}
	layoutErr := fmt.Errorf("%w: cannot safely edit [%s] %s; edit the file by hand", ErrConfigLayout, table, key)
	tbl, ok := want[table].(map[string]any)
	if !ok {
		if _, exists := want[table]; exists {
			return nil, layoutErr
		}
		tbl = map[string]any{}
		want[table] = tbl
	}
	var lit string
	stored := value
	switch v := value.(type) {
	case nil:
		if _, has := tbl[key]; !has {
			return raw, nil
		}
		delete(tbl, key)
	case bool:
		lit = strconv.FormatBool(v)
	case string:
		lit = strconv.Quote(v)
	case int64:
		lit = strconv.FormatInt(v, 10)
	case inlineRecords:
		lit, stored = v.literal(), v.decoded()
	case []string:
		quoted, decoded := make([]string, len(v)), make([]any, len(v))
		for i, x := range v {
			quoted[i], decoded[i] = strconv.Quote(x), x
		}
		lit, stored = "["+strings.Join(quoted, ", ")+"]", decoded
	default:
		return nil, fmt.Errorf("editTOML: unsupported value type %T", value)
	}
	if value != nil {
		tbl[key] = stored
	}

	text := string(raw)
	nl := "\n"
	if strings.Contains(text, "\r\n") {
		nl = "\r\n"
	}
	lines := strings.SplitAfter(text, "\n")
	header := regexp.MustCompile(`^\[` + regexp.QuoteMeta(table) + `\]\s*(#.*)?$`)
	keyLine := regexp.MustCompile(`^(\s*` + regexp.QuoteMeta(key) + `\s*=\s*)("(?:[^"\\]|\\.)*"|'[^']*'|\[[^#]*\]|[^\s#]+)(.*)$`)
	h := slices.IndexFunc(lines, func(l string) bool { return header.MatchString(strings.TrimSpace(l)) })
	i := keyLineIndex(lines, h, keyLine)
	switch {
	case value == nil && i >= 0:
		lines = slices.Delete(lines, i, i+1)
		if len(tbl) == 0 && bareSection(lines, h, table) {
			// Undo what set's append wrote: the header and its blank separator.
			from := h
			if h > 0 && strings.TrimSpace(lines[h-1]) == "" {
				from = h - 1
			}
			lines = slices.Delete(lines, from, h+1)
			delete(want, table)
		}
	case i >= 0:
		body := strings.TrimRight(lines[i], "\r\n")
		m := keyLine.FindStringSubmatch(body)
		lines[i] = m[1] + lit + m[3] + lines[i][len(body):]
	case value != nil && h >= 0:
		if !strings.HasSuffix(lines[h], "\n") {
			lines[h] += nl
		}
		lines = slices.Insert(lines, h+1, key+" = "+lit+nl)
	case value != nil:
		if text != "" {
			if !strings.HasSuffix(text, "\n") {
				text += nl
			}
			text += nl
		}
		lines = []string{text, "[" + table + "]" + nl, key + " = " + lit + nl}
	}
	text = strings.Join(lines, "")

	got := map[string]any{}
	if err := toml.Unmarshal([]byte(text), &got); err != nil || !reflect.DeepEqual(got, want) {
		return nil, layoutErr
	}
	return []byte(text), nil
}

// bareSection reports whether the section opened by lines[h] is a plain
// "[table]" header, with no comment on or right above it, and holds only blank
// lines.
func bareSection(lines []string, h int, table string) bool {
	if strings.TrimSpace(lines[h]) != "["+table+"]" || h > 0 && strings.HasPrefix(strings.TrimSpace(lines[h-1]), "#") {
		return false
	}
	for j := h + 1; j < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[j]), "["); j++ {
		if strings.TrimSpace(lines[j]) != "" {
			return false
		}
	}
	return true
}

// keyLineIndex returns the index of key's line in the section opened by
// lines[h] (which ends at the next header), or -1.
func keyLineIndex(lines []string, h int, keyLine *regexp.Regexp) int {
	if h < 0 {
		return -1
	}
	for i := h + 1; i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), "["); i++ {
		if keyLine.MatchString(strings.TrimRight(lines[i], "\r\n")) {
			return i
		}
	}
	return -1
}

// inlineRecords is a records value as editTOML writes it: one inline array of
// inline tables, each table's keys in fields order, the empty values left out.
type inlineRecords struct {
	fields []string
	rows   []map[string]string
}

func (r inlineRecords) literal() string {
	tables := make([]string, len(r.rows))
	for i, row := range r.rows {
		var kv []string
		for _, f := range r.fields {
			if row[f] != "" {
				kv = append(kv, f+" = "+strconv.Quote(row[f]))
			}
		}
		tables[i] = "{" + strings.Join(kv, ", ") + "}"
	}
	return "[" + strings.Join(tables, ", ") + "]"
}

// decoded is the literal as the TOML decoder reads it back.
func (r inlineRecords) decoded() any {
	out := make([]any, len(r.rows))
	for i, row := range r.rows {
		m := map[string]any{}
		for f, v := range row {
			if v != "" {
				m[f] = v
			}
		}
		out[i] = m
	}
	return out
}
