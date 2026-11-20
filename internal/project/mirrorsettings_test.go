package project

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/tracker"
)

// ghRecord is one mirror.trackers record on github.com, as TOML or JSON hands it over.
func ghRecord(name, repo string) map[string]any {
	return map[string]any{"name": name, "kind": "github", "url": "https://github.com/o/" + repo}
}

func readRC(t *testing.T, projDir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(projDir, ".cernerc"))
	if err != nil {
		t.Fatalf("read .cernerc: %v", err)
	}
	return string(b)
}

// asRecords normalizes a records value of any shape ([]any, []map[string]any,
// []map[string]string) to []map[string]string, dropping empty values: an
// omitted label and an empty one are the same record.
func asRecords(t *testing.T, v any) []map[string]string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal %v: %v", v, err)
	}
	var recs []map[string]string
	if err := json.Unmarshal(raw, &recs); err != nil {
		t.Fatalf("records value %s is not a list of string records: %v", raw, err)
	}
	for _, r := range recs {
		for k, val := range r {
			if val == "" {
				delete(r, k)
			}
		}
	}
	return recs
}

func storedTrackers(t *testing.T, projDir string) []map[string]string {
	t.Helper()
	return asRecords(t, findSetting(t, ResolveSettings(projDir), "mirror.trackers").Value)
}

// F-0449 crit. 2: mirror.trackers is written as one TOML inline array, in the
// order of the fields and without empty values. A rewrite replaces that line,
// the line's comment survives, unset gives the file back byte for byte, and a
// layout the line editor cannot rewrite is refused with nothing written.
func TestEditTOMLInlineRecords(t *testing.T) {
	set := func(t *testing.T, projDir string, v any) {
		t.Helper()
		if err := SetSettingValue(projDir, "mirror.trackers", ScopeProject, v); err != nil {
			t.Fatalf("set mirror.trackers: %v", err)
		}
	}
	const one = `trackers = [{name = "gh", kind = "github", url = "https://github.com/o/issues"}]`

	t.Run("set writes one line, a rewrite replaces it", func(t *testing.T) {
		projDir, _ := settingsFixture(t, "", "", false)
		set(t, projDir, []any{ghRecord("gh", "issues")})
		if got, want := readRC(t, projDir), baseCernerc+"\n[mirror]\n"+one+"\n"; got != want {
			t.Fatalf(".cernerc =\n%s\nwant\n%s", got, want)
		}
		two := []any{
			map[string]any{"label": "public", "url": "https://github.com/o/a", "kind": "github", "name": "a"},
			ghRecord("b", "b"),
		}
		set(t, projDir, two)
		got := readRC(t, projDir)
		var lines []string
		for _, l := range strings.Split(got, "\n") {
			if strings.HasPrefix(l, "trackers") {
				lines = append(lines, l)
			}
		}
		if len(lines) != 1 || strings.Count(got, "[mirror]") != 1 || !strings.HasPrefix(got, baseCernerc+"\n[mirror]\ntrackers = [") {
			t.Fatalf("rewrite must replace the one trackers line under the one [mirror]:\n%s", got)
		}
		for _, rec := range []string{
			`{name = "a", kind = "github", url = "https://github.com/o/a", label = "public"}`,
			`{name = "b", kind = "github", url = "https://github.com/o/b"}`,
		} {
			if !strings.Contains(lines[0], rec) {
				t.Errorf("line %q lacks %s (fields in declared order, empty values dropped)", lines[0], rec)
			}
		}
		want := []map[string]string{
			{"name": "a", "kind": "github", "url": "https://github.com/o/a", "label": "public"},
			{"name": "b", "kind": "github", "url": "https://github.com/o/b"},
		}
		if got := storedTrackers(t, projDir); !reflect.DeepEqual(got, want) {
			t.Fatalf("read back %v, want %v", got, want)
		}
	})

	t.Run("the line's comment survives", func(t *testing.T) {
		const old = `trackers = [{name = "old", kind = "github", url = "https://github.com/o/old"}]`
		projDir, _ := settingsFixture(t, "\n[mirror]\n"+old+" # public issues\n", "", false)
		before := readRC(t, projDir)
		set(t, projDir, []any{ghRecord("gh", "issues")})
		if got, want := readRC(t, projDir), strings.Replace(before, old, one, 1); got != want {
			t.Fatalf(".cernerc =\n%s\nwant\n%s", got, want)
		}
	})

	t.Run("unset gives the original bytes back", func(t *testing.T) {
		projDir, _ := settingsFixture(t, "\n[delivery]\nforge = \"github\"\n", "", false)
		before := readRC(t, projDir)
		set(t, projDir, []any{ghRecord("gh", "issues")})
		if err := UnsetSetting(projDir, "mirror.trackers", ScopeProject); err != nil {
			t.Fatalf("unset: %v", err)
		}
		if got := readRC(t, projDir); got != before {
			t.Fatalf("after set+unset .cernerc =\n%s\nwant\n%s", got, before)
		}
	})

	for name, layout := range map[string]string{
		"a multi-line array is refused":     "\n[mirror]\ntrackers = [\n  {name = \"a\", kind = \"github\", url = \"https://github.com/o/a\"},\n]\n",
		"an array with # inside is refused": "\n[mirror]\ntrackers = [{name = \"a\", kind = \"github\", url = \"https://github.com/o/a\", label = \"x#y\"}]\n",
	} {
		t.Run(name, func(t *testing.T) {
			projDir, _ := settingsFixture(t, layout, "", false)
			before := readRC(t, projDir)
			err := SetSettingValue(projDir, "mirror.trackers", ScopeProject, []any{ghRecord("gh", "issues")})
			if !errors.Is(err, ErrConfigLayout) {
				t.Fatalf("set over %q = %v, want errors.Is ErrConfigLayout", layout, err)
			}
			if got := readRC(t, projDir); got != before {
				t.Fatalf("a refused edit wrote:\n%s", got)
			}
		})
	}
}

