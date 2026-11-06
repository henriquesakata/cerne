package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/project"
	"gitlab.com/rockt/cerne/internal/room"
	"gitlab.com/rockt/cerne/internal/store"
)

// Permanent channel (F-0325): the agent side of the project channel stored by
// room.Channel. An agent's nick on the channel is its work id — the roster is
// the active works (no join), so the work is the only identity there is.

// channelIdentity is what a session answers to on the channel.
type channelIdentity struct {
	Work  string
	Refs  []string
	Paths []string
}

func identityOf(w store.Work) channelIdentity {
	return channelIdentity{Work: w.ID, Refs: w.Refs, Paths: w.Paths}
}

// sessionWork resolves session sess to the work it answers as: its W- binding,
// or for an F- binding the feature's auto work under one of my actors. A
// dead, missing or unresolvable binding is no identity.
func sessionWork(cacheDir, repoDir, sess string) (channelIdentity, bool) {
	id, ok, err := CurrentBinding(cacheDir, repoDir, sess)
	if err != nil || !ok {
		return channelIdentity{}, false
	}
	if strings.HasPrefix(id, "W-") {
		w, err := store.LoadWork(repoDir, id)
		if err != nil {
			return channelIdentity{}, false
		}
		return identityOf(w), true
	}
	works, err := store.LoadAllWorks(repoDir)
	if err != nil {
		return channelIdentity{}, false
	}
	mine := myActors()
	for _, w := range works {
		if w.State == domain.WorkActive && w.Auto && slices.Contains(w.Refs, id) && slices.Contains(mine, w.Actor) {
			return identityOf(w), true
		}
	}
	return channelIdentity{}, false
}

var channelRefRe = regexp.MustCompile(`\b[WFTE]-\d{4,}\b`)

// directedTo reports whether m is addressed to id (criterion 9): it names
// id's work or one of its refs, or a path inside id's declared scope. A
// session's own message is never addressed to it.
func directedTo(m room.Message, id channelIdentity) bool {
	if id.Work == "" || m.Nick == id.Work {
		return false
	}
	for _, ref := range channelRefRe.FindAllString(m.Body, -1) {
		if ref == id.Work || slices.Contains(id.Refs, ref) {
			return true
		}
	}
	if len(id.Paths) == 0 {
		return false
	}
	for _, tok := range strings.Fields(m.Body) {
		tok = strings.TrimRight(strings.TrimLeft(tok, "`'\"([{<"), "`'\")]}>,.;:!?")
		tok = strings.TrimPrefix(tok, "./")
		if !strings.Contains(tok, "/") {
			continue
		}
		for _, p := range id.Paths {
			if globsOverlap(tok, p) {
				return true
			}
		}
	}
	return false
}

// channelTTL bounds every channel message's life (criterion 20).
const channelTTL = 24 * time.Hour

// liveMessages keeps the messages still alive at now: younger than 24h and,
// for an agent's message, sent by a work that is still live. Every reader
// filters through it, so no surface ever shows an expired message.
func liveMessages(repoDir string, msgs []room.Message, now time.Time) []room.Message {
	senderLive := map[string]bool{}
	var out []room.Message
	for _, m := range msgs {
		if now.Sub(m.Time) > channelTTL {
			continue
		}
		if !m.Human {
			live, seen := senderLive[m.Nick]
			if !seen {
				live = bindingLive(repoDir, m.Nick)
				senderLive[m.Nick] = live
			}
			if !live {
				continue
			}
		}
		out = append(out, m)
	}
	return out
}

// Injection budget (criteria 6-8). Tokens are estimated as bytes/4, the
// repo's convention (materialize/stats.go, cli/savingscmd.go) — for pt-BR
// multibyte text that is more conservative than runes/4. channelBudgetBytes
// is the one knob if the real tokenizer disagrees.
const (
	channelBudgetBytes = 400 * 4
	channelInlineCap   = 5
	channelBodyRunes   = 280
	// ponytail: per-body floor of 60 bytes; below it (only with ~17+ human
	// messages pending) the oldest humans fall into the count line instead
	// of shrinking every body further.
	channelBodyFloor = 60
)

