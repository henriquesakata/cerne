package app

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/henriquesakata/cerne/internal/doccheck"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/index"
	"github.com/henriquesakata/cerne/internal/mdlink"
	"github.com/henriquesakata/cerne/internal/store"
)

// The task package (E-0005): what a feature, epic, task or bug needs to start,
// within a budget — its stage document whole, its epic, and its neighbourhood
// in four rings, each item an id, a title and a one-line gist, never a body:
//
//	(a) what its documents and its epic's cite by [[id]], or what informs them;
//	(b) active notes whose edit: trigger, or whose cited file, matches a path
//	    its spec or plan cites;
//	(c) active notes sharing a component with it, the least used one first;
//	(d) up to five active notes the search index ranks for its title and the
//	    first paragraph of its problem.
//
// It reads the bodies of the task and its epic only: the rest is entity
// metadata and the search index, opened read-only and never built
// (criterion 16). No model, no network. TaskIndex, what the binding points
// print, is the notes of rings (a) to (c).

// DefaultContextBudget is the package's ceiling in estimated tokens.
const DefaultContextBudget = 8000

// contentRing caps ring (d).
const contentRing = 5

// ErrNotATask refuses a package for an id outside the feature family.
var ErrNotATask = errors.New("a task package is for a feature, epic, task or bug")

// docSlots are a feature's document slots, in reading order.
var docSlots = []string{"spec", "plan", "result"}

// ContextItem is one neighbour of the package. Why is what brought a ring (b)
// or (c) item in: the trigger or the cited path, the shared component.
// Sources are the current references informing a note.
type ContextItem struct {
	ID      string   `json:"id"`
	Kind    string   `json:"kind"`
	Title   string   `json:"title"`
	State   string   `json:"state"`
	Gist    string   `json:"gist,omitempty"`
	Ring    string   `json:"ring"`
	Why     string   `json:"why,omitempty"`
	Sources []string `json:"sources,omitempty"`
}

// ContextRef names the epic of the package.
type ContextRef struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// ContextDoc is the task's document of its stage, whole.
type ContextDoc struct {
	Name string `json:"name"`
	Body string `json:"body"`
}

// OnDemand counts what shares a component with the task and stays out of the
// package, with the command that lists it ("" when the count is zero).
type OnDemand struct {
	What    string `json:"what"`
	Count   int    `json:"count"`
	Command string `json:"command,omitempty"`
}

// TaskPack is the package of one task. Compact means the budget cut the
// items to id and title; Omitted counts, per ring, the items it left out.
type TaskPack struct {
	ID         string         `json:"id"`
	Title      string         `json:"title"`
	State      string         `json:"state"`
	Epic       *ContextRef    `json:"epic,omitempty"`
	Doc        ContextDoc     `json:"doc"`
	Items      []ContextItem  `json:"items"`
	Components []string       `json:"components"`
	OnDemand   []OnDemand     `json:"on_demand"`
	Budget     int            `json:"budget"`
	Compact    bool           `json:"compact"`
	Omitted    map[string]int `json:"omitted,omitempty"`
}

// TaskContext assembles the package of id within budget tokens (<= 0: the
// default).
func TaskContext(repoDir, id string, budget int) (TaskPack, error) {
	if budget <= 0 {
		budget = DefaultContextBudget
	}
	t, err := loadTask(repoDir, id)
	if err != nil {
		return TaskPack{}, err
	}
	notes, err := store.LoadAllNotes(repoDir)
	if err != nil {
		return TaskPack{}, err
	}
	refs, err := store.LoadAllReferences(repoDir)
	if err != nil {
		return TaskPack{}, err
	}
	p := TaskPack{ID: t.f.ID, Title: t.f.Title, State: string(t.f.State), Items: t.neighbourhood(repoDir, notes, refs, true),
		Components: t.f.Components, Budget: budget}
	if p.Components == nil {
		p.Components = []string{}
	}
	if t.epic != nil {
		p.Epic = &ContextRef{ID: t.epic.ID, Title: t.epic.Title}
	}
	if name := StageDoc(t.f.State, slices.Collect(maps.Keys(t.docs))); name != "" {
		p.Doc = ContextDoc{Name: name, Body: t.docs[name]}
	}
	if p.OnDemand, err = onDemand(repoDir, t.f.Components, refs, p.Items); err != nil {
		return TaskPack{}, err
	}
	p.fit(budget)
	return p, nil
}

