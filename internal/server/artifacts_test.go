package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/project"
)

// writeArtifact drops an artifact file straight into the backend tree.
func writeArtifact(t *testing.T, repoDir, id, name, content string) {
	t.Helper()
	dir := filepath.Join(repoDir, "content", "artifacts", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", name, err)
	}
}

const sceneA = `{"type":"excalidraw","version":2,"elements":[],"appState":{}}`
const sceneB = `{"type":"excalidraw","version":2,"elements":[{"id":"r1","type":"rectangle"}],"appState":{}}`

func TestArtifactGet(t *testing.T) {
	repoDir, projDir := newBackend(t)
	writeArtifact(t, repoDir, "F-0001", "seq.excalidraw", sceneA)

	rec := httptest.NewRecorder()
	New(repoDir, projDir, "").ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet, "/api/artifacts/F-0001/seq.excalidraw", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	if rec.Body.String() != sceneA {
		t.Fatalf("body = %q, want raw artifact content %q", rec.Body.String(), sceneA)
	}
}

func TestArtifactGetNotFound(t *testing.T) {
	repoDir, projDir := newBackend(t)
	var got map[string]string
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/artifacts/F-0001/nope.excalidraw", "", &got)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body: %s", rec.Code, rec.Body.String())
	}
	if got["error"] != "artifact not found" {
		t.Fatalf("error = %q, want %q", got["error"], "artifact not found")
	}
}

func TestArtifactGetTraversalRejected(t *testing.T) {
	repoDir, projDir := newBackend(t)
	// Plant a file OUTSIDE the feature's artifact dir; traversal must never reach it.
	writeArtifact(t, repoDir, ".", "secret.excalidraw", `{"leak":true}`)
	h := New(repoDir, projDir, "")

	for _, target := range []string{
		"/api/artifacts/F-0001/..%2Fsecret.excalidraw",
		"/api/artifacts/..%2F..%2Fproject.json/x.excalidraw",
		"/api/artifacts/F-0001/..%5Csecret.excalidraw",
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("GET %s status = %d, want 400; body: %s", target, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "leak") {
			t.Fatalf("GET %s leaked file content outside the artifact dir:\n%s", target, rec.Body.String())
		}
	}
}

func TestArtifactPutCreateAndUpdate(t *testing.T) {
	repoDir, projDir, origin := newGitBackend(t)
	head0, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA origin: %v", err)
	}
	h := New(repoDir, projDir, "")

	// Create: PUT on a nonexistent name writes the file, pushes, updates the lock.
	var got struct {
		OK bool `json:"ok"`
	}
	rec := doJSON(t, h, http.MethodPut, "/api/artifacts/F-0001/seq.excalidraw", sceneA, &got)
	if rec.Code != http.StatusOK || !got.OK {
		t.Fatalf("create status = %d body = %s, want 200 {\"ok\":true}", rec.Code, rec.Body.String())
	}
	path := filepath.Join(repoDir, "content", "artifacts", "F-0001", "seq.excalidraw")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("artifact not written to backend: %v", err)
	}
	if string(data) != sceneA {
		t.Fatalf("backend file = %q, want %q", data, sceneA)
	}
	head1, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA origin: %v", err)
	}
	if head1 == head0 {
		t.Fatalf("origin HEAD did not advance after create (still %s)", head1)
	}
	lock, err := project.LoadLock(projDir)
	if err != nil {
		t.Fatalf("LoadLock: %v", err)
	}
	if lock.Snapshot != head1 {
		t.Fatalf("lock.Snapshot = %q, want origin HEAD %q", lock.Snapshot, head1)
	}

	// Update: PUT on the existing name with new content advances origin again.
	rec = doJSON(t, h, http.MethodPut, "/api/artifacts/F-0001/seq.excalidraw", sceneB, &got)
	if rec.Code != http.StatusOK || !got.OK {
		t.Fatalf("update status = %d body = %s, want 200 {\"ok\":true}", rec.Code, rec.Body.String())
	}
	if data, _ = os.ReadFile(path); string(data) != sceneB {
		t.Fatalf("backend file after update = %q, want %q", data, sceneB)
	}
	head2, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA origin: %v", err)
	}
	if head2 == head1 {
		t.Fatalf("origin HEAD did not advance after update (still %s)", head2)
	}
	if lock, _ = project.LoadLock(projDir); lock.Snapshot != head2 {
		t.Fatalf("lock.Snapshot = %q, want origin HEAD %q", lock.Snapshot, head2)
	}

	// Idempotent PUT: byte-identical content is a no-op, still 200.
	rec = doJSON(t, h, http.MethodPut, "/api/artifacts/F-0001/seq.excalidraw", sceneB, &got)
	if rec.Code != http.StatusOK || !got.OK {
		t.Fatalf("idempotent status = %d body = %s, want 200 {\"ok\":true}", rec.Code, rec.Body.String())
	}
	if head3, _ := gitback.HeadSHA(origin); head3 != head2 {
		t.Fatalf("origin HEAD moved on a no-op PUT: %s -> %s", head2, head3)
	}
}

