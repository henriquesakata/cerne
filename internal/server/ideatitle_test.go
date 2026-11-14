package server

import (
	"net/http"
	"testing"

	"github.com/henriquesakata/cerne/internal/store"
)

// F-0419 crit. 5: POST /ideas takes {title, body}; a multiline title is a 422.
func TestIdeaCaptureTitleAndBody(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	h := New(repoDir, projDir, "")
	var created struct {
		Idea store.Idea `json:"idea"`
	}
	if rec := doJSON(t, h, http.MethodPost, "/api/ideas", `{"title":"com corpo","body":"detalhe"}`, &created); rec.Code != http.StatusCreated || created.Idea.Title != "com corpo" || created.Idea.Body != "detalhe" {
		t.Fatalf("POST title+body = %d, idea %+v", rec.Code, created.Idea)
	}
	var e map[string]any
	if rec := doJSON(t, h, http.MethodPost, "/api/ideas", `{"title":"duas\nlinhas"}`, &e); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("multiline title = %d, want 422", rec.Code)
	}
}