// F-0449 crit. 1: the six integration keys are declared in the integrations
// group, project scope only, with the plan's types, defaults, human-only marks
// and limits; mirror.trackers declares its fields.
func TestSettingsListsIntegrationKeys(t *testing.T) {
	projDir, _ := settingsFixture(t, "", "", false)
	view := ResolveSettings(projDir)
	for _, c := range []struct {
		id, typ string
		def     any
		enum    []string
		human   bool
		max     int
	}{
		{"delivery.forge", "string", "", DeliveryForges, false, 64},
		{"delivery.commit_url", "string", "", nil, false, 256},
		{"delivery.pr_url", "string", "", nil, false, 256},
		{"mirror.enabled", "bool", false, nil, true, 0},
		{"mirror.trackers", "records", nil, nil, true, 0},
		{"mirror.close_on", "string", "accept", []string{"accept", "merge"}, false, 64},
	} {
		s := findSetting(t, view, c.id)
		if s.Group != "integrations" || !slices.Equal(s.Scopes, []string{ScopeProject}) || s.Type != c.typ || s.Title == "" || s.Description == "" {
			t.Errorf("%s = group %q, scopes %v, type %q, title %q; want integrations, [project], %s, a title and a description", c.id, s.Group, s.Scopes, s.Type, s.Title, c.typ)
		}
		if s.HumanOnly != c.human || SettingHumanOnly(c.id) != c.human {
			t.Errorf("%s human-only = %v, want %v", c.id, s.HumanOnly, c.human)
		}
		if !slices.Equal(s.Enum, c.enum) {
			t.Errorf("%s enum = %v, want %v", c.id, s.Enum, c.enum)
		}
		if c.typ == "records" {
			if d := reflect.ValueOf(s.Default); d.Kind() != reflect.Slice || d.Len() != 0 {
				t.Errorf("%s default = %#v, want an empty list", c.id, s.Default)
			}
		} else if s.Default != c.def {
			t.Errorf("%s default = %#v, want %#v", c.id, s.Default, c.def)
		}
		if c.max > 0 && maxLen(s.Setting) != c.max {
			t.Errorf("%s max length = %d, want %d", c.id, maxLen(s.Setting), c.max)
		}
	}

	tr := findSetting(t, view, "mirror.trackers")
	var ids []string
	for _, f := range tr.Fields {
		ids = append(ids, f.ID)
		if f.Type != "string" || f.Title == "" {
			t.Errorf("field %s = type %q, title %q; want a string field with a title", f.ID, f.Type, f.Title)
		}
	}
	if !slices.Equal(ids, []string{"name", "kind", "url", "label"}) {
		t.Fatalf("mirror.trackers fields = %v, want [name kind url label]", ids)
	}
	if !slices.Equal(tr.Fields[1].Enum, tracker.KindNames()) {
		t.Errorf("kind field enum = %v, want the implemented kinds %v", tr.Fields[1].Enum, tracker.KindNames())
	}
	if maxLen(tr.Fields[2]) != 256 {
		t.Errorf("url field max length = %d, want 256", maxLen(tr.Fields[2]))
	}
}

