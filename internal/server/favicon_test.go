package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// F-0461: the tab icon is a file at the root of the embedded front-end. The
// SPA fallback must serve it as an image, not answer with index.html.
func TestSPAHandlerServesFavicon(t *testing.T) {
	rec := httptest.NewRecorder()
	spaHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/favicon.svg", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /favicon.svg: status %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "image/svg+xml") {
		t.Errorf("Content-Type %q, want image/svg+xml", ct)
	}
	if body := rec.Body.String(); !strings.HasPrefix(body, "<svg") {
		t.Errorf("the body does not start with <svg: %.40q", body)
	}
}
