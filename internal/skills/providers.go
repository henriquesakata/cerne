package skills

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/henriquesakata/cerne/internal/hooks"
)

// target describes where one agent reads skills and which project doc, if any,
// carries the pointer to the installed guide.
type target struct {
	skillsDir string // project-relative, slash-separated
	pointer   string // AGENTS.md / GEMINI.md, "" when the agent needs none
	// instructions is cerne's own instructions file, written whole instead of a
	// pointer block in a project doc ("" when the agent has none; F-0444).
	instructions string
}

// targets are the project-level skills paths confirmed in each agent's
// official docs (F-0362): Codex scans .agents/skills (R-0064), Gemini CLI reads
// .gemini/skills (R-0065), Cursor reads .cursor/skills (R-0066).
var targets = map[hooks.Provider]target{
	hooks.ProviderCursor: {skillsDir: ".cursor/skills"},
	hooks.ProviderCodex:  {skillsDir: ".agents/skills", pointer: "AGENTS.md"},
	hooks.ProviderGemini: {skillsDir: ".gemini/skills", pointer: "GEMINI.md"},
	// Copilot's three surfaces read .github/skills and .github/instructions
	// (F-0444); the team's copilot-instructions.md is never touched.
	hooks.ProviderCopilot: {skillsDir: ".github/skills", instructions: ".github/instructions/cerne.instructions.md"},
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
	if p == hooks.ProviderCursor {
		return installCursorSkills(projDir, t)
	}
	rel := t.skillsDir + "/" + guideName
	res := Result{Skill: guideName, Path: rel, Agent: p}
	dst := filepath.Join(projDir, filepath.FromSlash(rel), "SKILL.md")
	if link := symlinkOnPath(projDir, rel+"/SKILL.md"); link != "" {
		res.Status, res.Note = StatusSkipped, link+" is a symlink — not written through; copy .claude/skills/"+guideName+"/SKILL.md to "+rel+"/SKILL.md by hand"
		return []Result{res}
	}
	if st, err := writeSkillFiles(dst, authoringSkill()); err != nil {
		res.Status, res.Note = StatusSkipped, "could not write "+rel+"/SKILL.md ("+err.Error()+") — copy .claude/skills/"+guideName+"/SKILL.md there by hand"
		return []Result{res}
	} else {
		res.Status = st
	}
	out := []Result{res}
	if t.pointer != "" {
		pr := writePointer(projDir, t.pointer, rel+"/SKILL.md")
		pr.Agent = p
		out = append(out, pr)
	}
	if t.instructions != "" {
		ir := writeInstructions(projDir, t.instructions, rel+"/SKILL.md")
		ir.Agent = p
		out = append(out, ir)
	}
	return out
}

// installCursorSkills delivers every canonical skill to .cursor/skills/ (F-0527).
func installCursorSkills(projDir string, t target) []Result {
	out := make([]Result, 0, len(All()))
	for _, s := range All() {
		rel := t.skillsDir + "/" + s.Name
		res := Result{Skill: s.Name, Path: rel, Agent: hooks.ProviderCursor}
		dst := filepath.Join(projDir, filepath.FromSlash(rel), "SKILL.md")
		if link := symlinkOnPath(projDir, rel+"/SKILL.md"); link != "" {
			res.Status, res.Note = StatusSkipped, link+" is a symlink — not written through; copy .claude/skills/"+s.Name+"/SKILL.md to "+rel+"/SKILL.md by hand"
			out = append(out, res)
			continue
		}
		st, err := writeSkillFiles(dst, s)
		if err != nil {
			res.Status, res.Note = StatusSkipped, "could not write "+rel+"/SKILL.md ("+err.Error()+") — copy .claude/skills/"+s.Name+"/SKILL.md there by hand"
		} else {
			res.Status = st
		}
		out = append(out, res)
	}
	return out
}

// copilotInstructions renders the always-applied instructions file.
func copilotInstructions(skillPath string) string {
	return "---\ndescription: cerne project rules — work registration discipline\napplyTo: \"**\"\n---\n\n" +
		hooks.RegisterRules + "\n\ncerne authoring guide: before creating or managing cerne features, decisions or references, read `" + skillPath + "`.\n"
}