// F-0449 crit. 1: the delivery.* keys read and write the [delivery] block the
// .cernerc already has — edited in place, read the same by LoadConfig, no
// migration and no second table.
func TestDeliverySettingReadsExistingBlock(t *testing.T) {
	const block = "\n[delivery]\ncommit_url = \"https://github.com/o/r/commit/{sha}\" # code repo\nforge = \"github\"\npr_url = \"https://github.com/o/r/pull/{n}\"\n"
	projDir, _ := settingsFixture(t, block, "", false)
	view := ResolveSettings(projDir)
	for id, want := range map[string]string{
		"delivery.commit_url": "https://github.com/o/r/commit/{sha}",
		"delivery.forge":      "github",
		"delivery.pr_url":     "https://github.com/o/r/pull/{n}",
	} {
		if s := findSetting(t, view, id); s.Value != want || s.Origin != ScopeProject {
			t.Errorf("%s = %v from %s, want %q from project", id, s.Value, s.Origin, want)
		}
	}

	before := readRC(t, projDir)
	if err := SetSettingValue(projDir, "delivery.pr_url", ScopeProject, "https://github.com/o/r2/pull/{n}"); err != nil {
		t.Fatalf("set delivery.pr_url: %v", err)
	}
	if err := SetSettingValue(projDir, "delivery.forge", ScopeProject, "gitlab"); err != nil {
		t.Fatalf("set delivery.forge: %v", err)
	}
	want := strings.NewReplacer(
		`pr_url = "https://github.com/o/r/pull/{n}"`, `pr_url = "https://github.com/o/r2/pull/{n}"`,
		`forge = "github"`, `forge = "gitlab"`,
	).Replace(before)
	if got := readRC(t, projDir); got != want {
		t.Fatalf(".cernerc =\n%s\nwant only the two lines edited in place:\n%s", got, want)
	}
	cfg, _, err := LoadConfig(projDir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if want := (DeliveryConfig{CommitURL: "https://github.com/o/r/commit/{sha}", Forge: "gitlab", PRURL: "https://github.com/o/r2/pull/{n}"}); cfg.Delivery != want {
		t.Fatalf("LoadConfig delivery = %+v, want %+v", cfg.Delivery, want)
	}

	edited := readRC(t, projDir)
	if err := SetSettingValue(projDir, "delivery.forge", ScopeProject, "bitbucket"); !errors.Is(err, ErrInvalidSettingValue) {
		t.Fatalf("set delivery.forge bitbucket = %v, want ErrInvalidSettingValue", err)
	}
	if got := readRC(t, projDir); got != edited {
		t.Fatalf("a refused forge wrote:\n%s", got)
	}
	if err := UnsetSetting(projDir, "delivery.pr_url", ScopeProject); err != nil {
		t.Fatalf("unset delivery.pr_url: %v", err)
	}
	if cfg, _, err := LoadConfig(projDir); err != nil || cfg.Delivery.PRURL != "" || cfg.Delivery.Forge != "gitlab" {
		t.Fatalf("after unset LoadConfig delivery = %+v (%v), want no pr_url, forge gitlab", cfg.Delivery, err)
	}
}

// F-0449 crit. 2: mirror.trackers takes only a list of {name, kind, url,
// label} records — name unique and non-empty, an implemented kind, an https
// URL the kind can read, label optional — and a refusal writes nothing.
func TestMirrorTrackersRecordsValidation(t *testing.T) {
	with := func(over map[string]any) map[string]any {
		r := ghRecord("gh", "issues")
		for k, v := range over {
			if v == nil {
				delete(r, k)
			} else {
				r[k] = v
			}
		}
		return r
	}
	for _, c := range []struct {
		name  string
		value any
		also  error  // a second sentinel the refusal wraps
		text  string // a text the refusal carries
	}{
		{"duplicate name", []any{ghRecord("gh", "issues"), ghRecord("gh", "other")}, nil, ""},
		{"empty name", []any{with(map[string]any{"name": ""})}, nil, ""},
		{"missing name", []any{with(map[string]any{"name": nil})}, nil, ""},
		{"unknown kind", []any{map[string]any{"name": "j", "kind": "nosuchkind", "url": "https://tracker.example/PROJ"}}, tracker.ErrUnknownKind, `unknown tracker kind "nosuchkind" (supported: ` + strings.Join(tracker.KindNames(), ", ") + `)`},
		{"missing kind", []any{with(map[string]any{"kind": nil})}, tracker.ErrUnknownKind, ""},
		{"http url", []any{with(map[string]any{"url": "http://github.com/o/issues"})}, tracker.ErrTrackerURL, ""},
		{"url the kind cannot read", []any{with(map[string]any{"url": "https://github.com/o"})}, tracker.ErrTrackerURL, ""},
		{"url over 256 characters", []any{with(map[string]any{"url": "https://github.com/o/" + strings.Repeat("r", 250)})}, nil, ""},
		{"undeclared field", []any{with(map[string]any{"token": "ghp_secret"})}, nil, ""},
		{"value that is not a string", []any{with(map[string]any{"label": int64(7)})}, nil, ""},
		{"value on several lines", []any{with(map[string]any{"label": "a\nb"})}, nil, ""},
		{"a record instead of a list", ghRecord("gh", "issues"), nil, ""},
		{"a list of strings", []any{"gh"}, nil, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			projDir, _ := settingsFixture(t, "", "", false)
			before := readRC(t, projDir)
			err := SetSettingValue(projDir, "mirror.trackers", ScopeProject, c.value)
			if !errors.Is(err, ErrInvalidSettingValue) {
				t.Fatalf("set %v = %v, want errors.Is ErrInvalidSettingValue", c.value, err)
			}
			if c.also != nil && !errors.Is(err, c.also) {
				t.Errorf("set %v = %v, want errors.Is %v too", c.value, err, c.also)
			}
			if c.text != "" && !strings.Contains(err.Error(), c.text) {
				t.Errorf("error %q lacks %q", err, c.text)
			}
			if got := readRC(t, projDir); got != before {
				t.Fatalf("a refused value wrote:\n%s", got)
			}
		})
	}

	for _, c := range []struct {
		name  string
		value any
		want  []map[string]string
	}{
		{"label omitted", []any{ghRecord("gh", "issues")}, []map[string]string{{"name": "gh", "kind": "github", "url": "https://github.com/o/issues"}}},
		{"label set, two trackers", []any{with(map[string]any{"label": "public"}), ghRecord("other", "other")}, []map[string]string{
			{"name": "gh", "kind": "github", "url": "https://github.com/o/issues", "label": "public"},
			{"name": "other", "kind": "github", "url": "https://github.com/o/other"},
		}},
		{"typed list of maps", []map[string]any{ghRecord("gh", "issues")}, []map[string]string{{"name": "gh", "kind": "github", "url": "https://github.com/o/issues"}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			projDir, _ := settingsFixture(t, "", "", false)
			if err := SetSettingValue(projDir, "mirror.trackers", ScopeProject, c.value); err != nil {
				t.Fatalf("set %v: %v", c.value, err)
			}
			if got := storedTrackers(t, projDir); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("read back %v, want %v", got, c.want)
			}
		})
	}
}

