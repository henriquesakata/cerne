package server

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var pngBytes = append([]byte("\x89PNG\x0d\x0a\x1a\x0a"), bytes.Repeat([]byte{0}, 64)...)

// attachServer is a server over a throwaway backend whose cache is a temp dir.
func attachServer(t *testing.T) (h http.Handler, cache, repoDir, projDir string) {
	t.Helper()
	cache = t.TempDir()
	t.Setenv("CERNE_CACHE_DIR", cache)
	repoDir, projDir = newBackend(t)
	return New(repoDir, projDir, "proj-attach"), cache, repoDir, projDir
}

func uploadAttachment(t *testing.T, h http.Handler, conv, contentType string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := localRequest(http.MethodPost, "/api/chat/attachments?conversation_id="+conv, bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func uploadedID(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		ID        string `json:"id"`
		MediaType string `json:"media_type"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || !attachmentIDPattern.MatchString(got.ID) || got.MediaType != "image/png" {
		t.Fatalf("upload body = %s (err=%v), want a server-made id and image/png", rec.Body.String(), err)
	}
	return got.ID
}

// filesUnder lists every regular file under root, relative to it.
func filesUnder(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(root, p)
			out = append(out, rel)
		}
		return nil
	})
	return out
}

// F-0520 crit. 3: the upload lands in the project cache, under the
// conversation, with a name the server made — and nowhere else.
func TestChatAttachUploadWritesOnlyToCache(t *testing.T) {
	h, cache, repoDir, projDir := attachServer(t)
	repoBefore, projBefore := filesUnder(t, repoDir), filesUnder(t, projDir)

	id := uploadedID(t, uploadAttachment(t, h, "conv-1", "image/png", pngBytes))

	want := filepath.Join(cache, "proj-attach", "chat-attachments", "conv-1", id)
	got, err := os.ReadFile(want)
	if err != nil || !bytes.Equal(got, pngBytes) {
		t.Fatalf("attachment not at %s (err=%v)", want, err)
	}
	if a, b := filesUnder(t, repoDir), filesUnder(t, projDir); len(a) != len(repoBefore) || len(b) != len(projBefore) {
		t.Fatalf("upload wrote outside the cache: backend %v, project %v", a, b)
	}
}

// F-0520 crit. 4: the type is read from the content, never from the header,
// and the size is capped.
func TestChatAttachUploadRefusals(t *testing.T) {
	h, _, _, _ := attachServer(t)
	for name, tc := range map[string]struct {
		contentType string
		body        []byte
		want        int
	}{
		"text claiming to be png": {"image/png", []byte("<html><script>alert(1)</script></html>"), http.StatusUnsupportedMediaType},
		"svg":                     {"image/svg+xml", []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), http.StatusUnsupportedMediaType},
		"empty":                   {"image/png", nil, http.StatusUnsupportedMediaType},
		"over the size cap":       {"image/png", append(bytes.Clone(pngBytes), make([]byte, maxChatImageBytes)...), http.StatusRequestEntityTooLarge},
	} {
		if rec := uploadAttachment(t, h, "conv-1", tc.contentType, tc.body); rec.Code != tc.want {
			t.Errorf("%s: status = %d, want %d; body: %s", name, rec.Code, tc.want, rec.Body.String())
		}
	}
}

// F-0520 crit. 11: neither id from the browser builds a path unvalidated.
func TestChatAttachRefusesIDsOutsideThePattern(t *testing.T) {
	h, cache, _, _ := attachServer(t)
	for _, conv := range []string{"", "..", "a%2Fb", "a.b", strings.Repeat("x", 65)} {
		if rec := uploadAttachment(t, h, conv, "image/png", pngBytes); rec.Code != http.StatusBadRequest {
			t.Errorf("upload with conversation_id %q: status = %d, want 400", conv, rec.Code)
		}
	}
	id := uploadedID(t, uploadAttachment(t, h, "conv-1", "image/png", pngBytes))
	secret := filepath.Join(cache, "proj-attach", "chat-attachments", "secret.png")
	if err := os.WriteFile(secret, pngBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"/api/chat/attachments/conv-1/..%2Fsecret.png",
		"/api/chat/attachments/conv-1/secret.png",
		"/api/chat/attachments/conv-1/" + strings.ToUpper(id),
		"/api/chat/attachments/a.b/" + id,
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, localRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("GET %s: status = %d, want 400", path, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, localRequest(http.MethodDelete, "/api/chat/attachments/a.b", nil))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("DELETE with a bad conversation id: status = %d, want 400", rec.Code)
	}
}

// F-0520 crit. 3: a page of another origin cannot plant a file.
func TestChatAttachUploadRefusesCrossOrigin(t *testing.T) {
	h, _, _, _ := attachServer(t)
	req := localRequest(http.MethodPost, "/api/chat/attachments?conversation_id=conv-1", bytes.NewReader(pngBytes))
	req.Header.Set("Content-Type", "image/png")
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-origin upload status = %d, want 403", rec.Code)
	}
}

// F-0520 crit. 9: the stored image is served back; a gone one is a 404.
func TestChatAttachGet(t *testing.T) {
	h, cache, _, _ := attachServer(t)
	id := uploadedID(t, uploadAttachment(t, h, "conv-1", "image/png", pngBytes))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, localRequest(http.MethodGet, "/api/chat/attachments/conv-1/"+id, nil))
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), pngBytes) || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("GET status = %d, type = %q, want the png back", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("GET must forbid sniffing, headers: %v", rec.Header())
	}

	if err := os.Remove(filepath.Join(cache, "proj-attach", "chat-attachments", "conv-1", id)); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, localRequest(http.MethodGet, "/api/chat/attachments/conv-1/"+id, nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET of an expired attachment: status = %d, want 404", rec.Code)
	}
}

// F-0520 crit. 10: deleting the conversation deletes its attachments, and an
// upload sweeps the directories nobody touched for 30 days.
func TestChatAttachDeleteAndSweep(t *testing.T) {
	h, cache, _, _ := attachServer(t)
	root := filepath.Join(cache, "proj-attach", "chat-attachments")
	uploadedID(t, uploadAttachment(t, h, "conv-old", "image/png", pngBytes))
	uploadedID(t, uploadAttachment(t, h, "conv-del", "image/png", pngBytes))
	uploadedID(t, uploadAttachment(t, h, "conv-keep", "image/png", pngBytes))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, localRequest(http.MethodDelete, "/api/chat/attachments/conv-del", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE status = %d, want 204", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(root, "conv-del")); !os.IsNotExist(err) {
		t.Fatalf("conv-del still there (err=%v)", err)
	}

	old := time.Now().Add(-chatAttachmentTTL - time.Hour)
	if err := os.Chtimes(filepath.Join(root, "conv-old"), old, old); err != nil {
		t.Fatal(err)
	}
	uploadedID(t, uploadAttachment(t, h, "conv-new", "image/png", pngBytes))
	if _, err := os.Stat(filepath.Join(root, "conv-old")); !os.IsNotExist(err) {
		t.Fatalf("conv-old survived the sweep (err=%v)", err)
	}
	for _, keep := range []string{"conv-keep", "conv-new"} {
		if _, err := os.Stat(filepath.Join(root, keep)); err != nil {
			t.Fatalf("%s was swept: %v", keep, err)
		}
	}
}