// writeInstructions writes cerne's own always-applied instructions file: the
// registration floor plus the pointer to the skill. Never through a symlink.
func writeInstructions(projDir, rel, skillPath string) Result {
	res := Result{Skill: guideName + " instructions", Path: rel}
	if link := symlinkOnPath(projDir, rel); link != "" {
		res.Status, res.Note = StatusSkipped, link+" is a symlink — not written through; copy the registration rules there by hand"
		return res
	}
	content := copilotInstructions(skillPath)
	st, err := writeSkill(filepath.Join(projDir, filepath.FromSlash(rel)), content)
	if err != nil {
		res.Status, res.Note = StatusSkipped, "could not write "+rel+" ("+err.Error()+")"
		return res
	}
	res.Status = st
	return res
}

func authoringSkill() Skill {
	for _, s := range All() {
		if s.Name == guideName {
			return s
		}
	}
	panic("skills: " + guideName + " not embedded")
}

// symlinkOnPath returns the first component of rel (slash-separated, under
// projDir) that exists and is a symlink, or "".
func symlinkOnPath(projDir, rel string) string {
	cur := projDir
	var seen []string
	for _, c := range strings.Split(rel, "/") {
		cur = filepath.Join(cur, c)
		seen = append(seen, c)
		if fi, err := os.Lstat(cur); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			return strings.Join(seen, "/")
		}
	}
	return ""
}

// scanBlock finds the managed block by line-anchored markers outside ``` / ~~~
// fences. It returns the byte range [start,end) of the block (end excludes the
// final EOL), or found=false when there is none, or ok=false when the markers
// are duplicated or out of order.
func scanBlock(raw []byte) (start, end int, found, ok bool) {
	var begins, ends [][2]int
	fence := ""
	off := 0
	for _, line := range bytes.SplitAfter(raw, []byte("\n")) {
		text := strings.TrimRight(string(line), "\r\n")
		trim := strings.TrimLeft(text, " ")
		switch {
		case fence != "":
			if strings.HasPrefix(trim, fence) {
				fence = ""
			}
		case strings.HasPrefix(trim, "```"):
			fence = "```"
		case strings.HasPrefix(trim, "~~~"):
			fence = "~~~"
		case text == pointerBegin:
			begins = append(begins, [2]int{off, off + len(text)})
		case text == pointerEnd:
			ends = append(ends, [2]int{off, off + len(text)})
		}
		off += len(line)
	}
	switch {
	case len(begins) == 0 && len(ends) == 0:
		return 0, 0, false, true
	case len(begins) != 1 || len(ends) != 1 || ends[0][0] < begins[0][1]:
		return 0, 0, false, false
	}
	return begins[0][0], ends[0][1], true, true
}

// StripManagedBlock returns raw without cerne's managed block. Without a block,
// or with duplicated or out-of-order markers, it returns raw intact.
func StripManagedBlock(raw []byte) []byte {
	start, end, found, ok := scanBlock(raw)
	if !found || !ok {
		return raw
	}
	out := append(append([]byte{}, raw[:start]...), raw[end:]...)
	if out = bytes.TrimRight(out, "\r\n"); len(out) > 0 {
		out = append(out, '\n')
	}
	return out
}