// F-0449 crit. 2: the CLI literal of mirror.trackers is JSON.
func TestSetSettingRecordsJSON(t *testing.T) {
	projDir, _ := settingsFixture(t, "", "", false)
	if err := SetSetting(projDir, "mirror.trackers", ScopeProject, `[{"name":"gh","kind":"github","url":"https://github.com/o/issues"}]`); err != nil {
		t.Fatalf("SetSetting JSON: %v", err)
	}
	if got, want := readRC(t, projDir), baseCernerc+"\n[mirror]\ntrackers = [{name = \"gh\", kind = \"github\", url = \"https://github.com/o/issues\"}]\n"; got != want {
		t.Fatalf(".cernerc =\n%s\nwant\n%s", got, want)
	}
	written := readRC(t, projDir)
	for _, c := range []struct {
		name, raw string
		also      error
	}{
		{"not JSON", `[{"name":`, nil},
		{"an object, not a list", `{"name":"gh","kind":"github","url":"https://github.com/o/issues"}`, nil},
		{"a number in a field", `[{"name":"gh","kind":"github","url":"https://github.com/o/issues","label":7}]`, nil},
		{"unknown kind", `[{"name":"j","kind":"nosuchkind","url":"https://tracker.example/PROJ"}]`, tracker.ErrUnknownKind},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := SetSetting(projDir, "mirror.trackers", ScopeProject, c.raw)
			if !errors.Is(err, ErrInvalidSettingValue) {
				t.Fatalf("SetSetting(%s) = %v, want errors.Is ErrInvalidSettingValue", c.raw, err)
			}
			if c.also != nil && !errors.Is(err, c.also) {
				t.Errorf("SetSetting(%s) = %v, want errors.Is %v too", c.raw, err, c.also)
			}
			if got := readRC(t, projDir); got != written {
				t.Fatalf("a refused value wrote:\n%s", got)
			}
		})
	}
}

