package app

import (
	"regexp"
	"slices"
	"strings"
	"time"

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