// TaskIndex is what a binding point prints (criterion 9): the notes of rings
// (a) to (c), in ring order. It loads no feature but the task and its epic.
func TaskIndex(repoDir, id string) ([]ContextItem, error) {
	t, err := loadTask(repoDir, id)
	if err != nil {
		return nil, err
	}
	notes, err := store.LoadAllNotes(repoDir)
	if err != nil {
		return nil, err
	}
	return t.neighbourhood(repoDir, notes, nil, false), nil
}

// StageDoc is the document of a feature's stage (F-0338) among the real ones
// in present: the most advanced up to the state's — the plan in development,
// the result once implemented or done, the spec otherwise; "" for none.
func StageDoc(state domain.FeatureState, present []string) string {
	upTo := 0
	switch state {
	case domain.StateInDevelopment:
		upTo = 1
	case domain.StateImplemented, domain.StateDone:
		upTo = 2
	}
	pick := ""
	for i, name := range docSlots {
		if i <= upTo && slices.Contains(present, name) {
			pick = name
		}
	}
	return pick
}

// task is the feature a package is for, with its epic and the real documents
// of both: the only bodies a package reads.
type task struct {
	f        store.Feature
	docs     map[string]string
	epic     *store.Feature
	epicDocs map[string]string
}

func loadTask(repoDir, id string) (task, error) {
	id = domain.CanonicalID(id)
	if !domain.IsFeatureID(id) {
		return task{}, fmt.Errorf("%s: %w", id, ErrNotATask)
	}
	f, err := store.LoadFeature(repoDir, id)
	if err != nil {
		return task{}, err
	}
	t := task{f: f, docs: realDocs(repoDir, id)}
	if f.Parent != "" {
		if e, err := store.LoadFeature(repoDir, f.Parent); err == nil {
			t.epic, t.epicDocs = &e, realDocs(repoDir, e.ID)
		}
	}
	return t, nil
}

// realDocs reads a feature's documents with real content, by slot.
func realDocs(repoDir, id string) map[string]string {
	out := map[string]string{}
	for _, name := range docSlots {
		data, err := os.ReadFile(FeatureDocPath(repoDir, id, name+".md"))
		if err == nil && doccheck.HasRealContent(data) {
			out[name] = strings.TrimRight(string(data), "\n")
		}
	}
	return out
}

