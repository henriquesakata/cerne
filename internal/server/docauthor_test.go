package server

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// lastDocEdited returns the payload of the newest feature.doc_edited of id.
func lastDocEdited(t *testing.T, repoDir, id string) map[string]string {
	t.Helper()
	evs, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	var p map[string]string
	for _, ev := range evs {
		if ev.EntityID == id && ev.Type == "feature.doc_edited" {
			p = ev.Payload
		}
	}
	return p
}

// agentServerEnv is a server started from an agent's shell: its environment
// says agent, declared provider and model.
func agentServerEnv(t *testing.T) {
	t.Setenv("CERNE_ACTOR", "agent")
	t.Setenv("CERNE_SESSION", "")
	t.Setenv("CERNE_ORIGIN", "")
	t.Setenv("CERNE_PROVIDER", "openai")
	t.Setenv("CERNE_MODEL", "gpt-x")
}

// TestEditorWritesAsHuman (F-0375 crit. 7): a document saved from the
// feature page is a human's, whatever the server's own environment says.
func TestEditorWritesAsHuman(t *testing.T) {
	agentServerEnv(t)
	repoDir, projDir, _ := newGitBackend(t)
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodPut, "/api/features/F-0001/docs/spec", realDoc("F-0001", "Problema real."), &map[string]any{})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT spec = %d %s", rec.Code, rec.Body.String())
	}
	if p := lastDocEdited(t, repoDir, "F-0001"); p["source"] != "human" || p["provider"] != "" || p["model"] != "" {
		t.Fatalf("editor write = %v, want human with no provider or model", p)
	}
}

// TestChatRegisterRecordsLLMConfig (F-0375 crit. 6): the spec the chat
// proposed is recorded as the [llm] config of the call — the conversation's
// model when the page sends it, the project's otherwise — on creation and on
// a refine's update.
func TestChatRegisterRecordsLLMConfig(t *testing.T) {
	agentServerEnv(t)
	repoDir, projDir, _ := newGitBackend(t)
	cfg := "backend = \"x\"\n\n[llm]\nmode = \"byo-key\"\nmodel = \"claude-project\"\n"
	if err := os.WriteFile(filepath.Join(projDir, ".cernerc"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	h := New(repoDir, projDir, "proj-chat-author")
	rec := doJSON(t, h, http.MethodPost, "/api/chat/register",
		`{"title":"Exportar CSV","model":"claude-chosen","spec_body":"# Exportar CSV\n\n## Problema\nFalta exportação.\n"}`, &map[string]any{})
	if rec.Code != http.StatusCreated {
		t.Fatalf("register = %d %s", rec.Code, rec.Body.String())
	}
	if p := lastDocEdited(t, repoDir, "F-0002"); p["source"] != "llm-config" || p["provider"] != "anthropic" || p["model"] != "claude-chosen" || p["doc"] != "spec" {
		t.Fatalf("chat create = %v, want llm-config anthropic/claude-chosen", p)
	}
	rec = doJSON(t, h, http.MethodPost, "/api/chat/register",
		`{"feature_id":"F-0002","spec_body":"# Exportar CSV\n\n## Problema\nFalta exportação e importação.\n"}`, &map[string]any{})
	if rec.Code != http.StatusOK {
		t.Fatalf("refine = %d %s", rec.Code, rec.Body.String())
	}
	if p := lastDocEdited(t, repoDir, "F-0002"); p["source"] != "llm-config" || p["model"] != "claude-project" || p["state"] != string(domain.StateCandidate) {
		t.Fatalf("chat refine = %v, want llm-config with the project's model", p)
	}
}