func TestArtifactPutInvalidJSON(t *testing.T) {
	repoDir, projDir := newBackend(t)
	seedFeatureForArtifacts(t, repoDir)
	var got map[string]string
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodPut, "/api/artifacts/F-0001/seq.excalidraw", `{not json`, &got)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", rec.Code, rec.Body.String())
	}
	if got["error"] == "" {
		t.Fatalf("body = %v, want pt-BR JSON error message", got)
	}
}

func TestArtifactPutBadExtension(t *testing.T) {
	repoDir, projDir := newBackend(t)
	seedFeatureForArtifacts(t, repoDir)
	var got map[string]string
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodPut, "/api/artifacts/F-0001/diagrama.png", `{}`, &got)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(got["error"], ".excalidraw") {
		t.Fatalf("error = %q, want message naming the .excalidraw extension", got["error"])
	}
}

func TestArtifactPutUnknownFeature(t *testing.T) {
	repoDir, projDir := newBackend(t)
	var got map[string]string
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodPut, "/api/artifacts/F-9999/seq.excalidraw", `{}`, &got)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body: %s", rec.Code, rec.Body.String())
	}
	if got["error"] != "feature not found" {
		t.Fatalf("error = %q, want %q", got["error"], "feature not found")
	}
}

func TestArtifactPutTraversalRejected(t *testing.T) {
	repoDir, projDir := newBackend(t)
	seedFeatureForArtifacts(t, repoDir)
	var got map[string]string
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodPut, "/api/artifacts/F-0001/..%2Fevil.excalidraw", `{}`, &got)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(repoDir, "content", "artifacts", "evil.excalidraw")); err == nil {
		t.Fatalf("traversal PUT escaped the feature artifact dir")
	}
}

const mermaidA = "sequenceDiagram\n  A->>B: olá\n"

// TestArtifactGetMmd serves a .mmd artifact as text/plain with its raw body.
func TestArtifactGetMmd(t *testing.T) {
	repoDir, projDir := newBackend(t)
	writeArtifact(t, repoDir, "F-0001", "sequence.mmd", mermaidA)

	rec := httptest.NewRecorder()
	New(repoDir, projDir, "").ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet, "/api/artifacts/F-0001/sequence.mmd", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type = %q, want text/plain", ct)
	}
	if rec.Body.String() != mermaidA {
		t.Fatalf("body = %q, want raw mermaid %q", rec.Body.String(), mermaidA)
	}
}

// TestArtifactPutMmdCreate creates a .mmd artifact from a plain-text body
// (no JSON validation) and advances origin.
func TestArtifactPutMmdCreate(t *testing.T) {
	repoDir, projDir, origin := newGitBackend(t)
	head0, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA origin: %v", err)
	}
	var got struct {
		OK bool `json:"ok"`
	}
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodPut, "/api/artifacts/F-0001/sequence.mmd", mermaidA, &got)
	if rec.Code != http.StatusOK || !got.OK {
		t.Fatalf("create status = %d body = %s, want 200 {\"ok\":true}", rec.Code, rec.Body.String())
	}
	data, err := os.ReadFile(filepath.Join(repoDir, "content", "artifacts", "F-0001", "sequence.mmd"))
	if err != nil {
		t.Fatalf(".mmd not written to backend: %v", err)
	}
	if string(data) != mermaidA {
		t.Fatalf("backend file = %q, want %q", data, mermaidA)
	}
	head1, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA origin: %v", err)
	}
	if head1 == head0 {
		t.Fatalf("origin HEAD did not advance after .mmd create (still %s)", head1)
	}
}

// TestArtifactPutMmdEmpty rejects an empty .mmd body with 400.
func TestArtifactPutMmdEmpty(t *testing.T) {
	repoDir, projDir := newBackend(t)
	seedFeatureForArtifacts(t, repoDir)
	var got map[string]string
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodPut, "/api/artifacts/F-0001/sequence.mmd", "", &got)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", rec.Code, rec.Body.String())
	}
	if got["error"] == "" {
		t.Fatalf("body = %v, want pt-BR JSON error message", got)
	}
}

// TestArtifactPutUnknownExtension rejects extensions other than
// .excalidraw / .mmd with 400 naming both accepted extensions.
func TestArtifactPutUnknownExtension(t *testing.T) {
	repoDir, projDir := newBackend(t)
	seedFeatureForArtifacts(t, repoDir)
	var got map[string]string
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodPut, "/api/artifacts/F-0001/notes.txt", "hello", &got)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(got["error"], ".excalidraw") || !strings.Contains(got["error"], ".mmd") {
		t.Fatalf("error = %q, want message naming .excalidraw and .mmd", got["error"])
	}
}

func seedFeatureForArtifacts(t *testing.T, repoDir string) {
	t.Helper()
	seedFeature(t, repoDir, "F-0001", "Feature A", "candidate")
}
