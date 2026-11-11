package notify

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/project"
)

func testEvent() Event {
	return Event{
		ID:     "F-0206",
		Kind:   "feature",
		Action: "approve",
		From:   "candidate",
		To:     "approved",
		Actor:  "local",
		Title:  "Webhook em transições de estado",
		At:     time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC),
	}
}

// TestSendJSONPayload (crit. 1): default format posts the full event as JSON.
func TestSendJSONPayload(t *testing.T) {
	var got Event
	var method, contentType string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		contentType = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v", err)
		}
	}))
	defer ts.Close()

	if err := Send(project.WebhookConfig{URL: ts.URL}, testEvent()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if method != http.MethodPost {
		t.Fatalf("method = %q, want POST", method)
	}
	if contentType != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", contentType)
	}
	if got != testEvent() {
		t.Fatalf("payload = %+v, want %+v", got, testEvent())
	}
}

// TestSendServerErrorReturnsError (crit. 2): a >=400 response is reported to
// the caller as an error — Send itself never panics or blocks past timeout.
func TestSendServerErrorReturnsError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	if err := Send(project.WebhookConfig{URL: ts.URL}, testEvent()); err == nil {
		t.Fatal("Send: want error on 500, got nil")
	}
}

// TestSendTimeout (crit. 2): a server slower than the configured timeout
// returns an error instead of hanging past it.
func TestSendTimeout(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
	}))
	defer ts.Close()

	start := time.Now()
	err := Send(project.WebhookConfig{URL: ts.URL, Timeout: 1}, testEvent())
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("Send: want error on timeout, got nil")
	}
	if elapsed >= 2*time.Second {
		t.Fatalf("Send took %s, want it to time out around 1s, not wait for the 2s server", elapsed)
	}
}

// TestSendSlackFormat (crit. 3): format = "slack" posts {"text": …} instead
// of the full event.
func TestSendSlackFormat(t *testing.T) {
	var body map[string]string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
	}))
	defer ts.Close()

	if err := Send(project.WebhookConfig{URL: ts.URL, Format: "slack"}, testEvent()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(body) != 1 || body["text"] == "" {
		t.Fatalf("slack payload = %+v, want single non-empty 'text' key", body)
	}
	want := "F-0206 Webhook em transições de estado: candidate → approved (local)"
	if body["text"] != want {
		t.Fatalf("text = %q, want %q", body["text"], want)
	}
}

// TestSendNoURLIsNoop (crit. 4): absent config sends nothing.
func TestSendNoURLIsNoop(t *testing.T) {
	called := false
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer ts.Close()

	if err := Send(project.WebhookConfig{}, testEvent()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if called {
		t.Fatal("Send called the server with no URL configured")
	}
}