// injection groups, in priority order (criterion 7).
const (
	groupHuman = iota
	groupDirected
	groupBackground
)

type injItem struct {
	m        room.Message
	group    int
	directed bool // addressed to the session — a human message can be too
}

func (it injItem) prefix() string {
	who := it.m.Nick
	if it.m.Human {
		who += " (human)"
	}
	if it.directed {
		who += " → you"
	}
	return fmt.Sprintf("#%d %s: ", it.m.Seq, who)
}

// channelState is the derived view of the channel for one identity.
type channelState struct {
	msgs []room.Message // every parseable message, expired included
	dels []room.Delivery
}

// answered reports whether work acked m, or a session of work posted after m
// was delivered to that same session (criterion 10) — nothing else silences
// an addressed message. A post made before the session was shown m, a peek
// (a subagent's discarded context) or another session's post answers nothing:
// each would silence exactly the message the session has not seen.
func (cs channelState) answered(m room.Message, work string) bool {
	if work == "" {
		return false
	}
	for _, d := range cs.dels {
		if d.Work == work && slices.Contains(d.Ack, m.Seq) {
			return true
		}
	}
	for _, d := range cs.dels {
		if d.Work != work || d.Peek || !slices.Contains(d.Seqs, m.Seq) {
			continue
		}
		for _, later := range cs.msgs {
			if later.Seq > m.Seq && later.Nick == work && later.Session == d.Session && !later.Time.Before(d.At) {
				return true
			}
		}
	}
	return false
}

// shownTo reports whether a delivery (not a peek) showed m to session sess —
// an injection, an explicit read or a Stop reminder.
func (cs channelState) shownTo(m room.Message, sess string) bool {
	for _, d := range cs.dels {
		if d.Session == sess && !d.Peek && slices.Contains(d.Seqs, m.Seq) {
			return true
		}
	}
	return false
}

// watermark is the highest seq session sess has seen.
func (cs channelState) watermark(sess string) int {
	wm := 0
	for _, d := range cs.dels {
		if d.Session == sess {
			wm = max(wm, d.Through)
		}
	}
	return wm
}

// ChannelInject renders the channel block for session sess at a contact point
// and records the delivery in the same locked step (criteria 4-11, 17): the
// live messages the session has not seen plus the ones addressed to it and
// still unanswered, human first, then addressed, then background, cut to the
// inline cap and the byte budget. Nothing to deliver is zero bytes and no
// write. consume=false is a peek (subagents): it records what was shown but
// leaves the watermark, so the parent session still gets the background.
func ChannelInject(cacheDir, repoDir, sess string, consume bool, now time.Time) (string, error) {
	if safeSessionID(sess) != nil {
		return "", nil
	}
	ch := room.OpenChannel(filepath.Join(cacheDir, "rooms"))
	if msgs, err := ch.Messages(); err != nil || len(msgs) == 0 {
		return "", err // hot path: no channel yet
	}
	id, _ := sessionWork(cacheDir, repoDir, sess)
	var out string
	err := ch.Update(func(msgs []room.Message, dels []room.Delivery) (*room.Delivery, error) {
		cs := channelState{msgs: msgs, dels: dels}
		live := liveMessages(repoDir, msgs, now)
		wm := cs.watermark(sess)
		var pending []injItem
		through := 0
		for _, m := range live {
			through = max(through, m.Seq)
			if m.Nick == id.Work && !m.Human {
				continue
			}
			directed := directedTo(m, id) && !cs.answered(m, id.Work)
			if !directed && (m.Seq <= wm || cs.shownTo(m, sess)) {
				continue
			}
			group := groupBackground
			switch {
			case m.Human:
				group = groupHuman
			case directed:
				group = groupDirected
			}
			pending = append(pending, injItem{m: m, group: group, directed: directed})
		}
		if len(pending) == 0 {
			return nil, nil
		}
		var inline []room.Message
		out, inline = renderInjection(pending, "cerne channel:\n")
		d := &room.Delivery{Session: sess, Work: id.Work, At: now, Peek: !consume}
		for _, m := range inline {
			d.Seqs = append(d.Seqs, m.Seq)
		}
		if consume {
			d.Through = through
		}
		return d, nil
	})
	if err != nil {
		return "", err
	}
	return out, nil
}

