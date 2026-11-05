package cli

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/lint"
	"gitlab.com/rockt/cerne/internal/store"
)

// closedFeaturesProject seeds one feature per state in the backend and pulls, so the
// project has a lock and a manifest; closed features materialize nothing.
func closedFeaturesProject(t *testing.T, states ...domain.FeatureState) (projDir string, ids map[domain.FeatureState]string) {
	t.Helper()
	projDir, _, repoDir := setupProject(t)
	ids = seedResultFeatures(t, repoDir, states...)
	if out, err := execCerne(t, "pull", "--dir", projDir); err != nil {
		t.Fatalf("pull: %v\n%s", err, out)
	}
	return projDir, ids
}

// recreate writes .docs/current/features/<folder>/<doc>.md by hand, the way an
// agent does after a pull that did not materialize it; returns the .docs-relative path.
func recreate(t *testing.T, projDir, folder, doc, content string) string {
	t.Helper()
	rel := "current/features/" + folder + "/" + doc + ".md"
	p := filepath.Join(projDir, ".docs", filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, p, content)
	return rel
}

var pushEntrypoints = [][]string{{"push"}, {"push", "--dry-run"}, {"lint", "--staged"}}

// F-0307 crit. 3: an untracked document of a closed feature is refused with the
// state, the correction, the read command and the unblock — on push,
// push --dry-run and lint --staged — and never with "run 'cerne pull' to resync".
func TestPushFrozenDocumentMessage(t *testing.T) {
	projDir, ids := closedFeaturesProject(t, domain.StateImplemented, domain.StateDone)
	for _, state := range []domain.FeatureState{domain.StateImplemented, domain.StateDone} {
		id := ids[state]
		for name, content := range map[string]string{"with frontmatter": realDoc(id, "Reescrito."), "without frontmatter": "# Reescrito\n"} {
			rel := recreate(t, projDir, id+"-feature", "result", content)
			local := ".docs/" + rel
			_, cmd := lint.Correction(state, id, "result")
			cmd = strings.Replace(cmd, "<result.md>", local, 1)
			for _, args := range pushEntrypoints {
				_, err := execCerne(t, append(args, "--dir", projDir)...)
				if !errors.Is(err, app.ErrFeatureFrozen) {
					t.Fatalf("%s %s %v: err = %v, want ErrFeatureFrozen", state, name, args, err)
				}
				for _, want := range []string{
					"frozen document: " + rel,
					id + " is " + string(state),
					"correct it: " + cmd,
					"read it:    " + wantRead(state, id, "result") + "\n  unblock:    remove " + local,
				} {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("%s %s %v: message lacks %q:\n%v", state, name, args, want, err)
					}
				}
				if strings.Contains(err.Error(), "to resync") {
					t.Errorf("%s %s %v: still tells to resync:\n%v", state, name, args, err)
				}
			}
			if err := os.Remove(filepath.Join(projDir, ".docs", filepath.FromSlash(rel))); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Logf("rendered block:\n%v", func() error {
		id := ids[domain.StateImplemented]
		recreate(t, projDir, id+"-feature", "spec", realDoc(id, "x"))
		_, err := execCerne(t, "push", "--dir", projDir)
		return err
	}())
}

// F-0307 crit. 4: where no command changes the document the block says so.
func TestFrozenMessageTerminalHasNoPath(t *testing.T) {
	projDir, ids := closedFeaturesProject(t, domain.StateRejected, domain.StateSuperseded)
	for state, id := range ids {
		rel := recreate(t, projDir, id+"-feature", "result", realDoc(id, "Reescrito."))
		_, err := execCerne(t, "push", "--dir", projDir)
		if !errors.Is(err, app.ErrFeatureFrozen) {
			t.Fatalf("%s: err = %v, want ErrFeatureFrozen", state, err)
		}
		for _, want := range []string{
			"frozen document: " + rel,
			"correct it: no command changes it: terminal history ('cerne feature errata " + id + "' only annotates)",
			"read it:    " + wantRead(state, id, "result") + "\n  unblock:    remove .docs/" + rel,
		} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s: message lacks %q:\n%v", state, want, err)
			}
		}
		t.Logf("%s:\n%v", state, err)
		if err := os.Remove(filepath.Join(projDir, ".docs", filepath.FromSlash(rel))); err != nil {
			t.Fatal(err)
		}
	}
}

