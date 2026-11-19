package project

import (
	"errors"
	"reflect"
	"testing"

	"github.com/BurntSushi/toml"
)

var unitRecords = Setting{ID: "x.recs", Type: "records", Fields: []Setting{{ID: "name"}, {ID: "kind"}, {ID: "url", MaxLen: 256}}}

// TestCheckRecordsNormalizes: a JSON list and a TOML array of tables read the
// same, and an empty value is dropped, as the writer drops it.
func TestCheckRecordsNormalizes(t *testing.T) {
	want := []map[string]string{{"name": "a", "kind": "k"}}
	for _, in := range []any{
		[]any{map[string]any{"name": "a", "kind": "k", "url": ""}},
		[]map[string]any{{"kind": "k", "name": "a"}},
	} {
		got, err := checkRecords(unitRecords, in)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("checkRecords(%#v) = %#v, %v; want %#v", in, got, err, want)
		}
	}
	if got, err := checkRecords(unitRecords, []any{}); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("checkRecords([]) = %#v, %v; want an empty list", got, err)
	}
}

// TestInlineRecordsLiteralDecodes: the literal is one line in Fields order,
// and TOML reads it back as decoded(), which the safety net compares against.
func TestInlineRecordsLiteralDecodes(t *testing.T) {
	for _, tc := range []struct {
		rows []map[string]string
		lit  string
	}{
		{nil, `[]`},
		{[]map[string]string{{"url": `https://h/o/r`, "name": `say "hi" \ é`}, {"name": "b", "kind": "k"}},
			`[{name = "say \"hi\" \\ é", url = "https://h/o/r"}, {name = "b", kind = "k"}]`},
	} {
		r := inlineRecords{fields: []string{"name", "kind", "url"}, rows: tc.rows}
		if got := r.literal(); got != tc.lit {
			t.Fatalf("literal() = %s, want %s", got, tc.lit)
		}
		doc := map[string]any{}
		if err := toml.Unmarshal([]byte("k = "+r.literal()), &doc); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(doc["k"], r.decoded()) {
			t.Fatalf("TOML reads %#v, decoded() says %#v", doc["k"], r.decoded())
		}
	}
}

// F-0449: a control character in a records value is refused before
// strconv.Quote turns it into an escape the TOML parser rejects.
func TestCheckRecordsRefusesControlCharacters(t *testing.T) {
	for _, v := range []string{"a\x01b", "tab\there", "del\x7f"} {
		_, err := checkRecords(unitRecords, []any{map[string]any{"name": "gh", "url": v}})
		if !errors.Is(err, ErrInvalidSettingValue) {
			t.Errorf("checkRecords(url %q) = %v, want ErrInvalidSettingValue", v, err)
		}
	}
}
