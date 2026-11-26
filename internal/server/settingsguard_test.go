package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// localRequest is httptest.NewRequest as the local ui sends it: a loopback
// Host, which every request to it requires (B-0043, B-0044) — httptest's own is
// example.com — and, on a write, the page's Origin, which the browser sends
// and the local ui requires (B-0091).
func localRequest(method, target string, body io.Reader) *http.Request {
	req := httptest.NewRequest(method, target, body)
	req.Host = "127.0.0.1:7717"
	if method != http.MethodGet && method != http.MethodHead {
		req.Header.Set("Origin", "http://"+req.Host)
	}
	return req
}

// doLocal is doJSON from the local ui: a loopback Host, as the browser sends
// to cerne ui, which the settings writes require (F-0449).
func doLocal(t *testing.T, h http.Handler, method, target, body string, out any) *httptest.ResponseRecorder {
	t.Helper()
	return doJSONWith(t, h, method, target, body, map[string]string{"Host": "127.0.0.1:7717"}, out)
}

// doJSONWith is doJSON with request headers; "Host" sets the request's host,
// and a write's page Origin follows it unless headers name one. An empty value
// drops the header ("Origin": "" is a write with no Origin).
func doJSONWith(t *testing.T, h http.Handler, method, target, body string, headers map[string]string, out any) *httptest.ResponseRecorder {
	t.Helper()
	req := localRequest(method, target, strings.NewReader(body))
	for k, v := range headers {
		switch {
		case k == "Host":
			req.Host = v
			if _, named := headers["Origin"]; !named && req.Header.Get("Origin") != "" {
				req.Header.Set("Origin", "http://"+v)
			}
		case v == "":
			req.Header.Del(k)
		default:
			req.Header.Set(k, v)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if out != nil {
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Fatalf("%s %s Content-Type = %q, want application/json", method, target, ct)
		}
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			t.Fatalf("%s %s: invalid JSON %q: %v", method, target, rec.Body.String(), err)
		}
	}
	return rec
}

// F-0449 (audit finding 3): a page in the user's browser must not change
// mirror.trackers or mirror.enabled, by DNS rebinding (a foreign Host) or a
// cross-site request (a foreign Origin). Settings writes need a loopback Host
// and a same-origin JSON request.
func TestSettingsWritesRefuseForeignHostOrOrigin(t *testing.T) {
	put := func(t *testing.T, headers map[string]string) int {
		t.Helper()
		h, _, _ := settingsBackend(t, "")
		return doJSONWith(t, h, http.MethodPut, "/api/settings/wiki.enabled", `{"scope":"host","value":false}`, headers, nil).Code
	}
	del := func(t *testing.T, headers map[string]string) int {
		t.Helper()
		h, _, _ := settingsBackend(t, "")
		return doJSONWith(t, h, http.MethodDelete, "/api/settings/wiki.enabled?scope=host", "", headers, nil).Code
	}
	for _, tc := range []struct {
		name    string
		headers map[string]string
		want    int
	}{
		{"foreign host", map[string]string{"Host": "evil.example:7717", "Content-Type": "application/json"}, http.StatusForbidden},
		{"rebound name, same origin", map[string]string{"Host": "attacker.example", "Origin": "http://attacker.example", "Content-Type": "application/json"}, http.StatusForbidden},
		{"foreign origin", map[string]string{"Host": "127.0.0.1:7717", "Origin": "https://evil.example", "Content-Type": "application/json"}, http.StatusForbidden},
		{"not json", map[string]string{"Host": "localhost:7717", "Content-Type": "text/plain"}, http.StatusForbidden},
		{"same-origin loopback", map[string]string{"Host": "127.0.0.1:7717", "Origin": "http://127.0.0.1:7717", "Content-Type": "application/json"}, http.StatusOK},
		{"localhost", map[string]string{"Host": "localhost:7717", "Content-Type": "application/json"}, http.StatusOK},
		{"ipv6 loopback", map[string]string{"Host": "[::1]:7717", "Content-Type": "application/json"}, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := put(t, tc.headers); got != tc.want {
				t.Errorf("PUT status = %d, want %d", got, tc.want)
			}
			if got := del(t, tc.headers); got != tc.want {
				t.Errorf("DELETE status = %d, want %d", got, tc.want)
			}
		})
	}
}