// neighbourhood lists rings (a) to (c), and (d) when full; without full only
// notes are listed and no other entity is loaded.
func (t task) neighbourhood(repoDir string, notes []store.Note, refs []store.Reference, full bool) []ContextItem {
	byID := make(map[string]store.Note, len(notes))
	for _, n := range notes {
		byID[n.ID] = n
	}
	sources := map[string][]string{}
	for _, r := range refs {
		if r.State != domain.ReferenceCurrent {
			continue
		}
		for _, to := range r.Informs {
			sources[to] = append(sources[to], r.ID)
		}
	}
	seen := map[string]bool{t.f.ID: true}
	if t.epic != nil {
		seen[t.epic.ID] = true
	}
	var out []ContextItem
	add := func(id, ring, why string) {
		if seen[id] {
			return
		}
		it := ContextItem{ID: id, Ring: ring, Why: why}
		if n, ok := byID[id]; ok {
			it.Kind, it.Title, it.State, it.Gist, it.Sources = "note", n.Title, string(n.State), n.Gist, sources[id]
		} else if !full {
			return
		} else if domain.IsFeatureID(id) {
			f, err := store.LoadFeature(repoDir, id)
			if err != nil || !featureLive(f.State) { // concluded work is in the code (criterion 19)
				return
			}
			it.Kind, it.Title, it.State = "feature", f.Title, string(f.State)
		} else {
			m, kind, err := store.LoadMeta(repoDir, id)
			if err != nil {
				return
			}
			it.Kind, it.Title, it.State = kind, m.Title, m.State
		}
		seen[id] = true
		out = append(out, it)
	}

	// (a) the task's own links before its epic's.
	owners := []struct {
		id   string
		docs map[string]string
	}{{t.f.ID, t.docs}}
	if t.epic != nil {
		owners = append(owners, struct {
			id   string
			docs map[string]string
		}{t.epic.ID, t.epicDocs})
	}
	for _, o := range owners {
		for _, name := range docSlots {
			cited, _ := mdlink.WikiLinks(o.docs[name], func(target string) (string, bool) {
				id := mdlink.CanonicalID(target)
				return id, (full || strings.HasPrefix(id, "N-")) && store.EntityExists(repoDir, id)
			})
			for _, id := range cited {
				add(id, "a", "")
			}
		}
		for _, n := range notes {
			if slices.Contains(n.Informs, o.id) {
				add(n.ID, "a", "")
			}
		}
		for _, r := range refs {
			if slices.Contains(r.Informs, o.id) {
				add(r.ID, "a", "")
			}
		}
	}

	// (b) a trigger before a citation, the more specific first.
	dbPath := filepath.Join(filepath.Dir(repoDir), "index.db")
	paths := citedPaths(t.docs["spec"] + "\n" + t.docs["plan"])
	type hit struct {
		id, why string
		score   int
	}
	var hits []hit
	for _, n := range notes {
		if n.State != domain.NoteActive {
			continue
		}
		h := hit{id: n.ID}
		for _, trig := range n.When {
			for _, p := range paths {
				if s := domain.PathTriggerScore(trig, p); s+3 > h.score {
					h.score, h.why = s+3, trig
				}
			}
		}
		if h.score > 3 {
			hits = append(hits, h)
		}
	}
	for id, c := range notesCitingPaths(dbPath, paths) {
		if !slices.ContainsFunc(hits, func(h hit) bool { return h.id == id }) {
			hits = append(hits, hit{id, c.why, c.score})
		}
	}
	slices.SortFunc(hits, func(a, b hit) int {
		if a.score != b.score {
			return b.score - a.score
		}
		return strings.Compare(a.id, b.id)
	})
	for _, h := range hits {
		add(h.id, "b", h.why)
	}

	// (c) the least used shared component first, counted over active notes.
	use := map[string]int{}
	for _, n := range notes {
		if n.State == domain.NoteActive {
			for _, c := range n.Components {
				use[c]++
			}
		}
	}
	var byTag []hit
	for _, n := range notes {
		if n.State != domain.NoteActive || seen[n.ID] {
			continue
		}
		tag := ""
		for _, c := range n.Components {
			if slices.Contains(t.f.Components, c) && (tag == "" || use[c] < use[tag] || use[c] == use[tag] && c < tag) {
				tag = c
			}
		}
		if tag != "" {
			byTag = append(byTag, hit{n.ID, tag, use[tag]})
		}
	}
	slices.SortFunc(byTag, func(a, b hit) int {
		if a.score != b.score {
			return a.score - b.score
		}
		if c := strings.Compare(a.why, b.why); c != 0 {
			return c
		}
		return strings.Compare(a.id, b.id)
	})
	for _, h := range byTag {
		add(h.id, "c", h.why)
	}

	// (d) the index, by the title and the problem.
	if full {
		listed := 0
		for id := range seen {
			if strings.HasPrefix(id, "N-") {
				listed++
			}
		}
		ranked, _ := index.RankActiveNotes(dbPath, t.f.Title+"\n"+problemParagraph(t.docs["spec"]), contentRing+listed)
		kept := 0
		for _, id := range ranked {
			if kept < contentRing && !seen[id] {
				add(id, "d", "")
				kept++
			}
		}
	}
	if out == nil {
		out = []ContextItem{}
	}
	return out
}

// backtickRe is an inline code span: where a spec or plan cites a path.
var backtickRe = regexp.MustCompile("`([^`\\s]+)`")

// lineSuffixRe is a ":12" or ":12-30" after a cited path.
var lineSuffixRe = regexp.MustCompile(`:\d+(-\d+)?$`)

