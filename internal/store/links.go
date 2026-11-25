package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// Link suggestion statuses (F-0502).
const (
	LinkOpen     = "open"
	LinkAccepted = "accepted"
	LinkRejected = "rejected"
)

// LinkSuggestion is one suggested relation From → To with the literal excerpt
// of From that justifies it. N is a sequential number never reused: entries
// are never deleted, a decision only changes Status.
type LinkSuggestion struct {
	N         int       `json:"n"`
	From      string    `json:"from"`
	To        string    `json:"to"`
	Excerpt   string    `json:"excerpt"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	DecidedAt time.Time `json:"decided_at,omitzero"`
	DecidedBy string    `json:"decided_by,omitempty"`
}

func linkSuggestionsPath(repoDir string) string {
	return filepath.Join(repoDir, "links", "suggestions.json")
}

// LoadLinkSuggestions reads links/suggestions.json sorted by n; a missing
// file is an empty queue.
func LoadLinkSuggestions(repoDir string) ([]LinkSuggestion, error) {
	data, err := os.ReadFile(linkSuggestionsPath(repoDir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var list []LinkSuggestion
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("links/suggestions.json: %w", err)
	}
	slices.SortFunc(list, func(a, b LinkSuggestion) int { return a.N - b.N })
	return list, nil
}

// SaveLinkSuggestions writes links/suggestions.json atomically, sorted by n.
func SaveLinkSuggestions(repoDir string, list []LinkSuggestion) error {
	list = slices.Clone(list)
	slices.SortFunc(list, func(a, b LinkSuggestion) int { return a.N - b.N })
	return writeJSONFile(filepath.Dir(linkSuggestionsPath(repoDir)), "suggestions.json", list)
}

// ConfirmedLinks is the accepted relations by source id, targets sorted and
// without repeats.
func ConfirmedLinks(repoDir string) (map[string][]string, error) {
	list, err := LoadLinkSuggestions(repoDir)
	if err != nil {
		return nil, err
	}
	out := map[string][]string{}
	for _, s := range list {
		if s.Status == LinkAccepted {
			out[s.From] = append(out[s.From], s.To)
		}
	}
	for from, tos := range out {
		slices.Sort(tos)
		out[from] = slices.Compact(tos)
	}
	return out, nil
}