// renderInjection picks and formats the inline set within the budget. Human
// messages are always inline (they pierce the cap); the rest fill up to the
// cap by priority, newest first within a group. Every body is cut to
// min(280 runes, its equal share of the remaining bytes), which holds the
// whole block under channelBudgetBytes by construction.
func renderInjection(pending []injItem, header string) (string, []room.Message) {
	ranked := slices.Clone(pending)
	slices.SortStableFunc(ranked, func(a, b injItem) int {
		if a.group != b.group {
			return a.group - b.group
		}
		return b.m.Seq - a.m.Seq
	})
	humans := 0
	for _, it := range ranked {
		if it.group == groupHuman {
			humans++
		}
	}
	sel := ranked[:min(len(ranked), max(humans, channelInlineCap))]
	anyDirected := slices.ContainsFunc(pending, func(it injItem) bool { return it.directed })

	var tail string
	per := 0
	for {
		tail = injectionTail(sel, len(pending)-len(sel), anyDirected)
		used := len(header) + len(tail)
		for _, it := range sel {
			used += len(it.prefix()) + 1
		}
		if len(sel) == 0 {
			break
		}
		if per = (channelBudgetBytes - used) / len(sel); per >= channelBodyFloor {
			break
		}
		sel = sel[:len(sel)-1] // drop the lowest-priority inline message
	}

	shown := slices.Clone(sel)
	slices.SortStableFunc(shown, func(a, b injItem) int {
		if a.group != b.group {
			return a.group - b.group
		}
		return a.m.Seq - b.m.Seq
	})
	var b strings.Builder
	b.WriteString(header)
	inline := make([]room.Message, 0, len(shown))
	for _, it := range shown {
		b.WriteString(it.prefix())
		b.WriteString(truncateBody(it.m.Body, channelBodyRunes, per))
		b.WriteByte('\n')
		inline = append(inline, it.m)
	}
	b.WriteString(tail)
	slices.SortFunc(inline, func(a, b room.Message) int { return a.Seq - b.Seq })
	return b.String(), inline
}

// injectionTail is the count line for what did not fit inline, plus the
// protocol line when anything addressed is pending (criterion 17).
func injectionTail(sel []injItem, more int, anyDirected bool) string {
	var b strings.Builder
	if more > 0 {
		fmt.Fprintf(&b, "+%d more — cerne msg read\n", more)
	}
	var directed []int
	for _, it := range sel {
		if it.directed {
			directed = append(directed, it.m.Seq)
		}
	}
	slices.Sort(directed)
	ack := "<seq>"
	if len(directed) > 0 {
		ack = strings.Trim(fmt.Sprint(directed), "[]")
	}
	if anyDirected {
		fmt.Fprintf(&b, "↳ addressed to you: answer with cerne msg send \"<text>\" (any later message of yours counts) or cerne msg ack %s\n", ack)
	}
	return b.String()
}

