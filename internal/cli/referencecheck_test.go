package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/henriquesakata/cerne/internal/store"
)

// checkFixture serves /ok, /moves (its text changes after the first request
// round), /gone (404) and /flaky (503), and registers one reference per path,
// each informing F-0001.
func checkFixture(t *testing.T) (projDir, repoDir string, flip *atomic.Bool) {
	t.Helper()
	flip = new(atomic.Bool)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/gone":
			http.NotFound(w, r)
		case "/flaky":
			w.WriteHeader(http.StatusServiceUnavailable)
		case "/moves":
			w.Header().Set("Content-Type", "text/html")
			if flip.Load() {
				_, _ = w.Write([]byte("<p>new text</p>"))
			} else {
				_, _ = w.Write([]byte("<p>old text</p>"))
			}
		default:
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<p>stable</p>"))
		}
	}))
	t.Cleanup(srv.Close)
	projDir, _, repoDir = setupProject(t)
	if out, err := execCerne(t, "feature", "new", "Uses sources", "--context", "ctx", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\n%s", err, out)
	}
	for _, p := range []string{"ok", "moves", "gone", "flaky"} {
		if out, err := execCerne(t, "reference", "new", "Src "+p, "--url", srv.URL+"/"+p, "--informs", "F-0001", "--dir", projDir); err != nil {
			t.Fatalf("reference new %s: %v\n%s", p, err, out)
		}
	}
	return projDir, repoDir, flip
}

// F-0501 crit. 10: one line per reference with the result, informs on changed
// and gone, and a closing summary.
func TestReferenceCheckOutput(t *testing.T) {
	projDir, repoDir, flip := checkFixture(t)
	out, err := execCerne(t, "reference", "check", "--dir", projDir)
	if err != nil {
		t.Fatalf("check: %v\n%s", err, out)
	}
	want := []string{
		"R-0001  ok\n",
		"R-0002  ok\n",
		"R-0003  gone (HTTP 404)  informs F-0001\n",
		"R-0004  transient (HTTP 503)\n",
		"checked 4: 2 ok, 0 changed, 1 gone, 1 transient\n",
	}
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("output missing %q:\n%s", w, out)
		}
	}
	if r, _ := store.LoadReference(repoDir, "R-0003"); r.Check == nil || r.Check.Status != "gone" {
		t.Fatalf("R-0003 check = %+v", r.Check)
	}

	flip.Store(true)
	out, err = execCerne(t, "reference", "check", "R-0002", "--dir", projDir)
	if err != nil {
		t.Fatalf("check R-0002: %v\n%s", err, out)
	}
	for _, w := range []string{"R-0002  changed  informs F-0001\n", "checked 1: 0 ok, 1 changed, 0 gone, 0 transient\n"} {
		if !strings.Contains(out, w) {
			t.Errorf("output missing %q:\n%s", w, out)
		}
	}
	if strings.Contains(out, "R-0001") {
		t.Errorf("only R-0002 was named:\n%s", out)
	}
}

// F-0501 crit. 6: keep accepts the changed page; any other state is refused in English.
func TestReferenceKeep(t *testing.T) {
	projDir, repoDir, flip := checkFixture(t)
	if out, err := execCerne(t, "reference", "check", "--dir", projDir); err != nil {
		t.Fatalf("check: %v\n%s", err, out)
	}
	flip.Store(true)
	if out, err := execCerne(t, "reference", "check", "R-0002", "--dir", projDir); err != nil {
		t.Fatalf("check: %v\n%s", err, out)
	}
	out, err := execCerne(t, "reference", "keep", "R-0002", "--dir", projDir)
	if err != nil || !strings.Contains(out, "R-0002") || !strings.Contains(out, "baseline") {
		t.Fatalf("keep: %v\n%s", err, out)
	}
	if r, _ := store.LoadReference(repoDir, "R-0002"); r.Check == nil || r.Check.Status != "ok" {
		t.Fatalf("R-0002 check = %+v", r.Check)
	}
	if _, err := execCerne(t, "reference", "keep", "R-0001", "--dir", projDir); err == nil || !strings.Contains(err.Error(), "not in the changed state") {
		t.Fatalf("keep on ok: err = %v", err)
	}
}
