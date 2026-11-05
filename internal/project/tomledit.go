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

// editTOML sets key in [table] of raw, touching only that line: it replaces
// the value token in place, inserts the key right after an existing header,
// or appends the table at the end. Comments, order, indentation and line
// endings survive. The result is verified to decode to raw's document with
// only that key changed; any other outcome is ErrConfigLayout (F-0106's
// splice-with-safety-net, applied to TOML).
//
// ponytail: line editor for "[table] key = scalar"; dotted/inline/multi-line layouts are refused by the safety net, not parsed — move to a CST TOML lib if users hit it.
func editTOML(raw []byte, table, key string, value any) ([]byte, error) {
	want := map[string]any{}
	if err := toml.Unmarshal(raw, &want); err != nil {
		return nil, err
	}
	layoutErr := fmt.Errorf("%w: cannot safely set [%s] %s; edit the file by hand", ErrConfigLayout, table, key)
	tbl, ok := want[table].(map[string]any)
	if !ok {
		if _, exists := want[table]; exists {
			return nil, layoutErr
		}
		tbl = map[string]any{}
		want[table] = tbl
	}
	tbl[key] = value
	var lit string
	switch v := value.(type) {
	case bool:
		lit = strconv.FormatBool(v)
	case string:
		lit = strconv.Quote(v)
	default:
		return nil, fmt.Errorf("editTOML: unsupported value type %T", value)
	}

	text := string(raw)
	nl := "\n"
	if strings.Contains(text, "\r\n") {
		nl = "\r\n"
	}
	lines := strings.SplitAfter(text, "\n")
	header := regexp.MustCompile(`^\[` + regexp.QuoteMeta(table) + `\]\s*(#.*)?$`)
	keyLine := regexp.MustCompile(`^(\s*` + regexp.QuoteMeta(key) + `\s*=\s*)("(?:[^"\\]|\\.)*"|'[^']*'|[^\s#]+)(.*)$`)
	h := slices.IndexFunc(lines, func(l string) bool { return header.MatchString(strings.TrimSpace(l)) })
	if h < 0 {
		if text != "" {
			if !strings.HasSuffix(text, "\n") {
				text += nl
			}
			text += nl
		}
		text += "[" + table + "]" + nl + key + " = " + lit + nl
	} else {
		text = editSection(lines, h, keyLine, key+" = "+lit, lit, nl)
	}

	got := map[string]any{}
	if err := toml.Unmarshal([]byte(text), &got); err != nil || !reflect.DeepEqual(got, want) {
		return nil, layoutErr
	}
	return []byte(text), nil
}

// editSection replaces the value token of the key line in the section opened
// by lines[h], or inserts line right after the header when the key is absent.
func editSection(lines []string, h int, keyLine *regexp.Regexp, line, lit, nl string) string {
	for i := h + 1; i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), "["); i++ {
		body := strings.TrimRight(lines[i], "\r\n")
		if m := keyLine.FindStringSubmatch(body); m != nil {
			lines[i] = m[1] + lit + m[3] + lines[i][len(body):]
			return strings.Join(lines, "")
		}
	}
	if !strings.HasSuffix(lines[h], "\n") {
		lines[h] += nl
	}
	return strings.Join(slices.Insert(lines, h+1, line+nl), "")
}