// truncateBody flattens body to one line and cuts it to maxRunes runes and
// maxBytes bytes (the ellipsis included), marking any cut with "…".
func truncateBody(body string, maxRunes, maxBytes int) string {
	body = strings.Join(strings.Fields(body), " ")
	if utf8.RuneCountInString(body) <= maxRunes && len(body) <= maxBytes {
		return body
	}
	const ellipsis = "…"
	n, runes := 0, 0
	for i, r := range body {
		if runes == maxRunes || i+utf8.RuneLen(r) > maxBytes-len(ellipsis) {
			break
		}
		n = i + utf8.RuneLen(r)
		runes++
	}
	return body[:n] + ellipsis
}

// ErrNoChannelIdentity is the sentinel for a channel write from a session with
// no live registration: on the channel an agent speaks as its work.
var ErrNoChannelIdentity = errors.New("this session has no live cerne registration to speak on the channel as")

// Derived delivery states (criterion 13), worst to best.
const (
	ChannelNotDelivered = "not_delivered"
	ChannelDelivered    = "delivered"
	ChannelAnswered     = "answered"
)

// ChannelTarget is one work a message reached (or is addressed to) and how far
// it got there.
type ChannelTarget struct {
	Work  string `json:"work"`
	State string `json:"state"`
}

// ChannelMessage is a live channel message with its derived state: the worst
// of its targets' (none: not_delivered). An addressed message's targets are
// the works it names; anything else's are the works it was delivered to.
type ChannelMessage struct {
	room.Message
	State    string          `json:"state"`
	Directed bool            `json:"directed,omitempty"`
	Targets  []ChannelTarget `json:"targets,omitempty"`
}

// ChannelMember is one roster entry: an active work, live when a session on
// this machine is bound to it (criterion 3 — no join).
type ChannelMember struct {
	Work  string `json:"work"`
	Title string `json:"title"`
	Actor string `json:"actor"`
	Live  bool   `json:"live"`
}

func channelOf(cacheDir string) room.Channel {
	return room.OpenChannel(filepath.Join(cacheDir, "rooms"))
}

// ChannelSend posts body as session sess's work, creating the channel on
// demand (criterion 1), and returns the bound ids of the other live sessions
// that will receive it (criterion 12) — empty when nobody is around.
func ChannelSend(cacheDir, repoDir, sess, body string, now time.Time) (room.Message, []string, error) {
	if strings.TrimSpace(body) == "" {
		return room.Message{}, nil, errors.New("the message cannot be empty")
	}
	if safeSessionID(sess) != nil {
		return room.Message{}, nil, ErrNoChannelIdentity
	}
	id, ok := sessionWork(cacheDir, repoDir, sess)
	if !ok {
		return room.Message{}, nil, ErrNoChannelIdentity
	}
	m, err := channelOf(cacheDir).Append(room.Message{Type: "message", Nick: id.Work, Session: sess, Body: body, Time: now})
	if err != nil {
		return room.Message{}, nil, err
	}
	bindings := sessionBindingsIn(cacheDir)
	mine := bindings[sess]
	var to []string
	for s, bound := range bindings {
		if s != sess && bound != mine && bindingLive(repoDir, bound) {
			to = append(to, bound)
		}
	}
	slices.Sort(to)
	return m, to, nil
}

// ChannelPost posts body as the human named name (criterion 22).
func ChannelPost(cacheDir, name, body string, now time.Time) (room.Message, error) {
	if strings.TrimSpace(body) == "" {
		return room.Message{}, errors.New("the message cannot be empty")
	}
	return channelOf(cacheDir).Append(room.Message{Type: "message", Nick: name, Body: body, Human: true, Time: now})
}

// ChannelAck silences addressed messages seqs for session sess's work without
// a reply (criterion 10).
func ChannelAck(cacheDir, repoDir, sess string, seqs []int, now time.Time) error {
	if safeSessionID(sess) != nil {
		return ErrNoChannelIdentity
	}
	id, ok := sessionWork(cacheDir, repoDir, sess)
	if !ok {
		return ErrNoChannelIdentity
	}
	return channelOf(cacheDir).Update(func([]room.Message, []room.Delivery) (*room.Delivery, error) {
		return &room.Delivery{Session: sess, Work: id.Work, Ack: seqs, At: now}, nil
	})
}