// writePointer adds or refreshes the single managed block in a user-owned doc.
// It never rewrites anything outside the markers, never writes through a
// symlink, mirrors the file's predominant line ending, writes atomically, and
// leaves the file byte-identical when the block is already current.
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
	line := "read " + skillPath + " before creating or managing cerne features"
	if exists && fi.Mode()&os.ModeSymlink != 0 {
		return skip(name + " is a symlink — not written through; add this line to its target by hand: " + line)
	}
	var raw []byte
	if exists {
		if raw, err = os.ReadFile(path); err != nil {
			return skip("cannot read " + name + ": " + err.Error())
		}
		if !utf8.Valid(raw) || bytes.HasPrefix(raw, []byte{0xFF, 0xFE}) || bytes.HasPrefix(raw, []byte{0xFE, 0xFF}) {
			return skip(name + " is not UTF-8 — left untouched; add this line by hand: " + line)
		}
	}
	nl := "\n"
	if crlf := bytes.Count(raw, []byte("\r\n")); crlf > bytes.Count(raw, []byte("\n"))-crlf {
		nl = "\r\n"
	}
	block := pointerBegin + nl +
		"cerne authoring guide: before creating or managing cerne features, decisions or references, read `" + skillPath + "`." + nl +
		pointerEnd

	start, end, found, ok := scanBlock(raw)
	if !ok {
		return skip("the cerne markers in " + name + " are duplicated or unbalanced — fix or remove them by hand, then rerun")
	}
	var out []byte
	if found {
		out = append(append(append([]byte{}, raw[:start]...), block...), raw[end:]...)
	} else {
		out = raw
		if len(out) > 0 {
			if !bytes.HasSuffix(out, []byte("\n")) {
				out = append(out, nl...)
			}
			out = append(out, nl...)
		}
		out = append(append(out, block...), nl...)
	}
	if exists && bytes.Equal(out, raw) {
		res.Status = StatusUnchanged
		return res
	}
	mode := os.FileMode(0o644)
	if exists {
		mode = fi.Mode().Perm()
	}
	if err := writeAtomic(path, out, mode); err != nil {
		return skip("cannot write " + name + ": " + err.Error())
	}
	res.Status = StatusCreated
	if exists {
		res.Status = StatusUpdated
	}
	return res
}

// writeAtomic writes via a temp file in the same directory and a rename, so a
// crash never leaves the user's file half-written.
func writeAtomic(path string, data []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".cerne-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, werr := f.Write(data)
	cerr := f.Close()
	if err := firstErr(werr, cerr, os.Chmod(tmp, mode)); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

// AgentLines is the per-agent summary `cerne skill install` prints: one line for
// every agent, detected or not. It says "delivered" plus the documented load
// condition — never "loaded", which cerne cannot verify — and keeps the reason
// and manual step of anything skipped.
func AgentLines(results []Result, detected []hooks.Provider) []string {
	has := map[hooks.Provider]bool{}
	for _, p := range detected {
		has[p] = true
	}
	lines := []string{"claude: guide delivered to .claude/skills/" + guideName + "/SKILL.md — " + hooks.GuideCondition(hooks.ProviderClaude)}
	for _, p := range []hooks.Provider{hooks.ProviderCursor, hooks.ProviderCodex, hooks.ProviderGemini, hooks.ProviderCopilot} {
		t := targets[p]
		dir := hooks.ConfigDir(p)
		if !has[p] {
			if p == hooks.ProviderCopilot {
				dir = ".github/copilot-instructions.md, hooks/, skills/ or agents"
			}
			lines = append(lines, string(p)+": not detected (no "+dir+"/) — run 'cerne hook install --provider "+string(p)+"' first, then 'cerne skill install'")
			continue
		}
		line := string(p) + ": guide delivered to " + t.skillsDir + "/" + guideName + "/SKILL.md — " + hooks.GuideCondition(p) + "; cerne cannot verify the agent loads it"
		for _, r := range results {
			if r.Agent == p && r.Status == StatusSkipped {
				line = string(p) + ": skipped (" + r.Path + ") — " + r.Note
				break
			}
		}
		lines = append(lines, line)
	}
	return lines
}

// AgentSummary splits the agents into the connected one (claude: the only
// agent init wires with guide and hooks) and the ones still to connect (every
// other agent, detected or not: a detected one got only the guide). A skipped
// delivery is in neither: AgentLines carries its reason.
func AgentSummary(results []Result, detected []hooks.Provider) (connected, missing []string) {
	has := map[hooks.Provider]bool{}
	for _, p := range detected {
		has[p] = true
	}
	skipped := map[hooks.Provider]bool{}
	for _, r := range results {
		if r.Status == StatusSkipped {
			skipped[r.Agent] = true
		}
	}
	connected = []string{"claude"}
	for _, p := range []hooks.Provider{hooks.ProviderCursor, hooks.ProviderCodex, hooks.ProviderGemini, hooks.ProviderCopilot} {
		switch {
		case has[p] && skipped[p]:
		default:
			missing = append(missing, string(p))
		}
	}
	return connected, missing
}
