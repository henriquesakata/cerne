package cost

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// AgentRole is one subagent classified by its transcript.
type AgentRole struct {
	Session string `json:"session"`
	Agent   string `json:"agent"`
	Role    string `json:"role"`
	Bound   bool   `json:"bound"` // a retro bind already records the role
}

// FeatureRoles is what the backfill would add to one feature's anchor.
type FeatureRoles struct {
	ID     string              `json:"id"`
	State  domain.FeatureState `json:"state"`
	Critic Counters            `json:"critic"`
	Judge  Counters            `json:"judge"`
	Agents []AgentRole         `json:"agents"`
	// NeedsAnchor: the anchor lacks the roles (else only binds are missing).
	NeedsAnchor bool `json:"needs_anchor"`
}

// Ambiguity is a classified subagent whose messages fall on more than one
// entity: never written.
type Ambiguity struct {
	Session  string   `json:"session"`
	Agent    string   `json:"agent"`
	Role     string   `json:"role"`
	Entities []string `json:"entities"`
	Reason   string   `json:"reason"`
}

// Why a subagent is ambiguous (Ambiguity.Reason).
const (
	reasonBinding = "binding changed while it ran"            // F-0396 crit. 10
	reasonCites   = "prompt cites another feature"            // F-0411 crit. 1
	reasonMixed   = "binding has prompts of several features" // F-0411 crit. 2
)

// Backfill is the report of BackfillRoles.
type Backfill struct {
	Features     []FeatureRoles `json:"features"`
	Unattributed int            `json:"unattributed"`
	Ambiguous    []Ambiguity    `json:"ambiguous"`
	Partial      []AgentRole    `json:"partial"` // counted, but only the share some binding governs
	HasRoles     []string       `json:"has_roles"`
	NoAnchor     []string       `json:"no_anchor"`
	Inconsistent []string       `json:"inconsistent"`
}

type agentKey struct{ sess, agent string }

// agentGroup is one classified subagent's messages, as BackfillRoles reads them.
type agentGroup struct {
	class    string
	cited    []string // the feature ids its first prompt cites
	entities map[string]bool
	c        Counters // the messages some binding governs
	first    time.Time
	bound    bool // a bind already gives it its critic or judge role
	skip     bool // a bind gives it another role: never touched
	partial  bool // some messages are ungoverned or after the entity closed
}

