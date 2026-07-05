package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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

// fakeStreamHost stands in for a host serving /api/stream: it writes the
// on-connect resync event and holds the connection until the server closes.
func fakeStreamHost(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/stream", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: projects\ndata: \n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-r.Context().Done()
	})
	return httptest.NewServer(mux)
}

func TestMonitorHostDetectsDeath(t *testing.T) {
	srv := fakeStreamHost(t)
	done := make(chan error, 1)
	go func() { done <- monitorHost(context.Background(), srv.URL) }()
	time.Sleep(100 * time.Millisecond) // let the monitor attach
	srv.CloseClientConnections()
	srv.Close()
	select {
	case err := <-done:
		if !errors.Is(err, errHostDied) {
			t.Fatalf("monitorHost returned %v, want errHostDied", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("monitorHost did not detect the host death")
	}
}

func TestMonitorHostCtxCancelIsUserExit(t *testing.T) {
	srv := fakeStreamHost(t)
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- monitorHost(ctx, srv.URL) }()
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ctx cancellation should return nil (user exit), got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("monitorHost did not honor ctx cancellation")
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