// F-0449 crit. 1 to 3: MirrorSettings reads the three mirror keys through the
// registry: defaults off/accept, the records as tracker configs in order, a
// stored value the registry refuses counts as unset, and the keys are
// project-only, so the host settings cannot turn the mirror on.
func TestMirrorSettings(t *testing.T) {
	const two = `trackers = [{name = "a", kind = "github", url = "https://github.com/o/a", label = "public"}, {name = "b", kind = "github", url = "https://ghe.example/o/b"}]`
	for _, c := range []struct {
		name, project, host string
		want                MirrorConfig
	}{
		{"unset", "", "", MirrorConfig{CloseOn: "accept"}},
		{"set", "\n[mirror]\nenabled = true\nclose_on = \"merge\"\n" + two + "\n", "", MirrorConfig{Enabled: true, CloseOn: "merge", Trackers: []tracker.Config{
			{Name: "a", Kind: "github", URL: "https://github.com/o/a", Label: "public"},
			{Name: "b", Kind: "github", URL: "https://ghe.example/o/b"},
		}}},
		{"invalid values count as unset", "\n[mirror]\nenabled = \"yes\"\nclose_on = \"never\"\n" +
			`trackers = [{name = "a", kind = "github", url = "https://github.com/o/a"}, {name = "a", kind = "github", url = "https://github.com/o/b"}]` + "\n", "",
			MirrorConfig{CloseOn: "accept"}},
		{"an unknown kind counts as unset", "\n[mirror]\nenabled = true\n" + `trackers = [{name = "j", kind = "nosuchkind", url = "https://tracker.example/P"}]` + "\n", "",
			MirrorConfig{Enabled: true, CloseOn: "accept"}},
		{"the host scope cannot turn it on", "", "[mirror]\nenabled = true\nclose_on = \"merge\"\n" + two + "\n", MirrorConfig{CloseOn: "accept"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			projDir, _ := settingsFixture(t, c.project, c.host, false)
			got := MirrorSettings(projDir)
			if got.Enabled != c.want.Enabled || got.CloseOn != c.want.CloseOn || !slices.Equal(got.Trackers, c.want.Trackers) {
				t.Fatalf("MirrorSettings = %+v, want %+v", got, c.want)
			}
		})
	}
	t.Run("outside a project", func(t *testing.T) {
		projDir, _ := settingsFixture(t, "", "", true)
		if got := MirrorSettings(projDir); got.Enabled || got.CloseOn != "accept" || len(got.Trackers) != 0 {
			t.Fatalf("MirrorSettings outside a project = %+v, want off, accept, no trackers", got)
		}
	})
}