// BackfillRoles classifies the subagents of the ledger's Claude Code sessions
// by their transcripts (classifyRole) and reports, per feature, the critic and
// judge cost the feature's anchor lacks (F-0396). Each subagent counts on the
// entity its parent session's binding gives it. The prompt never picks the entity;
// it only takes a subagent out, to ambiguous (F-0411). It writes nothing. since drops the subagents whose first message is
// older.
func BackfillRoles(in Inputs, since time.Time) (Backfill, error) {
	var out Backfill
	binds, exists, err := readLedger(in.CacheDir)
	if err != nil || !exists {
		return out, err
	}
	att, err := newAttributor(in.RepoDir)
	if err != nil {
		return out, err
	}
	bySess := map[string][]Bind{}
	for _, b := range binds {
		bySess[b.Session] = append(bySess[b.Session], b)
	}
	var sessions []string
	for sess, bs := range bySess {
		if !slices.ContainsFunc(bs, func(b Bind) bool { return b.Harness != HarnessClaudeCode }) {
			sessions = append(sessions, sess)
			sort.SliceStable(bs, func(i, j int) bool { return bs[i].TS.Before(bs[j].TS) })
		}
	}
	sort.Strings(sessions)
	files, err := sessionFiles(in.Roots, sessions)
	if err != nil {
		return out, err
	}
	groups := map[agentKey]*agentGroup{}
	seen := map[string]bool{}
	for _, sess := range sessions {
		msgs, _, err := readSession(sess, files[sess])
		if err != nil {
			return out, err
		}
		spawn := spawnTimes(msgs)
		for _, m := range msgs {
			if m.agent == "" || m.class == "" || (m.key != "" && seen[m.key]) {
				continue
			}
			seen[m.key] = m.key != ""
			g := groups[agentKey{sess, m.agent}]
			if g == nil {
				g = &agentGroup{class: m.class, cited: workIDs(m.cited), entities: map[string]bool{}, first: m.ts}
				groups[agentKey{sess, m.agent}] = g
			}
			if !m.ts.IsZero() && (g.first.IsZero() || m.ts.Before(g.first)) {
				g.first = m.ts
			}
			b, ok := governing(bySess[sess], m, spawn[m.agent])
			switch {
			case !ok || att.closed(b, m.ts):
				g.partial = true
				continue
			case b.Role != "" && b.Role != "critic" && b.Role != "judge":
				g.skip = true
				continue
			case b.Role != "":
				g.class, g.bound = b.Role, true
			case !b.Retro:
				// The parent's main bind moving while the subagent runs (a
				// TL rebinding with a judge in flight) puts it on two entities.
				if e, ok := mainAt(bySess[sess], m.ts); ok {
					g.entities[att.res.resolve(e)] = true
				}
			}
			g.entities[att.res.resolve(b.Entity)] = true
			g.c = g.c.add(m.c)
		}
	}
	keys := make([]agentKey, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i].sess < keys[j].sess || keys[i].sess == keys[j].sess && keys[i].agent < keys[j].agent
	})
	// The prompt only ever takes a subagent out of a feature (F-0411): the
	// ids the unbound subagents of one session under one binding cite, summed.
	type bindKey struct{ sess, entity string }
	cites := map[bindKey]map[string]bool{}
	for _, k := range keys {
		if g := groups[k]; !g.skip && !g.bound && len(g.entities) == 1 {
			bk := bindKey{k.sess, slices.Sorted(maps.Keys(g.entities))[0]}
			if cites[bk] == nil {
				cites[bk] = map[string]bool{}
			}
			for _, id := range g.cited {
				cites[bk][id] = true
			}
		}
	}
	byFeature := map[string]*FeatureRoles{}
	for _, k := range keys {
		g := groups[k]
		if g.skip || (!since.IsZero() && g.first.Before(since)) {
			continue
		}
		ents := slices.Sorted(maps.Keys(g.entities))
		ar := AgentRole{Session: k.sess, Agent: k.agent, Role: g.class, Bound: g.bound}
		reason, listed := reasonBinding, ents
		if len(ents) == 1 && !g.bound && domain.IsFeatureID(ents[0]) && !domain.IsEpicID(ents[0]) { // an epic binding keeps its critic (F-0411)
			all := cites[bindKey{k.sess, ents[0]}]
			switch {
			case len(g.cited) > 0 && !slices.Contains(g.cited, ents[0]):
				reason = reasonCites
			case len(all) > 1 && !slices.Equal(g.cited, ents):
				reason = reasonMixed
				g.cited = slices.Sorted(maps.Keys(all)) // the blind judge lists what its siblings cite
			}
			if reason != reasonBinding {
				listed = slices.Compact(slices.Sorted(slices.Values(append(slices.Clone(ents), g.cited...))))
			}
		}
		switch {
		case len(listed) > 1:
			out.Ambiguous = append(out.Ambiguous, Ambiguity{Session: k.sess, Agent: k.agent, Role: g.class, Entities: listed, Reason: reason})
		case len(ents) == 0 || !domain.IsFeatureID(ents[0]):
			out.Unattributed++
		default:
			f := byFeature[ents[0]]
			if f == nil {
				f = &FeatureRoles{ID: ents[0]}
				byFeature[ents[0]] = f
			}
			f.Agents = append(f.Agents, ar)
			if g.class == "critic" {
				f.Critic = f.Critic.add(g.c)
			} else {
				f.Judge = f.Judge.add(g.c)
			}
			if g.partial {
				out.Partial = append(out.Partial, ar)
			}
		}
	}
	for _, id := range slices.Sorted(maps.Keys(byFeature)) {
		f := byFeature[id]
		feat, err := store.LoadFeature(in.RepoDir, id)
		a, ok := readAnchor(in.RepoDir, id)
		switch {
		case err != nil || !ok || (feat.State != domain.StateImplemented && feat.State != domain.StateDone):
			out.NoAnchor = append(out.NoAnchor, id)
		case a.ByRole["critic"] != (Counters{}) || a.ByRole["judge"] != (Counters{}):
			if slices.ContainsFunc(f.Agents, func(a AgentRole) bool { return !a.Bound }) {
				f.State = feat.State // only the binds are missing
				out.Features = append(out.Features, *f)
			} else {
				out.HasRoles = append(out.HasRoles, id)
			}
		default:
			if _, ok := mergeRoles(a, f.Critic, f.Judge); !ok {
				out.Inconsistent = append(out.Inconsistent, id)
				continue
			}
			f.State, f.NeedsAnchor = feat.State, true
			out.Features = append(out.Features, *f)
		}
	}
	return out, nil
}

