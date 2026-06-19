package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUICmdPullIntervalFlag(t *testing.T) {
	cmd := newUICmd()
	f := cmd.Flags().Lookup("pull-interval")
	if f == nil {
		t.Fatal("ui command is missing the --pull-interval flag")
	}
	if f.DefValue != "15s" {
		t.Fatalf("default pull-interval = %q, want 15s", f.DefValue)
	}
	// 0 disables the periodic pull (offline-by-default).
	if err := cmd.Flags().Set("pull-interval", "0"); err != nil {
		t.Fatalf("Set pull-interval=0: %v", err)
	}
	d, err := cmd.Flags().GetDuration("pull-interval")
	if err != nil {
		t.Fatalf("GetDuration: %v", err)
	}
	if d != 0 {
		t.Fatalf("pull-interval = %v, want 0", d)
	}
}

// fakeUIHost stands in for a running cerne ui: it answers the project endpoints
// the join path uses.
func fakeUIHost(t *testing.T, onRegister func(dir string)) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/projects", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"projects":[],"default":""}`))
	})
	mux.HandleFunc("POST /api/projects", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Dir string `json:"dir"`
		}
		_ = json.NewDecoder(r.Body).Decode(&b)
		if onRegister != nil {
			onRegister(b.Dir)
		}
		_, _ = w.Write([]byte(`{"pid":"proj-x","name":"Projeto X"}`))
	})
	mux.HandleFunc("DELETE /api/projects/{pid}", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"removed":true}`))
	})
	return httptest.NewServer(mux)
}

func TestProbeAcceptsCerneUI(t *testing.T) {
	srv := fakeUIHost(t, nil)
	defer srv.Close()
	if !probeCerneUI(srv.URL) {
		t.Fatal("probeCerneUI should accept a server exposing /api/projects")
	}
}

func TestProbeRejectsNonCerne(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("hello, not cerne"))
	}))
	defer srv.Close()
	if probeCerneUI(srv.URL) {
		t.Fatal("probeCerneUI should reject a non-cerne server on the port")
	}
}

func TestRegisterAndDeregisterProject(t *testing.T) {
	var gotDir string
	srv := fakeUIHost(t, func(dir string) { gotDir = dir })
	defer srv.Close()

	pid, name, err := registerProject(srv.URL, `C:\proj\x`)
	if err != nil {
		t.Fatalf("registerProject: %v", err)
	}
	if pid != "proj-x" || name != "Projeto X" {
		t.Fatalf("register returned %q/%q, want proj-x/Projeto X", pid, name)
	}
	if gotDir != `C:\proj\x` {
		t.Fatalf("host received dir %q, want the absolute dir sent by the client", gotDir)
	}
	if err := deregisterProject(srv.URL, pid); err != nil {
		t.Fatalf("deregisterProject: %v", err)
	}
}