// ChannelRead is the explicit read (criterion 18): every live message with its
// derived state, plus the roster. With a session it also records that the
// session saw all of it, so no injection repeats what was just read; without
// one (the UI) it writes nothing.
func ChannelRead(cacheDir, repoDir, sess string, now time.Time) ([]ChannelMessage, []ChannelMember, error) {
	works, err := store.LoadAllWorks(repoDir)
	if err != nil {
		return nil, nil, err
	}
	var active []store.Work
	for _, w := range works {
		if w.State == domain.WorkActive {
			active = append(active, w)
		}
	}
	ch := channelOf(cacheDir)
	msgs, err := ch.Messages()
	if err != nil {
		return nil, nil, err
	}
	dels, err := ch.Deliveries()
	if err != nil {
		return nil, nil, err
	}
	if len(msgs) > 0 && safeSessionID(sess) == nil {
		id, _ := sessionWork(cacheDir, repoDir, sess) // unregistered: still a session that saw it
		err := ch.Update(func(lockedMsgs []room.Message, lockedDels []room.Delivery) (*room.Delivery, error) {
			msgs, dels = lockedMsgs, lockedDels
			d := &room.Delivery{Session: sess, Work: id.Work, At: now}
			for _, m := range liveMessages(repoDir, msgs, now) {
				d.Through = max(d.Through, m.Seq)
				if m.Human || m.Nick != id.Work {
					d.Seqs = append(d.Seqs, m.Seq)
				}
			}
			if len(d.Seqs) == 0 {
				return nil, nil
			}
			dels = append(dels, *d)
			return d, nil
		})
		if err != nil {
			return nil, nil, err
		}
	}
	cs := channelState{msgs: msgs, dels: dels}
	var out []ChannelMessage
	for _, m := range liveMessages(repoDir, msgs, now) {
		out = append(out, cs.derive(m, active))
	}
	return out, channelRoster(cacheDir, active), nil
}

// derive computes m's targets and state against the active works.
func (cs channelState) derive(m room.Message, active []store.Work) ChannelMessage {
	cm := ChannelMessage{Message: m}
	cm.Session = "" // a session id lets `msg send --session` speak as its work: never served
	for _, w := range active {
		if directedTo(m, identityOf(w)) {
			cm.Directed = true
			cm.Targets = append(cm.Targets, ChannelTarget{Work: w.ID})
		}
	}
	if !cm.Directed {
		seen := map[string]bool{}
		for _, d := range cs.dels {
			if d.Work != "" && d.Work != m.Nick && !seen[d.Work] && slices.Contains(d.Seqs, m.Seq) {
				seen[d.Work] = true
				cm.Targets = append(cm.Targets, ChannelTarget{Work: d.Work})
			}
		}
	}
	rank := map[string]int{ChannelNotDelivered: 0, ChannelDelivered: 1, ChannelAnswered: 2}
	cm.State = ChannelAnswered
	for i, t := range cm.Targets {
		switch {
		case cs.answered(m, t.Work):
			t.State = ChannelAnswered
		case cs.delivered(m, t.Work):
			t.State = ChannelDelivered
		default:
			t.State = ChannelNotDelivered
		}
		cm.Targets[i] = t
		if rank[t.State] < rank[cm.State] {
			cm.State = t.State
		}
	}
	if len(cm.Targets) == 0 {
		cm.State = ChannelNotDelivered
	}
	return cm
}

// delivered reports whether m was ever shown to a session working as work (a
// peek is no delivery).
func (cs channelState) delivered(m room.Message, work string) bool {
	for _, d := range cs.dels {
		if d.Work == work && !d.Peek && slices.Contains(d.Seqs, m.Seq) {
			return true
		}
	}
	return false
}

