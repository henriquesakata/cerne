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
	"strings"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// AgentRole is one subagent classified by its transcript.
type AgentRole struct {
	Session string `json:"session"`
	Agent   string `json:"agent"`
	Role    string `json:"role"`
}

// FeatureRoles is what the backfill would add to one feature's anchor.
type FeatureRoles struct {
	ID     string              `json:"id"`
	State  domain.FeatureState `json:"state"`
	Critic Counters            `json:"critic"`
	Judge  Counters            `json:"judge"`
	Agents []AgentRole         `json:"agents"`
}

// Ambiguity is a classified subagent whose messages fall on more than one
// entity: never written.
type Ambiguity struct {
	Session  string   `json:"session"`
	Agent    string   `json:"agent"`
	Role     string   `json:"role"`
	Entities []string `json:"entities"`
}

// Backfill is the report of BackfillRoles.
type Backfill struct {
	Features     []FeatureRoles `json:"features"`
	Unattributed int            `json:"unattributed"`
	Ambiguous    []Ambiguity    `json:"ambiguous"`
	HasRoles     []string       `json:"has_roles"`
	NoAnchor     []string       `json:"no_anchor"`
	Inconsistent []string       `json:"inconsistent"`
}

type agentKey struct{ sess, agent string }

// agentGroup is one classified subagent's messages, as BackfillRoles reads them.
type agentGroup struct {
	class    string
	entities map[string]bool
	c        Counters // the messages some binding governs
	first    time.Time
	hasRole  bool // a bind already gave it a role
}

// BackfillRoles classifies the subagents of the ledger's Claude Code sessions
// by their transcripts (classifyRole) and reports, per feature, the critic and
// judge cost the feature's anchor lacks (F-0396). Each subagent counts on the
// entity its parent session's binding gives it — the prompt is never read for
// an id. It writes nothing. since drops the subagents whose first message is
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
		msgs, err := readSession(sess, files[sess])
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
				g = &agentGroup{class: m.class, entities: map[string]bool{}, first: m.ts}
				groups[agentKey{sess, m.agent}] = g
			}
			if !m.ts.IsZero() && (g.first.IsZero() || m.ts.Before(g.first)) {
				g.first = m.ts
			}
			b, ok := governing(bySess[sess], m, spawn[m.agent])
			switch {
			case !ok || att.closed(b, m.ts):
			case b.Role != "":
				g.hasRole = true
			default:
				g.entities[att.res.resolve(b.Entity)] = true
				g.c = g.c.add(m.c)
			}
		}
	}
	keys := make([]agentKey, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i].sess < keys[j].sess || keys[i].sess == keys[j].sess && keys[i].agent < keys[j].agent
	})
	byFeature := map[string]*FeatureRoles{}
	for _, k := range keys {
		g := groups[k]
		if g.hasRole || (!since.IsZero() && g.first.Before(since)) {
			continue
		}
		var ents []string
		for e := range g.entities {
			if domain.IsFeatureID(e) {
				ents = append(ents, e)
			}
		}
		sort.Strings(ents)
		switch len(ents) {
		case 0:
			out.Unattributed++
		case 1:
			f := byFeature[ents[0]]
			if f == nil {
				f = &FeatureRoles{ID: ents[0]}
				byFeature[ents[0]] = f
			}
			f.Agents = append(f.Agents, AgentRole{Session: k.sess, Agent: k.agent, Role: g.class})
			if g.class == "critic" {
				f.Critic = f.Critic.add(g.c)
			} else {
				f.Judge = f.Judge.add(g.c)
			}
		default:
			out.Ambiguous = append(out.Ambiguous, Ambiguity{Session: k.sess, Agent: k.agent, Role: g.class, Entities: ents})
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
			out.HasRoles = append(out.HasRoles, id)
		default:
			if _, ok := mergeRoles(a, f.Critic, f.Judge); !ok {
				out.Inconsistent = append(out.Inconsistent, id)
				continue
			}
			f.State = feat.State
			out.Features = append(out.Features, *f)
		}
	}
	return out, nil
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
	anchorJSON, err := json.Marshal(a)
	if err != nil {
		return "", false, err
	}
	if m := anchorRe.FindAllStringIndex(section, -1); len(m) > 0 {
		last := m[len(m)-1]
		section = section[:last[0]] + "<!-- cerne:cost v1 " + string(anchorJSON) + " -->" + section[last[1]:]
	}
	var parts []string
	for _, role := range slices.Sorted(maps.Keys(roles)) {
		parts = append(parts, role+" ("+countersLine(roles[role])+")")
	}
	line := "Por papel: " + strings.Join(parts, "; ") + ".\n"
	if rolesLineRe.MatchString(section) {
		section = rolesLineRe.ReplaceAllLiteralString(section, line)
	} else if i := modelLineRe.FindStringIndex(section); i != nil {
		section = section[:i[1]] + line + section[i[1]:]
	}
	if inResult && !done {
		raw, err := os.ReadFile(filepath.Join(repoDir, "content", "features", id, "result.md"))
		if err != nil {
			return "", false, err
		}
		start, end, _, _ := custoSection(string(raw))
		return string(raw[:start]) + section + string(raw[end:]), true, nil
	}
	return "Custo por papel recuperado das transcrições (`cerne cost backfill-roles`, F-0396): o anchor vigente, com o by_role.\n\n" + section, false, nil
}

var (
	rolesLineRe = regexp.MustCompile(`(?m)^Por papel: .*\n`)
	modelLineRe = regexp.MustCompile(`(?m)^Por modelo: .*\n`)
)