// mainAt is the entity of the last main-thread bind at or before ts.
func mainAt(binds []Bind, ts time.Time) (string, bool) {
	id := ""
	for _, b := range binds {
		if b.Agent == "" && !b.Retro && !b.TS.After(ts) {
			id = b.Entity
		}
	}
	return id, id != ""
}

// mergeRoles is the anchor's by_role with the critic and judge counters added,
// moved out of "other" and "unknown", where an anchor stamped before this
// classification counted them. false when the roles would then sum above the
// anchor's own counters: the durable total never goes down, so the feature is
// left alone.
func mergeRoles(a share, critic, judge Counters) (map[string]Counters, bool) {
	roles := maps.Clone(a.ByRole)
	if roles == nil {
		roles = map[string]Counters{}
	}
	moved := critic.add(judge)
	for _, from := range []string{"other", "unknown"} {
		if old, ok := roles[from]; ok {
			take := old.min(moved)
			moved = moved.sub(take)
			if left := old.sub(take); left == (Counters{}) {
				delete(roles, from)
			} else {
				roles[from] = left
			}
		}
	}
	roles["critic"], roles["judge"] = critic, judge
	var sum Counters
	for _, c := range roles {
		sum = sum.add(c)
	}
	if sum.max(a.Counters) != a.Counters {
		return nil, false
	}
	return roles, true
}

var (
	// ErrBackfillNoAnchor: the feature has no "## Custo" anchor to complete.
	ErrBackfillNoAnchor = errors.New("the feature has no cost anchor to complete")
	// ErrBackfillInconsistent: critic plus judge would sum above the anchor's total.
	ErrBackfillInconsistent = errors.New("critic and judge tokens exceed the anchor's total")
)

// RolesEdit adds the critic and judge counters to id's newest cost anchor
// (F-0396), keeping its counters. For an implemented feature whose anchor is
// in the result it returns the whole new result (asResult); otherwise the text
// of an erratum carrying the same "## Custo" block with the roles — the
// result of a done feature stays as it is.
func RolesEdit(repoDir, id string, done bool, critic, judge Counters) (text string, asResult bool, err error) {
	section, a, inResult, ok := newestCusto(repoDir, id)
	if !ok {
		return "", false, fmt.Errorf("%w: %s", ErrBackfillNoAnchor, id)
	}
	roles, ok := mergeRoles(a, critic, judge)
	if !ok {
		return "", false, fmt.Errorf("%w: %s", ErrBackfillInconsistent, id)
	}
	a.ByRole = roles
	section = putLine(section, rolesLineRe, breakdownLine("Por papel", roles), afterRolesRe)
	return custoEdit(repoDir, id, done, inResult, section, a,
		"Custo por papel recuperado das transcrições (`cerne cost backfill-roles`, F-0396): o anchor vigente, com o by_role.")
}

// FeatureEffort is what the effort backfill would add to one feature's anchor.
type FeatureEffort struct {
	ID           string              `json:"id"`
	State        domain.FeatureState `json:"state"`
	ByEffort     map[string]Counters `json:"by_effort"`
	ModelEfforts map[string][]string `json:"model_efforts"`
}

// EffortBackfill is the report of BackfillEffort.
type EffortBackfill struct {
	Features     []FeatureEffort `json:"features"`
	HasEffort    []string        `json:"has_effort"`
	Drifted      []string        `json:"drifted"`       // recomputed counters differ from the anchor's
	NoTranscript []string        `json:"no_transcript"` // nothing left to recompute from
	Unmeasured   []string        `json:"unmeasured"`    // same counters, but no line records an effort
}

// BackfillEffort reports, per implemented or done feature whose newest anchor
// lacks by_effort (stamped before B-0024), the measured effort the
// transcripts still on disk hold (F-0448), read in one pass of the project
// view — the attribution Compute makes per feature. A feature counts only when
// the recomputed counters equal the anchor's: the effort is a partition of the
// stamped tokens; else it is named by why it is left out. It writes nothing.
func BackfillEffort(in Inputs) (EffortBackfill, error) {
	var out EffortBackfill
	feats, err := store.LoadAllFeatures(in.RepoDir)
	if err != nil {
		return out, err
	}
	proj, err := own(in, "")
	if err != nil {
		return out, err
	}
	sort.Slice(feats, func(i, j int) bool { return feats[i].ID < feats[j].ID })
	for _, f := range feats {
		if f.State != domain.StateImplemented && f.State != domain.StateDone {
			continue
		}
		a, ok := readAnchor(in.RepoDir, f.ID)
		if !ok {
			continue
		}
		s := proj.perEntity[f.ID]
		switch {
		case a.ByEffort != nil:
			out.HasEffort = append(out.HasEffort, f.ID)
		case s == nil || s.Messages == 0 || slices.Contains(proj.FromStamp, f.ID): // pruned since the stamp: the anchor is all there is
			out.NoTranscript = append(out.NoTranscript, f.ID)
		case s.Counters != a.Counters:
			out.Drifted = append(out.Drifted, f.ID)
		case len(s.ByEffort) == 0:
			out.Unmeasured = append(out.Unmeasured, f.ID)
		default:
			byEffort := maps.Clone(s.ByEffort)
			unknownShare(byEffort, s.Counters) // lines from before Claude Code recorded the effort
			out.Features = append(out.Features, FeatureEffort{ID: f.ID, State: f.State, ByEffort: byEffort, ModelEfforts: s.ModelEfforts})
		}
	}
	return out, nil
}

