package server

import (
	"net/http"
	"strings"
	"testing"
)

// F-0324 (A7): a duplicate refusal on the note/reference creation routes answers
// 409 with the same body shape as the feature routes (dupConflictBody).
func TestCreateEntityDuplicateIs409(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	h := New(repoDir, projDir, "proj-dup-entities")

	noteBody := `{"title":"Candidata é a proposta antes do aprove","body":"corpo real","facets":["code"],"components":["backlog"]}`
	refBody := `{"title":"Artigo PKCE","url":"https://example.com/pkce","content":"texto"}`
	for _, c := range []struct{ name, path, body string }{
		{"note", "/api/chat/note", noteBody},
		{"reference", "/api/references", refBody},
	} {
		if rec := doJSON(t, h, http.MethodPost, c.path, c.body, &map[string]any{}); rec.Code != http.StatusCreated {
			t.Fatalf("%s first: status = %d; %s", c.name, rec.Code, rec.Body.String())
		}
		var got struct {
			Error   string     `json:"error"`
			Matches []dupMatch `json:"matches"`
		}
		rec := doJSON(t, h, http.MethodPost, c.path, c.body, &got)
		if rec.Code != http.StatusConflict {
			t.Fatalf("%s duplicate: status = %d, want 409; %s", c.name, rec.Code, rec.Body.String())
		}
		if len(got.Matches) != 1 || !strings.Contains(got.Error, got.Matches[0].ID) {
			t.Fatalf("%s duplicate body = %+v, want matches + ids in the message", c.name, got)
		}
	}
}
