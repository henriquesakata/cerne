package domain

import (
	"errors"
	"slices"
	"testing"
)

// F-0457 criterion 1: the two accepted forms, in canonical shape.
func TestNormalizeTriggers(t *testing.T) {
	got, err := NormalizeTriggers([]string{
		" edit:ui/dist/** ", "edit:./docs.lock", `edit:ui\src\app.tsx`,
		"bash:git   tag", "bash:git tag", "", "bash:glab mr create",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"edit:ui/dist/**", "edit:docs.lock", "edit:ui/src/app.tsx", "bash:git tag", "bash:glab mr create"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got, err := NormalizeTriggers(nil); err != nil || got != nil {
		t.Fatalf("nil in: got %q, %v", got, err)
	}
}

// F-0457 criterion 1: every refusal names its sentinel and yields no partial result.
func TestNormalizeTriggersRefusals(t *testing.T) {
	cases := []struct {
		in   string
		want error
	}{
		{"read:x", ErrTriggerKind},
		{"ui/**", ErrTriggerKind},
		{"edit:/etc/passwd", ErrTriggerPath},
		{"edit:C:/repo/x", ErrTriggerPath},
		{"edit:../other/x", ErrTriggerPath},
		{"edit:ui/../x", ErrTriggerPath},
		{"edit:ui/*.ts", ErrTriggerPath},
		{"edit:**", ErrTriggerPath},
		{"edit:ui/", ErrTriggerPath},
		{"edit:", ErrTriggerEmpty},
		{"bash:", ErrTriggerEmpty},
		{"bash:   ", ErrTriggerEmpty},
		{"bash:git tag && git push", ErrTriggerWords},
		{"bash:echo;ls", ErrTriggerWords},
		{"bash:(git)", ErrTriggerWords},
		{"bash:a|b", ErrTriggerWords},
	}
	for _, c := range cases {
		got, err := NormalizeTriggers([]string{"bash:git tag", c.in})
		if !errors.Is(err, c.want) {
			t.Errorf("%q: err = %v, want %v", c.in, err, c.want)
		}
		if got != nil {
			t.Errorf("%q: partial result %q", c.in, got)
		}
	}
}

func TestMatchGlob(t *testing.T) {
	cases := []struct {
		glob, path string
		want       bool
	}{
		{"docs.lock", "docs.lock", true},
		{"docs.lock", "x/docs.lock", false},
		{"ui/dist/**", "ui/dist/index.html", true},
		{"ui/dist/**", "ui/dist/assets/a.js", true},
		{"ui/dist/**", "ui/distro/a.js", false},
		{"ui/dist/**", "ui/dist", false},
	}
	for _, c := range cases {
		if got := MatchGlob(c.glob, c.path); got != c.want {
			t.Errorf("MatchGlob(%q, %q) = %v", c.glob, c.path, got)
		}
	}
}

// F-0457 criterion 4: an exact path outranks a dir/** glob; bash triggers never match a path.
func TestPathTriggerScore(t *testing.T) {
	cases := []struct {
		trig, rel string
		want      int
	}{
		{"edit:ui/dist/index.html", "ui/dist/index.html", 2},
		{"edit:ui/dist/**", "ui/dist/index.html", 1},
		{"edit:ui/src/**", "ui/dist/index.html", 0},
		{"bash:ui/dist/index.html", "ui/dist/index.html", 0},
	}
	for _, c := range cases {
		if got := PathTriggerScore(c.trig, c.rel); got != c.want {
			t.Errorf("PathTriggerScore(%q, %q) = %d, want %d", c.trig, c.rel, got, c.want)
		}
	}
}

// F-0457 criterion 6: consecutive words of the command, split by spaces and
// shell operators; never inside a longer word.
func TestCommandTriggerWords(t *testing.T) {
	cases := []struct {
		trig, cmd string
		want      int
	}{
		{"bash:git tag", "git tag v0.14.0", 2},
		{"bash:git tag", "cd x && git tag -a v1", 2},
		{"bash:git tag", "cd x&&git tag", 2},
		{"bash:git tag", "(git tag)", 2},
		{"bash:git tag", "git tags", 0},
		{"bash:git tag", "git log --tag", 0},
		{"bash:git tag", "git -C x tag", 0},
		{"bash:cerne import", "./bin/cerne import", 2},
		{"bash:cerne import", "x/cerne-import", 0},
		{"bash:glab mr create", "glab mr create --fill\n", 3},
		{"edit:git tag", "git tag", 0},
		{"bash:git tag", "", 0},
	}
	for _, c := range cases {
		if got := CommandTriggerWords(c.trig, c.cmd); got != c.want {
			t.Errorf("CommandTriggerWords(%q, %q) = %d, want %d", c.trig, c.cmd, got, c.want)
		}
	}
}