// citedPaths are the repository paths cited in code spans of text: a span
// with a directory, without a scheme, a glob or a placeholder.
func citedPaths(text string) []string {
	var out []string
	for _, m := range backtickRe.FindAllStringSubmatch(text, -1) {
		p := strings.TrimSuffix(lineSuffixRe.ReplaceAllString(strings.TrimPrefix(m[1], "./"), ""), "/")
		if !strings.Contains(p, "/") || strings.Contains(p, "://") || strings.ContainsAny(p, "*<>{}$") || slices.Contains(out, p) {
			continue
		}
		out = append(out, p)
	}
	return out
}

type pathCite struct {
	why   string
	score int
}

// notesCitingPaths is NotesForPath for every path in one index query: the
// active notes citing a path whole (3) or by a suffix with a directory (2).
// A missing or busy index cites nothing.
func notesCitingPaths(dbPath string, paths []string) map[string]pathCite {
	cands := map[string]int{}
	for _, p := range paths {
		parts := strings.Split(p, "/")
		cands[p] = 3
		for i := 1; i < len(parts)-1; i++ {
			if s := strings.Join(parts[i:], "/"); cands[s] < 2 {
				cands[s] = 2
			}
		}
	}
	if len(cands) == 0 {
		return nil
	}
	hits, err := index.PhraseNotes(context.Background(), dbPath, slices.Sorted(maps.Keys(cands)))
	if err != nil {
		return nil
	}
	out := map[string]pathCite{}
	for _, h := range hits {
		for p, sc := range cands {
			if sc > out[h.ID].score && (citesPath(h.Body, p) || citesPath(h.Title, p)) {
				out[h.ID] = pathCite{p, sc}
			}
		}
	}
	return out
}

// problemHeadRe opens the problem section, in either docs language.
var problemHeadRe = regexp.MustCompile(`(?im)^##\s+problem`)

// problemParagraph is the first paragraph of a spec's problem section.
func problemParagraph(spec string) string {
	loc := problemHeadRe.FindStringIndex(spec)
	if loc == nil {
		return ""
	}
	var para []string
	for _, line := range strings.Split(spec[loc[1]:], "\n")[1:] {
		line = strings.TrimSpace(line)
		switch {
		case line == "" && len(para) > 0, strings.HasPrefix(line, "#"):
			return strings.Join(para, " ")
		case line != "":
			para = append(para, line)
		}
	}
	return strings.Join(para, " ")
}

// onDemand counts the decisions, references and runbooks in force, and the
// rejected or superseded proposals, sharing a component with the task and
// left out of the package (criterion 19).
func onDemand(repoDir string, comps []string, refs []store.Reference, items []ContextItem) ([]OnDemand, error) {
	listed := map[string]bool{}
	for _, it := range items {
		listed[it.ID] = true
	}
	shares := func(id string, cs []string) bool {
		return !listed[id] && slices.ContainsFunc(cs, func(c string) bool { return slices.Contains(comps, c) })
	}
	var decs, rfs, props int
	var rbs []string
	if len(comps) > 0 {
		ds, err := store.LoadAllDecisions(repoDir)
		if err != nil {
			return nil, err
		}
		for _, d := range ds {
			if d.State == domain.DecisionActive && shares(d.ID, d.Components) {
				decs++
			}
		}
		for _, r := range refs {
			if r.State == domain.ReferenceCurrent && shares(r.ID, r.Components) {
				rfs++
			}
		}
		runbooks, err := store.LoadAllRunbooks(repoDir)
		if err != nil {
			return nil, err
		}
		for _, rb := range runbooks {
			if rb.State == domain.RunbookActive && shares(rb.ID, rb.Components) {
				rbs = append(rbs, rb.ID)
			}
		}
		feats, err := store.LoadAllFeatures(repoDir)
		if err != nil {
			return nil, err
		}
		for _, f := range feats {
			if (f.State == domain.StateRejected || f.State == domain.StateSuperseded) && shares(f.ID, f.Components) {
				props++
			}
		}
	}
	csv := strings.Join(comps, ",")
	cmd := func(n int, c string) string {
		if n == 0 {
			return ""
		}
		return c
	}
	show := "cerne show " + strings.Join(rbs, "")
	if len(rbs) > 1 {
		show = "cerne show <id>: " + strings.Join(rbs, ", ")
	}
	return []OnDemand{
		{What: "decisions", Count: decs, Command: cmd(decs, "cerne decision list --component "+csv)},
		{What: "references", Count: rfs, Command: cmd(rfs, "cerne reference list --component "+csv)},
		{What: "runbooks", Count: len(rbs), Command: cmd(len(rbs), show)},
		{What: "rejected or superseded proposals", Count: props, Command: cmd(props, `cerne search "`+strings.Join(comps, " ")+`"`)},
	}, nil
}