// ErrBackfillEffortMismatch: the efforts do not partition the anchor's total.
var ErrBackfillEffortMismatch = errors.New("the efforts do not sum to the anchor's total")

// EffortEdit adds the measured effort (by_effort, model_efforts and the "Por
// esforço:" line) to id's newest cost anchor (F-0448), keeping everything
// else; where it goes is RolesEdit's rule. byEffort must sum to the anchor's
// counters: the efforts are a partition of the same tokens.
func EffortEdit(repoDir, id string, done bool, byEffort map[string]Counters, modelEfforts map[string][]string) (text string, asResult bool, err error) {
	section, a, inResult, ok := newestCusto(repoDir, id)
	if !ok {
		return "", false, fmt.Errorf("%w: %s", ErrBackfillNoAnchor, id)
	}
	var sum Counters
	for _, c := range byEffort {
		sum = sum.add(c)
	}
	if sum != a.Counters {
		return "", false, fmt.Errorf("%w: %s", ErrBackfillEffortMismatch, id)
	}
	a.ByEffort, a.ModelEfforts = byEffort, modelEfforts
	section = putLine(section, effortLineRe, breakdownLine("Por esforço", byEffort), modelLineRe)
	return custoEdit(repoDir, id, done, inResult, section, a,
		"Esforço do modelo recuperado das transcrições (`cerne cost backfill-effort`, F-0448): o anchor vigente, com o by_effort.")
}

// custoEdit writes anchor a over the last anchor line of section, then places
// the section: the whole new result for an implemented feature whose newest
// anchor is in its result (asResult), else an erratum text — intro, then the
// block; the result of a done feature is immutable.
func custoEdit(repoDir, id string, done, inResult bool, section string, a share, intro string) (string, bool, error) {
	anchorJSON, err := json.Marshal(a)
	if err != nil {
		return "", false, err
	}
	if m := anchorRe.FindAllStringIndex(section, -1); len(m) > 0 {
		last := m[len(m)-1]
		section = section[:last[0]] + "<!-- cerne:cost v1 " + string(anchorJSON) + " -->" + section[last[1]:]
	}
	if inResult && !done {
		raw, err := os.ReadFile(filepath.Join(repoDir, "content", "features", id, "result.md"))
		if err != nil {
			return "", false, err
		}
		start, end, _, _ := custoSection(string(raw))
		return string(raw[:start]) + section + string(raw[end:]), true, nil
	}
	return intro + "\n\n" + section, false, nil
}

// putLine replaces section's line matching re with line, else inserts line
// after the last line matching after.
func putLine(section string, re *regexp.Regexp, line string, after *regexp.Regexp) string {
	if re.MatchString(section) {
		return re.ReplaceAllLiteralString(section, line)
	}
	if m := after.FindAllStringIndex(section, -1); len(m) > 0 {
		i := m[len(m)-1][1]
		return section[:i] + line + section[i:]
	}
	return section
}

var (
	rolesLineRe  = regexp.MustCompile(`(?m)^Por papel: .*\n`)
	effortLineRe = regexp.MustCompile(`(?m)^Por esforço: .*\n`)
	modelLineRe  = regexp.MustCompile(`(?m)^Por modelo: .*\n`)
	afterRolesRe = regexp.MustCompile(`(?m)^Por (?:modelo|esforço): .*\n`) // the block's order: model, effort, role
)

// workIDs drops the epics from cited ids: naming the parent epic is not
// citing other work (B-0021); another bug, task or feature is.
func workIDs(cited []string) []string {
	var out []string
	for _, id := range cited {
		if !domain.IsEpicID(id) {
			out = append(out, id)
		}
	}
	return out
}