// channelRoster lists the active works, marking those a local session is bound
// to (directly, or through the feature of an auto work).
func channelRoster(cacheDir string, active []store.Work) []ChannelMember {
	bound := map[string]bool{}
	for _, id := range sessionBindingsIn(cacheDir) {
		bound[id] = true
	}
	out := make([]ChannelMember, 0, len(active))
	for _, w := range active {
		live := bound[w.ID]
		if w.Auto {
			live = live || slices.ContainsFunc(w.Refs, func(r string) bool { return bound[r] })
		}
		out = append(out, ChannelMember{Work: w.ID, Title: w.Title, Actor: w.Actor, Live: live})
	}
	return out
}

// ChannelStopReminder is the end-of-turn nudge for messages addressed to
// session sess and still unanswered (criterion 16): each is reminded at most
// once per session, so a Stop continuation never comes back for it. The
// returned mark records them as reminded — call it only once the text was
// actually emitted. Nothing due, or any error: "" and a no-op mark.
func ChannelStopReminder(cacheDir, repoDir, sess string, now time.Time) (string, func()) {
	noop := func() {}
	if safeSessionID(sess) != nil {
		return "", noop
	}
	ch := channelOf(cacheDir)
	msgs, err := ch.Messages()
	if err != nil || len(msgs) == 0 {
		return "", noop
	}
	id, ok := sessionWork(cacheDir, repoDir, sess)
	if !ok {
		return "", noop
	}
	dels, err := ch.Deliveries()
	if err != nil {
		return "", noop
	}
	marker := filepath.Join(cacheDir, "rooms", room.ChannelID, "stop-reminded", sess)
	raw, _ := os.ReadFile(marker)
	reminded := strings.Fields(string(raw))
	cs := channelState{msgs: msgs, dels: dels}
	var due []injItem
	var seqs []string
	for _, m := range liveMessages(repoDir, msgs, now) {
		seq := strconv.Itoa(m.Seq)
		if slices.Contains(reminded, seq) || !directedTo(m, id) || cs.answered(m, id.Work) {
			continue
		}
		group := groupDirected
		if m.Human {
			group = groupHuman
		}
		due = append(due, injItem{m: m, group: group, directed: true})
		seqs = append(seqs, seq)
	}
	if len(due) == 0 {
		return "", noop
	}
	// Same renderer, caps and 400-token ceiling as every injection (critic C4).
	text, inline := renderInjection(due, "cerne channel — addressed to you and still unanswered as this turn ends:\n")
	return text, func() {
		// The reminder showed these messages to the session: a delivery like
		// any injection, so the reply it asks for answers them (critic C3).
		// It moves no watermark: it carries nothing but addressed messages.
		// Every due message is marked reminded — the count line points at
		// `cerne msg read` for the rest — so one Stop never becomes many.
		shown := make([]int, len(inline))
		for i, m := range inline {
			shown[i] = m.Seq
		}
		_ = ch.Update(func([]room.Message, []room.Delivery) (*room.Delivery, error) {
			return &room.Delivery{Session: sess, Work: id.Work, Seqs: shown, At: now}, nil
		})
		if os.MkdirAll(filepath.Dir(marker), 0o755) == nil {
			_ = os.WriteFile(marker, []byte(strings.Join(append(reminded, seqs...), "\n")+"\n"), 0o644)
		}
	}
}

// ChannelHumanName is the human's nick on the channel (criterion 22): the
// channel.human_name setting (host scope — it is the person's name, not the
// project's), else the OS user, as for a work's actor, else "human".
func ChannelHumanName(projDir string) string {
	for _, s := range project.ResolveSettings(projDir).Settings {
		if name, _ := s.Value.(string); s.ID == "channel.human_name" && strings.TrimSpace(name) != "" {
			return name
		}
	}
	for _, env := range []string{"USER", "USERNAME"} {
		if u := os.Getenv(env); u != "" {
			return u
		}
	}
	return "human"
}