// fit applies the budget (criterion 7): when the text passes it, every item
// is cut to id and title, then items leave from the end — ring (d) first, (a)
// last — until it fits. The document is never cut.
func (p *TaskPack) fit(budget int) {
	limit := budget * 4 // bytes: the ~4 bytes per token of every estimate here
	if len(p.Text()) <= limit {
		return
	}
	all := p.Items
	p.Compact = true
	p.Items, p.Omitted = nil, ringCounts(all)
	room := limit - len(p.Text())
	kept := 0
	for kept < len(all) {
		n := len(all[kept].line(true))
		if n > room {
			break
		}
		room -= n
		kept++
	}
	for {
		p.Items, p.Omitted = all[:kept], ringCounts(all[kept:])
		if kept == 0 || len(p.Text()) <= limit {
			return
		}
		kept--
	}
}

// ringCounts counts items by ring, every ring present.
func ringCounts(items []ContextItem) map[string]int {
	out := map[string]int{"a": 0, "b": 0, "c": 0, "d": 0}
	for _, it := range items {
		out[it.Ring]++
	}
	return out
}

// historyStates mark a linked entity that is no longer in force.
var historyStates = []string{"superseded", "rejected", "retired"}

// line is the item as the package prints it.
func (it ContextItem) line(compact bool) string {
	if compact {
		return fmt.Sprintf("[%s] %s  %s\n", it.Ring, it.ID, it.Title)
	}
	s := fmt.Sprintf("[%s] %s  %s  %s", it.Ring, it.ID, it.Kind, it.Title)
	if slices.Contains(historyStates, it.State) {
		s += " [" + it.State + "]"
	}
	if it.Gist != "" {
		s += " — " + it.Gist
	}
	var tags []string
	if it.Why != "" {
		tags = append(tags, it.Why)
	}
	if len(it.Sources) > 0 {
		tags = append(tags, "sources "+strings.Join(it.Sources, ","))
	}
	if len(tags) > 0 {
		s += "  · " + strings.Join(tags, " · ")
	}
	return s + "\n"
}

// Text is the package as `cerne context` prints it.
func (p TaskPack) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s  [%s]  %s\n", p.ID, p.State, p.Title)
	if p.Epic != nil {
		fmt.Fprintf(&b, "epic: %s  %s\n", p.Epic.ID, p.Epic.Title)
	}
	if p.Doc.Name != "" {
		fmt.Fprintf(&b, "\n## %s\n%s\n", p.Doc.Name, p.Doc.Body)
	}
	b.WriteString("\n## neighbourhood (a: linked · b: a path the spec or plan cites · c: a shared component · d: the search index; read one: cerne show <id>)\n")
	for _, it := range p.Items {
		b.WriteString(it.line(p.Compact))
	}
	if len(p.Items) == 0 && !p.Compact {
		fmt.Fprintf(&b, "(empty: nothing links to %s, and no active note matches its paths, components or terms)\n", p.ID)
	}
	if len(p.Components) == 0 {
		fmt.Fprintf(&b, "\n## on demand\n%s has no component: nothing is counted on demand\n", p.ID)
	} else {
		fmt.Fprintf(&b, "\n## on demand (components %s)\n", strings.Join(p.Components, ", "))
		for _, o := range p.OnDemand {
			if o.Command == "" {
				fmt.Fprintf(&b, "%s: %d\n", o.What, o.Count)
			} else {
				fmt.Fprintf(&b, "%s: %d — %s\n", o.What, o.Count, o.Command)
			}
		}
	}
	if p.Compact {
		o := p.Omitted
		fmt.Fprintf(&b, "the budget of %d tokens cut the list to id and title and left out: a %d · b %d · c %d · d %d — raise --budget, or read one: cerne show <id>\n",
			p.Budget, o["a"], o["b"], o["c"], o["d"])
	}
	return b.String()
}
