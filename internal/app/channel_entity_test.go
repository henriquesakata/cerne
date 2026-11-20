package app

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
)

// postTo posts a human message about ref addressed to sessions to, as the
// feature page does (F-0486).
func (f *chanFixture) postTo(ref string, to []string, body string) int {
	f.t.Helper()
	f.at = f.at.Add(time.Second)
	m, err := ChannelPost(f.cacheDir, "Henrique", ref, to, body, f.at)
	if err != nil {
		f.t.Fatalf("ChannelPost: %v", err)
	}
	return m.Seq
}

// withDoneSession binds session s-dev to a work that already closed: the
// implementer of a card that reached implemented.
func withDoneSession(t *testing.T, f *chanFixture) {
	t.Helper()
	bindTestWork(t, f.repoDir, "W-0009", domain.WorkActive)
	if err := BindSession(f.cacheDir, "s-dev", "W-0009"); err != nil {
		t.Fatal(err)
	}
	bindTestWork(t, f.repoDir, "W-0009", domain.WorkDone)
}

// TestChannelToSessionDeliveredWithoutLiveBinding (F-0486 crit. 8, 9, 10, 11):
// a message from the card's page reaches only the sessions it names — even
// one whose work already closed — names the card and how to answer, repeats
// until answered, and the answer, from a session with no live work, counts
// and stays on the thread.
func TestChannelToSessionDeliveredWithoutLiveBinding(t *testing.T) {
	f := newChanFixture(t)
	withDoneSession(t, f)
	seq := f.postTo("F-0001", []string{"s-dev"}, "o teste do canal quebrou, corrige?")

	want := fmt.Sprintf("#%d Henrique (human) → you, about F-0001: o teste do canal quebrou", seq)
	for i := range 2 {
		out := f.inject("s-dev", true)
		if !strings.Contains(out, want) || !strings.Contains(out, `cerne msg send "<text>"`) {
			t.Fatalf("s-dev injection %d = %q, want the addressed message and how to answer", i+1, out)
		}
	}
	for _, sess := range []string{"s1", "s2", "s-none"} {
		if out := f.inject(sess, true); strings.Contains(out, "corrige") {
			t.Fatalf("%s got a message addressed to s-dev only: %q", sess, out)
		}
	}
	msgs, _ := f.read("")
	got := stateOf(t, msgs, seq)
	if !got.Directed || len(got.Targets) != 1 || got.Targets[0] != (ChannelTarget{Work: "F-0001·s-dev", Session: "s-dev", State: ChannelDelivered}) {
		t.Fatalf("before the answer = %+v, want one delivered target s-dev", got)
	}
	if got.To != nil || got.Session != "" {
		t.Fatalf("session ids served: to=%v session=%q", got.To, got.Session)
	}

	reply, to, err := ChannelSend(f.cacheDir, f.repoDir, "s-dev", "corrigido no PR", f.at.Add(time.Second))
	f.at = f.at.Add(time.Second)
	if err != nil {
		t.Fatalf("reply without live work: %v", err)
	}
	if reply.Nick != "F-0001·s-dev" || reply.Ref != "F-0001" || len(to) != 0 {
		t.Fatalf("reply = %+v to %v, want nick F-0001·s-dev, ref F-0001, broadcast to nobody", reply, to)
	}
	if out := f.inject("s-dev", true); out != "" {
		t.Fatalf("after the answer: %q, want zero bytes", out)
	}
	for _, sess := range []string{"s1", "s2"} {
		if out := f.inject(sess, true); strings.Contains(out, "corrigido") {
			t.Fatalf("%s got the reply meant for the page: %q", sess, out)
		}
	}
	msgs, _ = f.read("")
	if got := stateOf(t, msgs, seq); got.State != ChannelAnswered || got.Targets[0].State != ChannelAnswered || got.Targets[0].Acked {
		t.Fatalf("after the answer = %+v, want answered by a reply", got)
	}
	if r := stateOf(t, msgs, reply.Seq); r.Ref != "F-0001" {
		t.Fatalf("reply on the thread = %+v, want it alive with ref F-0001", r)
	}

	// crit. 13: the addressed message and its reply expire in 24h too.
	msgs, _, err = ChannelRead(f.cacheDir, f.repoDir, "", f.at.Add(25*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range msgs {
		if m.Seq == seq || m.Seq == reply.Seq {
			t.Fatalf("#%d alive after 24h", m.Seq)
		}
	}
}

// TestChannelToSessionNotDeliveredTarget: a session that has not come back
// shows not_delivered — the page's hint to open it.
func TestChannelToSessionNotDeliveredTarget(t *testing.T) {
	f := newChanFixture(t)
	seq := f.postTo("F-0001", []string{"s-idle", "s1"}, "dúvida sobre o plano")
	f.inject("s1", true)
	msgs, _ := f.read("")
	got := stateOf(t, msgs, seq)
	if got.State != ChannelNotDelivered || len(got.Targets) != 2 || got.Targets[0].State != ChannelNotDelivered || got.Targets[1].State != ChannelDelivered {
		t.Fatalf("targets = %+v, want s-idle not delivered, s1 delivered", got.Targets)
	}
}

// TestChannelReplyWithLiveWorkCarriesRef: a session with live work answers
// as its work, and its answer joins the card's thread.
func TestChannelReplyWithLiveWorkCarriesRef(t *testing.T) {
	f := newChanFixture(t)
	seq := f.postTo("F-0001", []string{"s1"}, "quem escreveu o plano?")
	f.inject("s1", true)
	reply, _, err := ChannelSend(f.cacheDir, f.repoDir, "s1", "fui eu", f.at.Add(time.Second))
	f.at = f.at.Add(time.Second)
	if err != nil || reply.Nick != "W-0001" || reply.Ref != "F-0001" {
		t.Fatalf("reply = %+v (%v), want W-0001 on the F-0001 thread", reply, err)
	}
	msgs, _ := f.read("")
	if got := stateOf(t, msgs, seq); got.State != ChannelAnswered {
		t.Fatalf("state = %+v, want answered", got)
	}
}

// TestChannelToSessionAckAndStop: the Stop reminder and the ack work for an
// addressed session with no work; anyone else's ack is refused.
func TestChannelToSessionAckAndStop(t *testing.T) {
	f := newChanFixture(t)
	withDoneSession(t, f)
	seq := f.postTo("F-0001", []string{"s-dev"}, "viu o comentário do PR?")
	f.at = f.at.Add(time.Second)
	text, mark := ChannelStopReminder(f.cacheDir, f.repoDir, "s-dev", f.at)
	if !strings.Contains(text, fmt.Sprintf("#%d Henrique (human) → you, about F-0001", seq)) {
		t.Fatalf("Stop reminder = %q, want the addressed message", text)
	}
	mark()
	if text, _ := ChannelStopReminder(f.cacheDir, f.repoDir, "s1", f.at); strings.Contains(text, "PR") {
		t.Fatalf("Stop reminder of another session = %q", text)
	}
	if _, _, err := ChannelAck(f.cacheDir, f.repoDir, "s-ghost", []int{seq}, f.at); !errors.Is(err, ErrNoChannelIdentity) {
		t.Fatalf("ack by an unaddressed session with no work: err = %v, want ErrNoChannelIdentity", err)
	}
	acked, _, err := ChannelAck(f.cacheDir, f.repoDir, "s-dev", []int{seq}, f.at)
	if err != nil || !slices.Equal(acked, []int{seq}) {
		t.Fatalf("ack = %v (%v)", acked, err)
	}
	msgs, _ := f.read("")
	if got := stateOf(t, msgs, seq); got.State != ChannelAnswered || !got.Targets[0].Acked {
		t.Fatalf("after the ack = %+v, want answered and acked", got)
	}
}

// TestChannelSendWithoutWorkNorAddressRefused: with no work and nothing
// addressed to it, a session still has no identity (F-0325 unchanged).
func TestChannelSendWithoutWorkNorAddressRefused(t *testing.T) {
	f := newChanFixture(t)
	f.postTo("F-0001", []string{"s-dev"}, "só para o s-dev")
	if _, _, err := ChannelSend(f.cacheDir, f.repoDir, "s-ghost", "oi", f.at); !errors.Is(err, ErrNoChannelIdentity) {
		t.Fatalf("err = %v, want ErrNoChannelIdentity", err)
	}
}
