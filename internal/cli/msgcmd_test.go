package cli

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/app"
)

// startBoundWork registers a work in the project and binds session sess to it,
// returning the minted W- id.
func startBoundWork(t *testing.T, projDir, repoDir, sess, title string, extra ...string) string {
	t.Helper()
	out, err := execCerne(t, append([]string{"work", "start", title, "--dir", projDir}, extra...)...)
	if err != nil {
		t.Fatalf("work start: %v\n%s", err, out)
	}
	id := workIDRe.FindString(out)
	if id == "" {
		t.Fatalf("no W- id in %q", out)
	}
	if err := app.BindSession(filepath.Dir(repoDir), sess, id); err != nil {
		t.Fatal(err)
	}
	return id
}

// TestMsgSendWithoutRoomID (F-0325 crit. 1/12): `msg send` needs no room id —
// it lands on the permanent channel — and tells the sender who will get it.
func TestMsgSendWithoutRoomID(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	a := startBoundWork(t, projDir, repoDir, "sess-a", "Mexer no servidor")
	b := startBoundWork(t, projDir, repoDir, "sess-b", "Revisar a interface")

	out, err := execCerne(t, "msg", "send", "segura internal/server", "--session", "sess-a", "--dir", projDir)
	if err != nil {
		t.Fatalf("msg send: %v\n%s", err, out)
	}
	if want := fmt.Sprintf("sent #1 — 1 live session will receive it: %s\n", b); out != want {
		t.Fatalf("output = %q, want %q", out, want)
	}

	t.Setenv("CERNE_SESSION", "sess-b")
	out, err = execCerne(t, "msg", "send", "ok", "--dir", projDir)
	if err != nil || !strings.Contains(out, "sent #2") || !strings.Contains(out, a) {
		t.Fatalf("send via CERNE_SESSION: %v\n%s", err, out)
	}
}

// TestMsgSendToNobodySaysSo (crit. 12): with no other live session the send
// says so and says how long the message waits.
func TestMsgSendToNobodySaysSo(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	a := startBoundWork(t, projDir, repoDir, "sess-a", "Mexer no servidor")
	out, err := execCerne(t, "msg", "send", "alguém aí?", "--session", "sess-a", "--dir", projDir)
	if err != nil {
		t.Fatalf("msg send: %v\n%s", err, out)
	}
	for _, want := range []string{"no live session", "stays in the channel until it expires", "24h, or when " + a + " closes"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

// TestMsgSendRequiresRegistration: an unregistered session gets the actionable
// pointer, and no session at all says where one comes from.
func TestMsgSendRequiresRegistration(t *testing.T) {
	projDir, _, _ := setupProject(t)
	t.Setenv("CERNE_SESSION", "")
	if out, err := execCerne(t, "msg", "send", "oi", "--session", "sess-x", "--dir", projDir); err == nil || !strings.Contains(err.Error(), "cerne work start") {
		t.Fatalf("unregistered send: err = %v\n%s", err, out)
	}
	if out, err := execCerne(t, "msg", "send", "oi", "--dir", projDir); err == nil || !strings.Contains(err.Error(), "CERNE_SESSION") {
		t.Fatalf("sessionless send: err = %v\n%s", err, out)
	}
}

// TestMsgReadFullHistoryNoCap (crit. 13/18): the explicit read is every live
// message, past the injection cap, with its derived state; --json carries
// state, targets and the roster.
func TestMsgReadFullHistoryNoCap(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	a := startBoundWork(t, projDir, repoDir, "sess-a", "Mexer no servidor")
	b := startBoundWork(t, projDir, repoDir, "sess-b", "Revisar a interface")
	for i := range 7 {
		if out, err := execCerne(t, "msg", "send", fmt.Sprintf("ruído %d", i), "--session", "sess-a", "--dir", projDir); err != nil {
			t.Fatalf("send: %v\n%s", err, out)
		}
	}
	if out, err := execCerne(t, "msg", "send", b+" pode consumir", "--session", "sess-a", "--dir", projDir); err != nil {
		t.Fatalf("send: %v\n%s", err, out)
	}

	out, err := execCerne(t, "msg", "read", "--dir", projDir)
	if err != nil {
		t.Fatalf("msg read: %v\n%s", err, out)
	}
	var lines int
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "#") {
			lines++
		}
	}
	if lines != 8 || !strings.Contains(out, "roster: ") || !strings.Contains(out, a+" (live)") {
		t.Fatalf("read = %d message lines, want 8 plus the roster:\n%s", lines, out)
	}
	if !strings.Contains(out, fmt.Sprintf("#8 %s → %s [not_delivered] %s pode consumir", a, b, b)) {
		t.Fatalf("addressed line missing its target/state:\n%s", out)
	}

	out, err = execCerne(t, "msg", "read", "--json", "--dir", projDir)
	if err != nil {
		t.Fatalf("msg read --json: %v\n%s", err, out)
	}
	var got struct {
		Messages []app.ChannelMessage `json:"messages"`
		Roster   []app.ChannelMember  `json:"roster"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if len(got.Messages) != 8 || got.Messages[7].State != app.ChannelNotDelivered || len(got.Messages[7].Targets) != 1 ||
		got.Messages[7].Targets[0].Work != b || len(got.Roster) != 2 {
		t.Fatalf("json = %+v", got)
	}
}

// TestMsgAck (crit. 10): ack silences an addressed message without a reply.
func TestMsgAck(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	startBoundWork(t, projDir, repoDir, "sess-a", "Mexer no servidor")
	b := startBoundWork(t, projDir, repoDir, "sess-b", "Revisar a interface")
	if out, err := execCerne(t, "msg", "send", b+" segura", "--session", "sess-a", "--dir", projDir); err != nil {
		t.Fatalf("send: %v\n%s", err, out)
	}
	out, err := execCerne(t, "msg", "ack", "1", "--session", "sess-b", "--dir", projDir)
	if err != nil || out != "acked #1\n" {
		t.Fatalf("msg ack: %v\n%q", err, out)
	}
	out, err = execCerne(t, "msg", "read", "--dir", projDir)
	if err != nil || !strings.Contains(out, "[answered]") {
		t.Fatalf("read after ack: %v\n%s", err, out)
	}
	if out, err := execCerne(t, "msg", "ack", "x", "--session", "sess-b", "--dir", projDir); err == nil {
		t.Fatalf("non-numeric seq accepted:\n%s", out)
	}
}
