package server

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

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

// TestFeatureDetailWrites (F-0375 crit. 8, 11): the feature page gets every
// recorded write of the feature's documents, oldest first — [] when none, and
// a pre-F-0375 event (no source) is never one.
func TestFeatureDetailWrites(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	h := New(repoDir, projDir, "")
	var got struct {
		Writes []struct {
			Doc, Provider, Model, Source string
		} `json:"writes"`
	}
	rec := doJSON(t, h, http.MethodGet, "/api/features/F-0001", "", &got)
	if rec.Code != http.StatusOK || got.Writes == nil || len(got.Writes) != 0 {
		t.Fatalf("no writes yet: status %d writes %v body %s, want []", rec.Code, got.Writes, rec.Body.String())
	}
	if err := store.AppendEvent(repoDir, domain.Event{ID: "old", At: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), EntityID: "F-0001",
		Type: "feature.doc_edited", Payload: map[string]string{"doc": "spec", "state": "candidate"}}); err != nil {
		t.Fatal(err)
	}
	agentServerEnv(t)
	if rec := doJSON(t, h, http.MethodPut, "/api/features/F-0001/docs/spec", realDoc("F-0001", "Problema real."), &map[string]any{}); rec.Code != http.StatusOK {
		t.Fatalf("PUT = %d", rec.Code)
	}
	rec = doJSON(t, h, http.MethodGet, "/api/features/F-0001", "", &got)
	if rec.Code != http.StatusOK || len(got.Writes) != 1 || got.Writes[0].Doc != "spec" || got.Writes[0].Source != "human" {
		t.Fatalf("writes = %+v, want the editor's one human write", got.Writes)
	}
}

// TestPagePromoteNeverTakesTheServerEnv (critic round 1, item 4): the page's
// only promote is an idea's (the pendings API is read-only, pendings.go). Its
// spec is the idea's text in cerne's stub, written by whoever captured the
// idea — so the promote records no writer at all, never the server's agent
// environment.
func TestPagePromoteNeverTakesTheServerEnv(t *testing.T) {
	agentServerEnv(t)
	repoDir, projDir, _ := newGitBackend(t)
	h := New(repoDir, projDir, "")
	if rec := doJSON(t, h, http.MethodPost, "/api/ideas", `{"title":"exportar CSV"}`, &map[string]any{}); rec.Code != http.StatusCreated {
		t.Fatalf("POST idea = %d %s", rec.Code, rec.Body.String())
	}
	var promoted struct {
		Feature store.Feature `json:"feature"`
	}
	if rec := doJSON(t, h, http.MethodPost, "/api/ideas/I-0001/promote", `{}`, &promoted); rec.Code != http.StatusOK {
		t.Fatalf("promote = %d %s", rec.Code, rec.Body.String())
	}
	if p := lastDocEdited(t, repoDir, promoted.Feature.ID); p != nil {
		t.Fatalf("page promote recorded a writer %v, want none", p)
	}
}
