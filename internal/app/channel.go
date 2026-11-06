package app

import (
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"gitlab.com/rockt/cerne/internal/domain"
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

// answered reports whether work has posted after m or acked it (criterion
// 10) — nothing else silences an addressed message.
func (cs channelState) answered(m room.Message, work string) bool {
	if work == "" {
		return false
	}
	for _, later := range cs.msgs {
		if later.Seq > m.Seq && later.Nick == work {
			return true
		}
	}
	for _, d := range cs.dels {
		if d.Work == work && slices.Contains(d.Ack, m.Seq) {
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
			if m.Seq <= wm && !directed {
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
		out, inline = renderInjection(pending)
		d := &room.Delivery{Session: sess, Work: id.Work, At: now}
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
func renderInjection(pending []injItem) (string, []room.Message) {
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

	const header = "cerne channel:\n"
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
