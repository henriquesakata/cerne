package doccheck

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/doclang"
)

func completeSpec(extra string) string {
	var b strings.Builder
	for _, h := range doclang.Sections(doclang.Spec, doclang.PT) {
		b.WriteString("## " + h + "\n\ntexto\n\n")
	}
	return b.String() + extra
}

func codes(is []Issue) []string {
	var out []string
	for _, i := range is {
		out = append(out, i.Code+"/"+i.Severity)
	}
	return out
}

func TestCheckSpecBody(t *testing.T) {
	bar := "Barra: não aplicável (sem comparável) — padrão alternativo: os testes\n"
	cases := []struct {
		name  string
		body  string
		light bool
		want  string
	}{
		{"complete", completeSpec(bar), false, ""},
		{"no bar", completeSpec(""), false, "bar-missing/error"},
		{"no bar in light mode", completeSpec(""), true, ""},
		{"malformed bar", completeSpec("Barra:\n"), false, "bar-malformed/error"},
		{"clarification is a warning", completeSpec(bar + "[ESCLARECER: qual? | a | b]\n"), false, "open-clarification/warning"},
		{"truncated", "## Problema\n\ntexto\n", true, "incomplete-spec/error"},
	}
	for _, c := range cases {
		got := strings.Join(codes(CheckSpecBody([]byte(c.body), doclang.PT, c.light)), ",")
		if got != c.want {
			t.Errorf("%s: issues = %q, want %q", c.name, got, c.want)
		}
	}
	is := CheckSpecBody([]byte(completeSpec("Barra:\n")), doclang.PT, false)
	if len(is) != 1 || !strings.Contains(is[0].Detail, "malformed") {
		t.Errorf("detail = %+v", is)
	}
}

func TestCheckSpecBody_MissingListMatchesMissingSections(t *testing.T) {
	body := []byte("## Problema\n\ntexto\n")
	is := CheckSpecBody(body, doclang.PT, true)
	want := MissingSections(body, doclang.Spec, doclang.PT)
	if len(is) != 1 || strings.Join(is[0].Missing, ",") != strings.Join(want, ",") {
		t.Errorf("issue = %+v, want missing %v", is, want)
	}
}
