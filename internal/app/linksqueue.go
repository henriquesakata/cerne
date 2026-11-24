package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

var (
	// ErrLinkReply: the LLM's answer holds no JSON array of suggestions.
	ErrLinkReply = errors.New("unreadable link suggestions")
	// ErrLinkUnknown: no suggestion has that number.
	ErrLinkUnknown = errors.New("no such link suggestion")
	// ErrLinkDecided: the suggestion already has that decision (or, for an
	// accept, any decision).
	ErrLinkDecided = errors.New("link suggestion already decided")
)

// ParseLinkSuggestions reads the LLM's answer: the JSON array between the
// first "[" and the last "]", so a fence or a line of prose around it is fine.
func ParseLinkSuggestions(reply string) ([]RawLinkSuggestion, error) {
	i, j := strings.Index(reply, "["), strings.LastIndex(reply, "]")
	if i < 0 || j < i {
		return nil, fmt.Errorf("%w: no JSON array in the answer", ErrLinkReply)
	}
	var out []RawLinkSuggestion
	if err := json.Unmarshal([]byte(reply[i:j+1]), &out); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrLinkReply, err)
	}
	return out, nil
}

// EnqueueLinkSuggestions appends kept to the queue as open suggestions, with
// the next numbers (the highest ever used plus one: rows are never deleted,
// so a number is never reused), and returns the new rows.
func EnqueueLinkSuggestions(repoDir string, kept []RawLinkSuggestion, at time.Time) ([]store.LinkSuggestion, error) {
	queue, err := store.LoadLinkSuggestions(repoDir)
	if err != nil {
		return nil, err
	}
	n := 0
	for _, s := range queue {
		n = max(n, s.N)
	}
	var added []store.LinkSuggestion
	for _, k := range kept {
		n++
		added = append(added, store.LinkSuggestion{N: n, From: k.From, To: k.To, Excerpt: k.Excerpt, Status: store.LinkOpen, CreatedAt: at.UTC()})
	}
	if len(added) == 0 {
		return nil, nil
	}
	return added, store.SaveLinkSuggestions(repoDir, append(queue, added...))
}

// DecideLink moves suggestion n to status (LinkAccepted or LinkRejected) and
// logs link.accepted or link.rejected on its source. Accepting needs an open
// suggestion; rejecting also takes an accepted one, which leaves the graph.
func DecideLink(repoDir string, n int, status, by string) (store.LinkSuggestion, error) {
	queue, err := store.LoadLinkSuggestions(repoDir)
	if err != nil {
		return store.LinkSuggestion{}, err
	}
	for i, s := range queue {
		if s.N != n {
			continue
		}
		if s.Status == status || (status == store.LinkAccepted && s.Status != store.LinkOpen) {
			return s, fmt.Errorf("%w: #%d is %s", ErrLinkDecided, n, s.Status)
		}
		s.Status, s.DecidedAt, s.DecidedBy = status, time.Now().UTC(), by
		queue[i] = s
		if err := store.SaveLinkSuggestions(repoDir, queue); err != nil {
			return s, err
		}
		typ := "link." + status
		ev := domain.NewEvent(s.From, typ, map[string]string{"n": strconv.Itoa(n), "to": s.To}, EventActor())
		return s, store.AppendEvent(repoDir, ev)
	}
	return store.LinkSuggestion{}, fmt.Errorf("%w: #%d", ErrLinkUnknown, n)
}
