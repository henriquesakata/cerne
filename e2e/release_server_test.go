package e2e_test

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// A local stand-in for the GitHub release pages, shared by the black-box
// tests of install.sh (F-0357) and install.ps1 (F-0410).

const fakeLatest = "v9.9.9"

func fakeBinary(version string) string {
	return "#!/bin/sh\necho cerne " + version + "\n"
}

// releaseServer serves /releases/latest (a redirect to the fakeLatest tag),
// every asset of any version, and its SHA256SUMS. corruptSums makes the
// published checksums disagree with the assets.
func releaseServer(t *testing.T, corruptSums bool) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		p := r.URL.Path
		switch {
		case p == "/releases/latest":
			http.Redirect(w, r, "/releases/tag/"+fakeLatest, http.StatusFound)
		case strings.HasPrefix(p, "/releases/tag/"):
			w.WriteHeader(http.StatusOK)
		case strings.HasPrefix(p, "/releases/download/"):
			parts := strings.Split(strings.TrimPrefix(p, "/releases/download/"), "/")
			if len(parts) != 2 {
				http.NotFound(w, r)
				return
			}
			version, name := parts[0], parts[1]
			if name != "SHA256SUMS" {
				fmt.Fprint(w, fakeBinary(version))
				return
			}
			body := fakeBinary(version)
			if corruptSums {
				body += "tampered"
			}
			sum := sha256.Sum256([]byte(body))
			for _, target := range []string{"darwin_amd64", "darwin_arm64", "linux_amd64", "linux_arm64", "windows_amd64.exe"} {
				fmt.Fprintf(w, "%x  cerne_%s_%s\n", sum, version, target)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}
