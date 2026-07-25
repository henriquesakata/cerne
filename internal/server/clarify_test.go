package server

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seedOpenClarification puts a real spec with one open marker on F-0001 and
// returns the marker's anchor in the backend doc.
func seedOpenClarification(t *testing.T, repoDir, projDir string) (doc, raw string, offset int) {
	t.Helper()
	raw = "[ESCLARECER: sincrona ou assincrona? | sincrona | assincrona]"
	spec := realDoc("F-0001", "Problema real descrito aqui.") + "\n" + raw + "\n"
	var got struct {
		Saved bool `json:"saved"`
	}
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodPut, "/api/features/F-0001/docs/spec", spec, &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("seeding spec: status=%d body=%s", rec.Code, rec.Body.String())
	}
	doc = "content/features/F-0001/spec.md"
	data, err := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(doc)))
	if err != nil {
		t.Fatalf("reading seeded spec: %v", err)
	}
	offset = strings.Index(string(data), raw)
	if offset < 0 {
		t.Fatal("marker not present in seeded spec")
	}
	return doc, raw, offset
}

func TestClarifyAnswerEndpoint(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	doc, raw, offset := seedOpenClarification(t, repoDir, projDir)

	var got struct {
		Entity   string `json:"entity"`
		Question string `json:"question"`
		Answer   string `json:"answer"`
	}
	body := fmt.Sprintf(`{"doc":%q,"offset":%d,"raw":%q,"answer":"sincrona"}`, doc, offset, raw)
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodPost, "/api/clarify/F-0001/answer", body, &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s, want 200", rec.Code, rec.Body.String())
	}
	if got.Entity != "F-0001" || got.Question != "sincrona ou assincrona?" || got.Answer != "sincrona" {
		t.Fatalf("response = %+v", got)
	}
	data, err := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(doc)))
	if err != nil {
		t.Fatalf("reading spec: %v", err)
	}
	if !strings.Contains(string(data), "[ESCLARECIDO: sincrona ou assincrona? → sincrona]") {
		t.Fatalf("marker not resolved in backend doc:\n%s", data)
	}
}

func TestClarifyAnswerEndpointStaleAnchor(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	doc, raw, _ := seedOpenClarification(t, repoDir, projDir)

	var got map[string]string
	body := fmt.Sprintf(`{"doc":%q,"offset":%d,"raw":%q,"answer":"x"}`, doc, 1, raw)
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodPost, "/api/clarify/F-0001/answer", body, &got)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s, want 409", rec.Code, rec.Body.String())
	}
}

func TestClarifyAnswerEndpointUnknownEntity(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)

	var got map[string]string
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodPost, "/api/clarify/F-9999/answer",
		`{"doc":"content/features/F-9999/spec.md","offset":0,"raw":"[ESCLARECER]","answer":"x"}`, &got)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s, want 404", rec.Code, rec.Body.String())
	}
}

// F-0197: a rota irmã, que corrige em vez de responder.
func TestClarifyCorrectEndpoint(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	doc, raw, offset := seedOpenClarification(t, repoDir, projDir)

	body := fmt.Sprintf(`{"doc":%q,"offset":%d,"raw":%q,"answer":"sincrona"}`, doc, offset, raw)
	var answered struct{}
	if rec := doJSON(t, New(repoDir, projDir, ""), http.MethodPost, "/api/clarify/F-0001/answer", body, &answered); rec.Code != http.StatusOK {
		t.Fatalf("seeding the answer: status=%d body=%s", rec.Code, rec.Body.String())
	}

	data, err := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(doc)))
	if err != nil {
		t.Fatalf("reading spec: %v", err)
	}
	resolvedRaw := "[ESCLARECIDO: sincrona ou assincrona? → sincrona]"
	resolvedOff := strings.Index(string(data), resolvedRaw)
	if resolvedOff < 0 {
		t.Fatalf("resolved marker absent:\n%s", data)
	}

	var got struct {
		Entity   string `json:"entity"`
		Question string `json:"question"`
		Answer   string `json:"answer"`
	}
	body = fmt.Sprintf(`{"doc":%q,"offset":%d,"raw":%q,"answer":"assincrona"}`, doc, resolvedOff, resolvedRaw)
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodPost, "/api/clarify/F-0001/correct", body, &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s, want 200", rec.Code, rec.Body.String())
	}
	if got.Question != "sincrona ou assincrona?" || got.Answer != "assincrona" {
		t.Fatalf("response = %+v", got)
	}
	after, err := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(doc)))
	if err != nil {
		t.Fatalf("reading spec after: %v", err)
	}
	if !strings.Contains(string(after), "[ESCLARECIDO: sincrona ou assincrona? → sincrona → assincrona]") {
		t.Fatalf("trail not written:\n%s", after)
	}
}

func TestClarifyCorrectEndpointStaleAnchor(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	doc, raw, offset := seedOpenClarification(t, repoDir, projDir)

	// Marcador ainda ABERTO: corrigir não é responder.
	body := fmt.Sprintf(`{"doc":%q,"offset":%d,"raw":%q,"answer":"x"}`, doc, offset, raw)
	var got map[string]string
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodPost, "/api/clarify/F-0001/correct", body, &got)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s, want 409", rec.Code, rec.Body.String())
	}
}