// F-0307 crit. 5: not a closed feature's document -> the phrase stays, byte for byte.
func TestPushUntrackedMessageUnchanged(t *testing.T) {
	projDir, ids := closedFeaturesProject(t, domain.StateInDevelopment)
	live := ids[domain.StateInDevelopment]
	cases := map[string]struct{ folder, content string }{
		"no id":           {"loose-notes", "# sem id\n"},
		"unknown id":      {"F-0999-ghost", realDoc("F-0999", "x")},
		"live feature":    {live + "-copy", realDoc(live, "x")},
		"live, no header": {live + "-copy", "# sem frontmatter\n"},
	}
	for name, tc := range cases {
		rel := recreate(t, projDir, tc.folder, "result", tc.content)
		want := "untracked file: " + rel + " — run 'cerne pull' to resync"
		for _, args := range pushEntrypoints {
			_, err := execCerne(t, append(args, "--dir", projDir)...)
			if err == nil || err.Error() != want {
				t.Errorf("%s %v: err = %v, want exactly %q", name, args, err, want)
			}
		}
		if err := os.Remove(filepath.Join(projDir, ".docs", filepath.FromSlash(rel))); err != nil {
			t.Fatal(err)
		}
	}
}

// wantFix is the command (or the no-command note) the correction table gives a
// document in a state: what a refusal must name, read from the one source
// instead of a literal repeated in the test (F-0307 crit. 8).
func wantFix(state domain.FeatureState, id, doc string) string {
	kind, cmd := lint.Correction(state, id, doc)
	if kind == lint.CorrectNone {
		return app.NoCorrectionNote(id)
	}
	return cmd
}

const (
	evidenceMark = "sem evidência do(s) critério(s)"
	gauntletMark = "result.md sem " // "sem seção "## Gauntlet"", "sem veredito…": the rule's own opening
)

// lintLine returns the lint output line of the rule's finding ("" when absent).
func lintLine(out, mark string) string {
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, mark) && strings.Contains(l, " — ") {
			return l
		}
	}
	return ""
}

