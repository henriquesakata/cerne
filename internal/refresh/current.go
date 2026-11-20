package refresh

import (
	"os"
	"path/filepath"
	"sync"

	"github.com/henriquesakata/cerne/internal/hooks"
	"github.com/henriquesakata/cerne/internal/skills"
)

var (
	currentOnce  sync.Once
	currentItems map[string]string
)

// AllProviders is every agent cerne can connect.
var AllProviders = []hooks.Provider{hooks.ProviderClaude, hooks.ProviderCursor, hooks.ProviderCodex, hooks.ProviderGemini, hooks.ProviderCopilot}

// InstallAll writes every agent's hooks and guide into dir, the folders the
// providers detect included.
func InstallAll(dir string) error {
	for _, d := range []string{".cursor", ".codex", ".gemini", filepath.Join(".github", "skills")} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			return err
		}
	}
	if _, err := hooks.Install(dir, AllProviders...); err != nil {
		return err
	}
	_, err := skills.Install(dir, AllProviders[1:]...)
	return err
}

// Current is what this build writes, every agent included: it installs into a
// scratch folder and reads it back, so there is no second list to keep.
func Current() map[string]string {
	currentOnce.Do(func() {
		dir, err := os.MkdirTemp("", "cerne-current-*")
		if err != nil {
			return
		}
		defer os.RemoveAll(dir)
		if InstallAll(dir) == nil {
			currentItems = Collect(dir)
		}
	})
	return currentItems
}
