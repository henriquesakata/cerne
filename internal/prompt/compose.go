package prompt

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/lint"
	"gitlab.com/rockt/cerne/internal/project"
	"gitlab.com/rockt/cerne/internal/slug"
	"gitlab.com/rockt/cerne/internal/store"
)

// ErrUnknownType is the sentinel for a --type that matches no live
// prompt-template note; its message lists the available types.
var ErrUnknownType = errors.New("unknown prompt type")

// promptTemplateComponent is the component tag that turns a note into a
// prompt template (F-0152): templates are team-versioned backend entities,
// not a new kind — they ride the note lifecycle, tags and sync.
const promptTemplateComponent = "prompt-template"

// PromptType is one available template: the slug used as --type and the
// note title it came from.
type Type struct {
	Slug  string `json:"slug"`
	Title string `json:"title"`
}

// ListPromptTypes returns the live prompt-template notes as types, sorted by
// slug (criterion 4, F-0152).
func ListTypes(repoDir string) ([]Type, error) {
	notes, err := store.LoadAllNotes(repoDir)
	if err != nil {
		return nil, err
	}
	var types []Type
	for _, n := range notes {
		if n.State != domain.NoteActive {
			continue
		}
		for _, c := range n.Components {
			if c == promptTemplateComponent {
				types = append(types, Type{Slug: slug.Make(n.Title), Title: n.Title})
				break
			}
		}
	}
	sort.Slice(types, func(i, j int) bool { return types[i].Slug < types[j].Slug })
	return types, nil
}

// ComposePromptRegister is ComposePrompt with the dispatch registered at
// composition time (criterion 3, F-0152): the W- is created here — its title
// derived from the intent, same dup-guard as `cerne work start` (F-0147) —
// so the discipline stops depending on the receiving agent. The id and the
// closing instruction ride inside the prompt.
func ComposeRegister(repoDir, projDir, typ, text string) (string, string, error) {
	w, err := app.StartWork(repoDir, projDir, deriveWorkTitle(text), nil, "", false)
	if err != nil {
		return "", "", err
	}
	prompt, err := composeInner(repoDir, projDir, typ, text, w.ID)
	if err != nil {
		return "", "", err
	}
	return prompt, w.ID, nil
}

// stripFrontmatter drops a leading `---\n…\n---` block: note bodies carry the
// cerne_id frontmatter on disk, which is plumbing — never prompt content.
// (Same local helper ask/compile/materialize keep, by package convention.)
func stripFrontmatter(s string) string {
	if !strings.HasPrefix(s, "---\n") {
		return s
	}
	rest := s[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return s
	}
	rest = rest[end+len("\n---"):]
	if i := strings.IndexByte(rest, '\n'); i >= 0 {
		rest = rest[i+1:]
	} else {
		rest = ""
	}
	return strings.TrimLeft(rest, "\n")
}

// deriveWorkTitle bounds the intent to a work title: first line, cut at a
// word boundary near 70 runes.
func deriveWorkTitle(text string) string {
	title := strings.TrimSpace(text)
	if i := strings.IndexByte(title, '\n'); i >= 0 {
		title = strings.TrimSpace(title[:i])
	}
	runes := []rune(title)
	if len(runes) <= 70 {
		return title
	}
	cut := string(runes[:70])
	if i := strings.LastIndexByte(cut, ' '); i > 0 {
		cut = cut[:i]
	}
	return strings.TrimSpace(cut) + "…"
}

// ComposePrompt builds the dispatch prompt for a prompt outbound from the
// harness (criterion 1, F-0152): the user's intent, the template body of the
// chosen type, and a live board snapshot only cerne has — active works, live
// features and the lint debt count. Deterministic concatenation by design
// (spec fork): the value is the live data, not rewriting the intent.
func Compose(repoDir, projDir, typ, text string) (string, error) {
	return composeInner(repoDir, projDir, typ, text, "")
}

func composeInner(repoDir, projDir, typ, text, registeredWork string) (string, error) {
	notes, err := store.LoadAllNotes(repoDir)
	if err != nil {
		return "", err
	}
	var tmpl *store.Note
	for i, n := range notes {
		if n.State != domain.NoteActive {
			continue
		}
		if slug.Make(n.Title) != typ {
			continue
		}
		for _, c := range n.Components {
			if c == promptTemplateComponent {
				tmpl = &notes[i]
				break
			}
		}
	}
	if tmpl == nil {
		types, terr := ListTypes(repoDir)
		if terr != nil {
			return "", terr
		}
		slugs := make([]string, len(types))
		for i, t := range types {
			slugs[i] = t.Slug
		}
		return "", fmt.Errorf("%w: %q — available: %s", ErrUnknownType, typ, strings.Join(slugs, ", "))
	}
	body, err := store.LoadNoteContent(repoDir, tmpl.ID)
	if err != nil {
		return "", err
	}
	body = stripFrontmatter(body)

	cfg, _, cfgErr := project.LoadConfig(projDir)
	projectID := "?"
	staleReadyDays, staleWorkHours := 0, 0
	var ignore []string
	if cfgErr == nil {
		projectID = cfg.ProjectID
		staleReadyDays = cfg.StaleReadyWindowDays()
		staleWorkHours = cfg.StaleWorkWindowHours()
		ignore = cfg.UnregisteredSourceIgnore
	}

	works, err := store.LoadAllWorks(repoDir)
	if err != nil {
		return "", err
	}
	features, err := store.LoadAllFeatures(repoDir)
	if err != nil {
		return "", err
	}
	findings, err := lint.RunConfig(repoDir, staleReadyDays, staleWorkHours, ignore...)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "## Intenção\n\n%s\n\n", strings.TrimSpace(text))
	fmt.Fprintf(&b, "## Orientações — %s\n\n%s\n\n", tmpl.Title, strings.TrimSpace(body))
	fmt.Fprintf(&b, "## Estado do board (%s)\n\n", projectID)

	b.WriteString("Works ativos:\n")
	activeWorks := 0
	for _, w := range works {
		if w.State != domain.WorkActive {
			continue
		}
		activeWorks++
		line := fmt.Sprintf("- %s [%s] %s", w.ID, w.Actor, w.Title)
		if len(w.Refs) > 0 {
			line += fmt.Sprintf(" (refs: %s)", strings.Join(w.Refs, ", "))
		}
		b.WriteString(line + "\n")
	}
	if activeWorks == 0 {
		b.WriteString("- nenhum\n")
	}

	b.WriteString("\nFeatures vivas:\n")
	liveFeatures := 0
	for _, f := range features {
		switch f.State {
		case domain.StateCandidate, domain.StateApproved, domain.StateInDevelopment:
			liveFeatures++
			fmt.Fprintf(&b, "- %s %s — %s\n", f.ID, f.State, f.Title)
		}
	}
	if liveFeatures == 0 {
		b.WriteString("- nenhuma\n")
	}

	fmt.Fprintf(&b, "\nDébitos de lint: %d\n\n", len(findings))

	b.WriteString("## Registro\n\n")
	if registeredWork != "" {
		fmt.Fprintf(&b, "Trabalho já registrado para este prompt: %s. Ao concluir, feche com 'cerne work done %s'.\n", registeredWork, registeredWork)
	} else {
		b.WriteString("Nenhum work registrado para este prompt — ao começar, rode 'cerne work start \"<descrição>\" --ref <F-...>' e feche com 'cerne work done <W-...>'.\n")
	}
	return b.String(), nil
}