// F-0307 crit. 2: following the lint message works — the command the detail
// names, run with a file in the form it describes, clears the finding.
func TestFollowingTheLintMessageClearsIt(t *testing.T) {
	spec := "## Objetivo e critérios de aceitação\n\n1. a\n2. b\n"
	good := "critério 1 provado. critério 2 provado.\n"
	cmdRe := regexp.MustCompile(`'(cerne feature (?:amend|errata) [^']*)'`)
	// A closed feature in debt: the result rewritten straight in the backend, as the dogfood did.
	closedWithDebt := func(t *testing.T, accept bool, resultBody string) (projDir, repoDir, id string) {
		projDir, repoDir, id = startedFeature(t)
		writeDoc(t, repoDir, id, "spec.md", realDoc(id, spec))
		writeDoc(t, repoDir, id, "result.md", realDoc(id, good))
		commitBackend(t, repoDir)
		if out, err := execCerne(t, "feature", "complete", id, "--dir", projDir); err != nil {
			t.Fatalf("complete: %v\n%s", err, out)
		}
		if accept {
			if out, err := execCerne(t, "feature", "accept", id, "--dir", projDir); err != nil {
				t.Fatalf("accept: %v\n%s", err, out)
			}
		}
		writeDoc(t, repoDir, id, "result.md", resultBody)
		commitBackend(t, repoDir)
		return
	}
	run := func(t *testing.T, projDir, id, line string, file, reason string) {
		t.Helper()
		m := cmdRe.FindStringSubmatch(line)
		if m == nil {
			t.Fatalf("no command in the detail: %q", line)
		}
		cmdline := strings.NewReplacer("<result.md>", file, "<errata.md>", file, "<why>", reason).Replace(m[1])
		args := regexpSplit(cmdline)
		out, err := execCerne(t, append(args[1:], "--dir", projDir)...)
		if err != nil {
			t.Fatalf("%s: %v\n%s", cmdline, err, out)
		}
	}
	write := func(t *testing.T, content string) string {
		p := filepath.Join(t.TempDir(), "doc.md")
		mustWrite(t, p, content)
		return p
	}

	t.Run("done, missing evidence", func(t *testing.T) {
		projDir, _, id := closedWithDebt(t, true, "Result sem prova.\n\n## Gauntlet\n\nBarra: fixture — ref\nRodada 1 — vitória: \"fixture\"\n")
		out, _ := execCerne(t, "lint", "--feature", id, "--dir", projDir)
		line := lintLine(out, evidenceMark)
		if line == "" {
			t.Fatalf("expected the evidence finding:\n%s", out)
		}
		run(t, projDir, id, line, write(t, "Errata: critério 1 provado por X; critério 2 provado por Y.\n"), "")
		if out, _ := execCerne(t, "lint", "--feature", id, "--dir", projDir); lintLine(out, evidenceMark) != "" {
			t.Fatalf("finding survived the erratum:\n%s", out)
		}
	})
	t.Run("done, missing gauntlet", func(t *testing.T) {
		projDir, _, id := closedWithDebt(t, true, "critério 1 provado. critério 2 provado.\n")
		out, _ := execCerne(t, "lint", "--feature", id, "--dir", projDir)
		line := lintLine(out, gauntletMark)
		if line == "" {
			t.Fatalf("expected the gauntlet finding:\n%s", out)
		}
		run(t, projDir, id, line, write(t, "Registro tardio.\n\n## Gauntlet\n\nBarra: fixture — ref\nRodada 1 — vitória: \"fixture\"\n"), "")
		if out, _ := execCerne(t, "lint", "--feature", id, "--dir", projDir); lintLine(out, gauntletMark) != "" {
			t.Fatalf("finding survived the erratum:\n%s", out)
		}
	})
	t.Run("implemented, both findings, amend", func(t *testing.T) {
		projDir, _, id := closedWithDebt(t, false, "Result sem nada.\n")
		out, _ := execCerne(t, "lint", "--feature", id, "--dir", projDir)
		for _, mark := range []string{evidenceMark, gauntletMark} {
			line := lintLine(out, mark)
			if line == "" {
				t.Fatalf("expected the %q finding:\n%s", mark, out)
			}
			if !strings.Contains(line, "feature implemented:") || !strings.Contains(line, "cerne feature amend "+id) {
				t.Fatalf("detail should name the amend of an implemented feature: %q", line)
			}
		}
		run(t, projDir, id, lintLine(out, evidenceMark), write(t, realDoc(id, good)), "fecha o débito")
		if out, _ := execCerne(t, "lint", "--feature", id, "--dir", projDir); lintLine(out, evidenceMark) != "" || lintLine(out, gauntletMark) != "" {
			t.Fatalf("findings survived the amend:\n%s", out)
		}
	})
}

// regexpSplit splits a command line on spaces, keeping a "double quoted" span whole.
func regexpSplit(s string) []string {
	var out []string
	for _, m := range regexp.MustCompile(`"([^"]*)"|\S+`).FindAllStringSubmatch(s, -1) {
		if m[0][0] == '"' {
			out = append(out, m[1])
		} else {
			out = append(out, m[0])
		}
	}
	return out
}

var quotedCommandRe = regexp.MustCompile(`(?:'|:\s+)cerne((?: [a-z][a-z-]*)+)`)

// missingCommands returns every quoted or block-line `cerne <words>` in text (up to the first
// flag, <placeholder>, id or quote) that is not a command of cli.Root().
func missingCommands(text string) (missing []string, found int) {
	for _, m := range quotedCommandRe.FindAllStringSubmatch(text, -1) {
		found++
		words := strings.Fields(m[1])
		c, rest, err := Root().Find(words)
		if err != nil || len(rest) != 0 || c.CommandPath() != "cerne "+strings.Join(words, " ") {
			missing = append(missing, "cerne"+m[1])
		}
	}
	return missing, found
}

