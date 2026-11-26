package app

import (
	"slices"
	"strings"
	"time"

	"github.com/henriquesakata/cerne/internal/cost"
	"github.com/henriquesakata/cerne/internal/store"
)

// What a session did on a card (F-0486). The document a session wrote (spec,
// plan, result) and a retro bind's role (critic, judge, builder, other) are
// kinds too: the agent that wrote the spec is often not the one that wrote the
// plan or the result.
const (
	RoleOpened      = "opened"      // ran the command that created the card
	RoleImplemented = "implemented" // bound to the card itself: `cerne feature start`
	RoleWorked      = "worked"      // a retro bind with no role
	RoleWork        = "work"        // a W- that refs the card (`work start`, `handoff take`)
)

// EntityRole is one thing a session — or one of its subagents (Agent) — did
// on the card, at its first time.
type EntityRole struct {
	Kind      string    `json:"kind"`
	Work      string    `json:"work,omitempty"`
	WorkTitle string    `json:"work_title,omitempty"`
	Agent     string    `json:"agent,omitempty"`
	AgentType string    `json:"agent_type,omitempty"`
	At        time.Time `json:"at"`
}

// EntitySession is a session that worked on a card, with where it is.
type EntitySession struct {
	Session string           `json:"session"`
	Harness string           `json:"harness"`
	Roles   []EntityRole     `json:"roles"`
	Info    cost.SessionInfo `json:"info"`
}

// EntitySessions lists the sessions of this machine that worked on card id —
// its opener, the writers of its documents, the sessions bound to it, the
// works that ref it — in order of
// their first touch. Subagents fold into their main session: a finished
// subagent hears nothing, its session does. roots hold Claude Code's
// transcripts, cursorRoots Cursor's (F-0513).
func EntitySessions(cacheDir, repoDir, id string, roots, cursorRoots []string) ([]EntitySession, error) {
	binds, err := cost.ReadBinds(cacheDir)
	if err != nil {
		return nil, err
	}
	marks, err := cost.ReadMarks(cacheDir)
	if err != nil {
		return nil, err
	}
	works, err := store.LoadAllWorks(repoDir)
	if err != nil {
		return nil, err
	}
	refsCard := map[string]string{} // W- id → title, for the works that ref the card
	for _, w := range works {
		if slices.Contains(w.Refs, id) {
			refsCard[w.ID] = w.Title
		}
	}

	byID := map[string]*EntitySession{}
	var order []string
	add := func(b cost.Bind, r EntityRole) {
		s := byID[b.Session]
		if s == nil {
			s = &EntitySession{Session: b.Session, Harness: b.Harness}
			byID[b.Session] = s
			order = append(order, b.Session)
		}
		r.Agent, r.At = b.Agent, b.TS
		for i, have := range s.Roles {
			if have.Kind == r.Kind && have.Work == r.Work && have.Agent == r.Agent {
				if r.At.Before(have.At) {
					s.Roles[i].At = r.At
				}
				return
			}
		}
		s.Roles = append(s.Roles, r)
	}
	for _, b := range marks {
		if b.Entity == id && b.Role != "" {
			add(b, EntityRole{Kind: b.Role})
		}
	}
	for _, b := range binds {
		switch title, isWork := refsCard[b.Entity]; {
		case b.Entity == id && b.Role != "":
			add(b, EntityRole{Kind: b.Role})
		case b.Entity == id && b.Retro:
			add(b, EntityRole{Kind: RoleWorked})
		case b.Entity == id:
			add(b, EntityRole{Kind: RoleImplemented})
		case isWork && strings.HasPrefix(b.Entity, "W-"):
			add(b, EntityRole{Kind: RoleWork, Work: b.Entity, WorkTitle: title})
		}
	}
	if len(order) == 0 {
		return []EntitySession{}, nil
	}

	infos, err := cost.SessionInfos(roots, cursorRoots, order)
	if err != nil {
		return nil, err
	}
	out := make([]EntitySession, 0, len(order))
	for _, sess := range order {
		s := byID[sess]
		s.Info = infos[sess]
		for i, r := range s.Roles {
			if r.Agent != "" {
				s.Roles[i].AgentType = s.Info.Agents[r.Agent]
			}
		}
		// Opening the card is a session's first touch, whatever the clock says:
		// `feature new --file` records its spec a moment before the hook
		// records the opening.
		slices.SortStableFunc(s.Roles, func(a, b EntityRole) int {
			if (a.Kind == RoleOpened) != (b.Kind == RoleOpened) {
				if a.Kind == RoleOpened {
					return -1
				}
				return 1
			}
			return a.At.Compare(b.At)
		})
		out = append(out, *s)
	}
	first := func(s EntitySession) time.Time { return s.Roles[0].At }
	slices.SortStableFunc(out, func(a, b EntitySession) int { return first(a).Compare(first(b)) })
	return out, nil
}
