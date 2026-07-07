package cli

import (
	"bytes"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestDoctorTimingsPrintsRing pins criterion 5 (F-0149): `cerne doctor
// --timings` renders the server's recent operations with phases and retries.
func TestDoctorTimingsPrintsRing(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/debug/timings" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"op_id":"abc123","kind":"http","route":"/features/{id}/transition",` +
			`"status":"200","start":"2026-07-07T00:00:00Z","dur_ns":4200000000,"ms":4200,"retries":1,` +
			`"phases":[{"name":"push","dur_ns":1800000000,"ms":1800}]}]`))
	}))
	defer ts.Close()
	_, portStr, err := net.SplitHostPort(strings.TrimPrefix(ts.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	root := Root()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"doctor", "--timings", "--port", portStr})
	if err := root.Execute(); err != nil {
		t.Fatalf("doctor --timings: %v\n%s", err, out.String())
	}
	got := out.String()
	for _, want := range []string{"/features/{id}/transition", "retries=1", "push 1800ms"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}

// TestDoctorTimingsNoServer pins the offline path: a clear error, not a hang.
func TestDoctorTimingsNoServer(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close() // freed: nothing listens here now

	var out bytes.Buffer
	root := Root()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"doctor", "--timings", "--port", strconv.Itoa(port)})
	start := time.Now()
	execErr := root.Execute()
	if execErr == nil {
		t.Fatal("want an error when no server is listening")
	}
	if !strings.Contains(execErr.Error(), "no cerne server") {
		t.Fatalf("error = %v, want the no-server message", execErr)
	}
	if time.Since(start) > 10*time.Second {
		t.Fatal("doctor hung instead of failing fast")
	}
}
