package llm

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/henriquesakata/cerne/internal/models"
)

// codexCache is the shape the Codex CLI writes to $CODEX_HOME/models_cache.json
// (codex-cli 0.169.0), trimmed to the fields cerne reads.
const codexCache = `{"client_version":"0.169.0","models":[
 {"slug":"gpt-b","display_name":"GPT B","visibility":"list","priority":5,"default_reasoning_level":"high",
  "supported_reasoning_levels":[{"effort":"low","description":"x"},{"effort":"high","description":"y"}]},
 {"slug":"hidden","display_name":"Hidden","visibility":"hide","priority":1,"default_reasoning_level":"medium",
  "supported_reasoning_levels":[{"effort":"medium"}]},
 {"slug":"gpt-a","display_name":"GPT A","visibility":"list","priority":3,"default_reasoning_level":"medium",
  "supported_reasoning_levels":[{"effort":"low"},{"effort":"medium"},{"effort":"xhigh"}]}
]}`

// B-0006: the Codex models the chat offers are the ones the CLI's own picker
// lists for the logged-in account, in its order, with their reasoning efforts.
func TestCodexModelsReadsCLICache(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "models_cache.json"), []byte(codexCache), 0o600); err != nil {
		t.Fatal(err)
	}
	want := []models.Entry{
		{ID: "gpt-a", Label: "GPT A", Provider: "openai", Efforts: []string{"low", "medium", "xhigh"}, DefaultEffort: "medium", Visibility: models.Current},
		{ID: "gpt-b", Label: "GPT B", Provider: "openai", Efforts: []string{"low", "high"}, DefaultEffort: "high", Visibility: models.Current},
	}
	if got := CodexModels(); !reflect.DeepEqual(got, want) {
		t.Fatalf("CodexModels() =\n %+v\nwant\n %+v", got, want)
	}
}

// An absent or unreadable cache offers no Codex model: the chat stays as it was.
func TestCodexModelsWithoutCacheIsEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	if got := CodexModels(); got != nil {
		t.Fatalf("absent cache: %+v, want nil", got)
	}
	if err := os.WriteFile(filepath.Join(home, "models_cache.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := CodexModels(); got != nil {
		t.Fatalf("malformed cache: %+v, want nil", got)
	}
}