// F-0471 crit. 1 to 3: the cerne: prefix belongs to the mirror's state labels.
func TestMirrorTrackersRefusesCerneLabel(t *testing.T) {
	record := func(label string) map[string]any {
		r := ghRecord("gh", "issues")
		r["label"] = label
		return r
	}
	for _, label := range []string{"cerne:public", "Cerne:Public", "  cerne:x", "cerne:approved"} {
		t.Run("refuses "+label, func(t *testing.T) {
			projDir, _ := settingsFixture(t, "", "", false)
			before := readRC(t, projDir)
			err := SetSettingValue(projDir, "mirror.trackers", ScopeProject, []any{record(label)})
			if !errors.Is(err, ErrInvalidSettingValue) {
				t.Fatalf("set label %q = %v, want errors.Is ErrInvalidSettingValue", label, err)
			}
			if !strings.Contains(err.Error(), label) || !strings.Contains(err.Error(), "cerne:") {
				t.Errorf("error %q must cite the label and the reserved prefix", err)
			}
			if got := readRC(t, projDir); got != before {
				t.Fatalf("a refused value wrote:\n%s", got)
			}
		})
	}
	for _, label := range []string{"", "cerne-public", "public:cerne", "cerne"} {
		t.Run("accepts "+label, func(t *testing.T) {
			projDir, _ := settingsFixture(t, "", "", false)
			if err := SetSettingValue(projDir, "mirror.trackers", ScopeProject, []any{record(label)}); err != nil {
				t.Fatalf("set label %q: %v", label, err)
			}
		})
	}
	t.Run("a hand-edited .cernerc is an invalid value", func(t *testing.T) {
		rc := "\n[mirror]\nenabled = true\n" + `trackers = [{name = "gh", kind = "github", url = "https://github.com/o/issues", label = "cerne:public"}]` + "\n"
		projDir, _ := settingsFixture(t, rc, "", false)
		if got := MirrorSettings(projDir); len(got.Trackers) != 0 {
			t.Fatalf("MirrorSettings trackers = %v, want none", got.Trackers)
		}
		var found bool
		for _, s := range ResolveSettings(projDir).Settings {
			for _, p := range s.Invalid {
				if s.ID == "mirror.trackers" && p.Scope == ScopeProject {
					found = true
				}
			}
		}
		if !found {
			t.Fatal("no project-scope Invalid for mirror.trackers")
		}
	})
}
