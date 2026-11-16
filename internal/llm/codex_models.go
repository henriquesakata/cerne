package llm

import (
	"cmp"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"

	"github.com/henriquesakata/cerne/internal/models"
)

// codexCacheModel is the part of a $CODEX_HOME/models_cache.json entry cerne
// reads (codex-cli 0.169.0). The file is the CLI's own cache, not a published
// contract: anything that does not parse yields no model, never an error.
type codexCacheModel struct {
	Slug         string `json:"slug"`
	DisplayName  string `json:"display_name"`
	Visibility   string `json:"visibility"`
	Priority     int    `json:"priority"`
	DefaultLevel string `json:"default_reasoning_level"`
	Levels       []struct {
		Effort string `json:"effort"`
	} `json:"supported_reasoning_levels"`
}

// CodexModels is the model list the Codex CLI keeps for the logged-in account
// (B-0006): the models its own picker lists, in its order, with their
// reasoning efforts, as openai catalog entries. The baseline catalog carries no
// Codex model because each account sees different ones. An absent or
// unreadable cache returns nil, and the chat offers no Codex model.
func CodexModels() []models.Entry {
	home := os.Getenv("CODEX_HOME")
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		home = filepath.Join(h, ".codex")
	}
	data, err := os.ReadFile(filepath.Join(home, "models_cache.json"))
	if err != nil {
		return nil
	}
	var cache struct {
		Models []codexCacheModel `json:"models"`
	}
	if json.Unmarshal(data, &cache) != nil {
		return nil
	}
	listed := slices.DeleteFunc(cache.Models, func(m codexCacheModel) bool { return m.Visibility != "list" || m.Slug == "" })
	slices.SortStableFunc(listed, func(a, b codexCacheModel) int { return cmp.Compare(a.Priority, b.Priority) })
	var out []models.Entry
	for _, m := range listed {
		e := models.Entry{ID: m.Slug, Label: cmp.Or(m.DisplayName, m.Slug), Provider: providerOpenAI,
			DefaultEffort: m.DefaultLevel, Visibility: models.Current}
		for _, l := range m.Levels {
			e.Efforts = append(e.Efforts, l.Effort)
		}
		out = append(out, e)
	}
	return out
}
