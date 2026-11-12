package skills

import (
	"bytes"
	"os"
	"path/filepath"

	"github.com/henriquesakata/cerne/internal/hooks"
)

// target describes where one agent reads skills and which project doc, if any,
// carries the pointer to the installed guide.
type target struct {
	skillsDir string // project-relative, slash-separated
	pointer   string // AGENTS.md / GEMINI.md, "" when the agent needs none
}

// targets are the project-level skills paths confirmed in each agent's
// official docs (F-0362): Codex scans .agents/skills (R-0064), Gemini CLI reads
// .gemini/skills (R-0065), Cursor reads .cursor/skills (R-0066).
var targets = map[hooks.Provider]target{
	hooks.ProviderCursor: {skillsDir: ".cursor/skills"},
	hooks.ProviderCodex:  {skillsDir: ".agents/skills", pointer: "AGENTS.md"},
	hooks.ProviderGemini: {skillsDir: ".gemini/skills", pointer: "GEMINI.md"},
}

const (
	pointerBegin = "<!-- cerne:authoring-guide:begin (managed by `cerne skill install`; do not edit) -->"
	pointerEnd   = "<!-- cerne:authoring-guide:end -->"
	guideName    = "cerne-authoring"
)

func installProvider(projDir string, p hooks.Provider) []Result {
	t, ok := targets[p]
	if !ok { // Claude Code is always installed above; unknown names are ignored
		return nil
	}
	rel := t.skillsDir + "/" + guideName
	res := Result{Skill: guideName, Path: rel}
	dst := filepath.Join(projDir, filepath.FromSlash(rel), "SKILL.md")
	if st, err := writeSkill(dst, authoringContent()); err != nil {
		res.Status, res.Note = StatusSkipped, "could not write "+rel+"/SKILL.md ("+err.Error()+") — copy .claude/skills/"+guideName+"/SKILL.md there by hand"
		return []Result{res}
	} else {
		res.Status = st
	}
	out := []Result{res}
	if t.pointer != "" {
		out = append(out, writePointer(projDir, t.pointer, rel+"/SKILL.md"))
	}
	return out
}

func authoringContent() string {
	for _, s := range All() {
		if s.Name == guideName {
			return s.Content
		}
	}
	panic("skills: " + guideName + " not embedded")
}

// writePointer adds or refreshes the single managed block in a user-owned doc.
// It never rewrites anything outside the markers, never writes through a
// symlink, mirrors the file's line endings, and leaves the file byte-identical
// when the block is already current.
func writePointer(projDir, name, skillPath string) Result {
	res := Result{Skill: guideName + " pointer", Path: name}
	skip := func(note string) Result {
		res.Status, res.Note = StatusSkipped, note
		return res
	}
	path := filepath.Join(projDir, name)
	fi, err := os.Lstat(path)
	exists := err == nil
	if err != nil && !os.IsNotExist(err) {
		return skip("cannot inspect " + name + ": " + err.Error())
	}
	if exists && fi.Mode()&os.ModeSymlink != 0 {
		return skip(name + " is a symlink — not written through; add this line to its target by hand: read " + skillPath + " before creating or managing cerne features")
	}
	var raw []byte
	if exists {
		if raw, err = os.ReadFile(path); err != nil {
			return skip("cannot read " + name + ": " + err.Error())
		}
	}
	nl := "\n"
	if bytes.Contains(raw, []byte("\r\n")) {
		nl = "\r\n"
	}
	block := pointerBegin + nl +
		"cerne authoring guide: before creating or managing cerne features, decisions or references, read `" + skillPath + "`." + nl +
		pointerEnd

	var out []byte
	b, e := bytes.Index(raw, []byte(pointerBegin)), bytes.Index(raw, []byte(pointerEnd))
	switch {
	case b < 0 && e < 0:
		out = raw
		if len(out) > 0 {
			if !bytes.HasSuffix(out, []byte("\n")) {
				out = append(out, nl...)
			}
			out = append(out, nl...)
		}
		out = append(append(out, block...), nl...)
	case b < 0 || e < b || bytes.Count(raw, []byte(pointerBegin)) != 1 || bytes.Count(raw, []byte(pointerEnd)) != 1:
		return skip("the cerne markers in " + name + " are unbalanced — fix or remove them by hand, then rerun")
	default:
		out = append(append(append([]byte{}, raw[:b]...), block...), raw[e+len(pointerEnd):]...)
	}
	if exists && bytes.Equal(out, raw) {
		res.Status = StatusUnchanged
		return res
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return skip("cannot write " + name + ": " + err.Error())
	}
	res.Status = StatusCreated
	if exists {
		res.Status = StatusUpdated
	}
	return res
}