// F-0307 crit. 7: every `cerne …` command the messages of this feature name
// exists in the command tree — the lint details, the push block, the pull hint,
// the errata confirmation and F-0305's write refusals, rendered, not copied.
func TestMessagesNameExistingCommands(t *testing.T) {
	t.Run("detects a command that does not exist", func(t *testing.T) {
		if missing, found := missingCommands("Use 'cerne show <id>' or 'cerne errata F-0001' to fix"); found != 2 || len(missing) != 1 || missing[0] != "cerne errata" {
			t.Fatalf("missing = %v of %d, want [cerne errata] of 2", missing, found)
		}
	})

	projDir, _, repoDir := setupProject(t, "finance")
	states := []domain.FeatureState{domain.StateInDevelopment, domain.StateImplemented, domain.StateDone, domain.StateRejected, domain.StateSuperseded}
	ids := seedResultFeatures(t, repoDir, states...)
	var rendered []string
	add := func(what, text string) {
		rendered = append(rendered, text)
		t.Logf("%s: %s", what, text)
	}

	for _, state := range states {
		id := ids[state]
		writeDoc(t, repoDir, id, "spec.md", "## Objetivo e critérios de aceitação\n\n1. a\n")
		writeDoc(t, repoDir, id, "result.md", "sem prova\n")
		f, _ := store.LoadFeature(repoDir, id)
		for _, fd := range lint.CheckResult(repoDir, f) { // both rules, in each state
			add(fd.Rule+" "+string(state), fd.Detail)
		}
		for _, doc := range []string{"spec", "plan", "result"} {
			if !domain.TitleEditable(state) {
				add("push block "+string(state)+" "+doc, frozenBlock("current/features/"+id+"-x/"+doc+".md", id, state))
			}
			add("write refusal "+string(state)+" "+doc, app.DocFixPhrase(id, doc, state))
			if _, err := app.CheckDocWritable(repoDir, id, doc); err != nil {
				add("CheckDocWritable "+string(state)+" "+doc, err.Error())
			}
		}
		if _, _, err := app.AmendFeatureResult(repoDir, projDir, id, realDoc(id, "critério 1."), "porque"); err != nil {
			add("amend "+string(state), err.Error())
		}
	}
	out, err := execCerne(t, "pull", "--scope", "finance", "--dir", projDir)
	if err != nil {
		t.Fatalf("pull: %v\n%s", err, out)
	}
	add("pull, no feature in module", out)
	// A module that has features, all closed: the other branch of the hint.
	f, _ := store.LoadFeature(repoDir, ids[domain.StateImplemented])
	f.Module = "finance"
	if err := store.SaveFeature(repoDir, f); err != nil {
		t.Fatal(err)
	}
	commitBackend(t, repoDir)
	out, err = execCerne(t, "pull", "--scope", "finance", "--dir", projDir)
	if err != nil {
		t.Fatalf("pull: %v\n%s", err, out)
	}
	add("pull, closed features", out)
	out, err = execCerne(t, "feature", "errata", ids[domain.StateDone], "nota", "--dir", projDir)
	if err != nil {
		t.Fatalf("errata: %v\n%s", err, out)
	}
	add("errata", out)

	missing, found := missingCommands(strings.Join(rendered, "\n"))
	if found < 20 {
		t.Fatalf("only %d command mentions found; the extraction is broken", found)
	}
	if len(missing) != 0 {
		t.Fatalf("messages name commands that do not exist: %v", missing)
	}
}

// Two frozen documents are refused once, each with its own block, in walk order —
// the second blocker must not surface only after the first is fixed.
func TestPushNamesEveryFrozenDocument(t *testing.T) {
	projDir, ids := closedFeaturesProject(t, domain.StateImplemented, domain.StateRejected)
	a, b := ids[domain.StateImplemented], ids[domain.StateRejected]
	relA := recreate(t, projDir, a+"-feature", "result", realDoc(a, "x"))
	relB := recreate(t, projDir, b+"-feature", "spec", realDoc(b, "y"))
	for _, args := range pushEntrypoints {
		_, err := execCerne(t, append(args, "--dir", projDir)...)
		if !errors.Is(err, app.ErrFeatureFrozen) {
			t.Fatalf("%v: err = %v, want ErrFeatureFrozen", args, err)
		}
		msg := err.Error()
		ia, ib := strings.Index(msg, "frozen document: "+relA), strings.Index(msg, "frozen document: "+relB)
		if ia < 0 || ib < 0 || ia > ib {
			t.Fatalf("%v: want both blocks in walk order:\n%s", args, msg)
		}
	}
}

// wantRead is what the push block's read line must say: the document alone for
// a live-style amend, the whole feature (which prints its errata) when the
// path is an erratum or none — `show --doc` has no errata document.
func wantRead(state domain.FeatureState, id, doc string) string {
	if kind, _ := lint.Correction(state, id, doc); kind == lint.CorrectByAmend {
		return "cerne show " + id + " --doc " + doc
	}
	return "cerne show " + id
}
