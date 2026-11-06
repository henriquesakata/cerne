package app

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/room"
	"gitlab.com/rockt/cerne/internal/store"
)

var chanT0 = time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)

// saveChanWork persists an active work with the given refs/paths.
func saveChanWork(t *testing.T, repoDir, id string, refs, paths []string) {
	t.Helper()
	if err := store.SaveWork(repoDir, store.Work{
		ID: id, Title: "atividade " + id, State: domain.WorkActive, Actor: "agent",
		Refs: refs, Paths: paths, CreatedAt: chanT0, UpdatedAt: chanT0,
	}); err != nil {
		t.Fatalf("SaveWork(%s): %v", id, err)
	}
}

// TestSessionWork (F-0325): a session's channel identity is its live work —
// directly for a W- binding, via its own auto work for an F- binding, and
// nothing for a dead binding.
func TestSessionWork(t *testing.T) {
	t.Setenv("CERNE_ACTOR", "agent")
	cacheDir, repoDir := t.TempDir(), t.TempDir()
	saveChanWork(t, repoDir, "W-0001", []string{"F-0009"}, []string{"internal/server/**"})
	if err := store.SaveFeature(repoDir, store.Feature{
		ID: "F-0002", Title: "viva", State: domain.StateInDevelopment, CreatedAt: chanT0, UpdatedAt: chanT0,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveWork(repoDir, store.Work{
		ID: "W-0003", Title: "auto", State: domain.WorkActive, Actor: "agent", Auto: true,
		Refs: []string{"F-0002"}, CreatedAt: chanT0, UpdatedAt: chanT0,
	}); err != nil {
		t.Fatal(err)
	}
	bindTestWork(t, repoDir, "W-0004", domain.WorkDone)
	for sess, id := range map[string]string{"s-w": "W-0001", "s-f": "F-0002", "s-dead": "W-0004"} {
		if err := BindSession(cacheDir, sess, id); err != nil {
			t.Fatal(err)
		}
	}

	got, ok := sessionWork(cacheDir, repoDir, "s-w")
	if !ok || got.Work != "W-0001" || len(got.Refs) != 1 || got.Paths[0] != "internal/server/**" {
		t.Fatalf("W- binding: %+v, %v", got, ok)
	}
	if got, ok := sessionWork(cacheDir, repoDir, "s-f"); !ok || got.Work != "W-0003" {
		t.Fatalf("F- binding: %+v, %v; want auto work W-0003", got, ok)
	}
	if got, ok := sessionWork(cacheDir, repoDir, "s-dead"); ok {
		t.Fatalf("dead binding resolved: %+v", got)
	}
	if got, ok := sessionWork(cacheDir, repoDir, "s-none"); ok {
		t.Fatalf("unbound session resolved: %+v", got)
	}
}

// TestChannelDirectedTo (F-0325 crit. 9): a message is addressed to a session
// when it names the session's work, one of the work's refs, or a path inside
// its declared scope — never by a near-miss id, and never to its own sender.
func TestChannelDirectedTo(t *testing.T) {
	me := channelIdentity{Work: "W-0290", Refs: []string{"F-0325"}, Paths: []string{"internal/server/**", "ui/src/App.tsx"}}
	for _, tc := range []struct {
		name string
		nick string
		body string
		want bool
	}{
		{"names my work", "W-0291", "W-0290, segura aí", true},
		{"names my work with @", "W-0291", "@W-0290 pode consumir", true},
		{"names one of my refs", "W-0291", "terminei o contrato de F-0325", true},
		{"names another ref", "W-0291", "terminei F-0326", false},
		{"path inside my scope", "W-0291", "vou mexer em internal/server/rooms.go, segura", true},
		{"path in backticks", "W-0291", "vou mexer em `internal/server/rooms.go`.", true},
		{"exact declared path", "W-0291", "mexendo em ui/src/App.tsx", true},
		{"path outside my scope", "W-0291", "vou mexer em internal/cli/root.go", false},
		{"near-miss id", "W-0291", "W-02901 terminou", false},
		{"own message", "W-0290", "W-0290 aqui, mexendo em internal/server/rooms.go", false},
		{"background", "W-0291", "build verde no main", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := room.Message{Nick: tc.nick, Body: tc.body}
			if got := directedTo(m, me); got != tc.want {
				t.Fatalf("directedTo(%q from %s) = %v, want %v", tc.body, tc.nick, got, tc.want)
			}
		})
	}
}

// TestChannelExpiry (F-0325 crit. 20): an agent message lives 24h or until its
// sender's work closes, whichever comes first; a human message only 24h.
func TestChannelExpiry(t *testing.T) {
	repoDir := t.TempDir()
	saveChanWork(t, repoDir, "W-0001", nil, nil)
	bindTestWork(t, repoDir, "W-0002", domain.WorkDone)
	now := chanT0.Add(48 * time.Hour)
	for _, tc := range []struct {
		name string
		m    room.Message
		live bool
	}{
		{"agent under 24h, work active", room.Message{Nick: "W-0001", Time: now.Add(-23 * time.Hour)}, true},
		{"agent at 24h+1s", room.Message{Nick: "W-0001", Time: now.Add(-24*time.Hour - time.Second)}, false},
		{"agent whose work is done", room.Message{Nick: "W-0002", Time: now.Add(-time.Minute)}, false},
		{"human at 23h, no work", room.Message{Nick: "Henrique", Human: true, Time: now.Add(-23 * time.Hour)}, true},
		{"human at 24h+1s", room.Message{Nick: "Henrique", Human: true, Time: now.Add(-24*time.Hour - time.Second)}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := liveMessages(repoDir, []room.Message{tc.m}, now)
			if (len(got) == 1) != tc.live {
				t.Fatalf("live = %v, want %v", len(got) == 1, tc.live)
			}
		})
	}
}

// chanFixture is a project cache with a channel: me (session "s1") works as
// W-0001 scoped to internal/server/**; W-0002 and W-0003 are other live works.
type chanFixture struct {
	t                 *testing.T
	cacheDir, repoDir string
	ch                room.Channel
	at                time.Time
}

func newChanFixture(t *testing.T) *chanFixture {
	t.Helper()
	t.Setenv("CERNE_ACTOR", "agent")
	f := &chanFixture{t: t, cacheDir: t.TempDir(), repoDir: t.TempDir(), at: chanT0}
	f.ch = room.OpenChannel(filepath.Join(f.cacheDir, "rooms"))
	saveChanWork(t, f.repoDir, "W-0001", []string{"F-0325"}, []string{"internal/server/**"})
	saveChanWork(t, f.repoDir, "W-0002", nil, nil)
	saveChanWork(t, f.repoDir, "W-0003", nil, nil)
	for sess, id := range map[string]string{"s1": "W-0001", "s2": "W-0002", "s3": "W-0003"} {
		if err := BindSession(f.cacheDir, sess, id); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

// fixtureSession is the session each fixture work posts from.
var fixtureSession = map[string]string{"W-0001": "s1", "W-0002": "s2", "W-0003": "s3"}

// post appends a message one second after the previous one; an agent posts
// from its fixture session.
func (f *chanFixture) post(nick, body string, human bool) int {
	f.t.Helper()
	f.at = f.at.Add(time.Second)
	sess := ""
	if !human {
		sess = fixtureSession[nick]
	}
	m, err := f.ch.Append(room.Message{Type: "message", Nick: nick, Session: sess, Body: body, Human: human, Time: f.at})
	if err != nil {
		f.t.Fatalf("Append: %v", err)
	}
	return m.Seq
}

func (f *chanFixture) inject(sess string, consume bool) string {
	f.t.Helper()
	f.at = f.at.Add(time.Second) // one fake clock: posts and injections stay ordered
	out, err := ChannelInject(f.cacheDir, f.repoDir, sess, consume, f.at)
	if err != nil {
		f.t.Fatalf("ChannelInject(%s): %v", sess, err)
	}
	return out
}

func (f *chanFixture) deliveries() []room.Delivery {
	f.t.Helper()
	dels, err := f.ch.Deliveries()
	if err != nil {
		f.t.Fatal(err)
	}
	return dels
}

// TestChannelInjectSilentWhenNothingNew (crit. 5): with no new traffic the
// second injection is zero bytes and records nothing.
func TestChannelInjectSilentWhenNothingNew(t *testing.T) {
	f := newChanFixture(t)
	if out := f.inject("s1", true); out != "" {
		t.Fatalf("empty channel injected %q", out)
	}
	f.post("W-0002", "build verde no main", false)
	if out := f.inject("s1", true); !strings.HasPrefix(out, "cerne channel:\n") {
		t.Fatalf("first injection = %q", out)
	}
	if out := f.inject("s1", true); out != "" {
		t.Fatalf("second injection = %q, want zero bytes", out)
	}
	if n := len(f.deliveries()); n != 1 {
		t.Fatalf("%d delivery lines, want 1", n)
	}
}

// TestChannelInjectWatermark (crit. 4): background delivered to a session does
// not come back to it, still reaches a new session, and the next injection
// carries only what is newer.
func TestChannelInjectWatermark(t *testing.T) {
	f := newChanFixture(t)
	f.post("W-0002", "primeira", false)
	if out := f.inject("s1", true); !strings.Contains(out, "#1 W-0002: primeira") {
		t.Fatalf("s1 injection = %q", out)
	}
	if out := f.inject("s3", true); !strings.Contains(out, "#1 W-0002: primeira") {
		t.Fatalf("new session s3 did not get #1: %q", out)
	}
	f.post("W-0002", "segunda", false)
	out := f.inject("s1", true)
	if !strings.Contains(out, "#2 W-0002: segunda") || strings.Contains(out, "#1") {
		t.Fatalf("s1 second injection = %q, want only #2", out)
	}
	if out := f.inject("s2", true); out != "" {
		t.Fatalf("sender got its own messages: %q", out)
	}
}

// TestChannelInjectDirectedRepeatsUntilAnswered (crit. 9/10): a message
// addressed to the session comes back at every injection until the session
// posts after it — or, separately, acks it.
func TestChannelInjectDirectedRepeatsUntilAnswered(t *testing.T) {
	t.Run("answered by a later message", func(t *testing.T) {
		f := newChanFixture(t)
		f.post("W-0002", "vou mexer em internal/server/rooms.go, segura", false)
		for i := range 3 {
			if out := f.inject("s1", true); !strings.Contains(out, "#1 W-0002 → you: vou mexer") {
				t.Fatalf("injection %d = %q, want the addressed message", i+1, out)
			}
		}
		f.post("W-0001", "ok, seguro", false)
		if out := f.inject("s1", true); out != "" {
			t.Fatalf("after answering: %q, want zero bytes", out)
		}
	})
	t.Run("acked", func(t *testing.T) {
		f := newChanFixture(t)
		f.post("W-0002", "W-0001 pode consumir o contrato", false)
		f.inject("s1", true)
		if err := f.ch.Update(func([]room.Message, []room.Delivery) (*room.Delivery, error) {
			return &room.Delivery{Session: "s1", Work: "W-0001", Ack: []int{1}, At: f.at}, nil
		}); err != nil {
			t.Fatal(err)
		}
		if out := f.inject("s1", true); out != "" {
			t.Fatalf("after ack: %q, want zero bytes", out)
		}
	})
	t.Run("a post made before the session saw it is no answer", func(t *testing.T) {
		f := newChanFixture(t)
		f.post("W-0002", "W-0001 segura o rooms.go", false)
		f.post("W-0001", "build verde", false) // never shown #1 yet
		if out := f.inject("s1", true); !strings.Contains(out, "#1 W-0002 → you") {
			t.Fatalf("addressed message silenced by a post that predates its delivery: %q", out)
		}
		if out := f.inject("s1", true); !strings.Contains(out, "#1 W-0002 → you") {
			t.Fatalf("addressed message did not repeat: %q", out)
		}
		f.post("W-0001", "ok, seguro", false)
		if out := f.inject("s1", true); out != "" {
			t.Fatalf("after a real answer: %q", out)
		}
	})
	t.Run("background is delivered once", func(t *testing.T) {
		f := newChanFixture(t)
		f.post("W-0002", "build verde", false)
		f.inject("s1", true)
		if out := f.inject("s1", true); out != "" {
			t.Fatalf("background repeated: %q", out)
		}
	})
}

// injectedBodies returns the body of every message line, keyed by seq.
func injectedBodies(out string) map[int]string {
	bodies := map[int]string{}
	for _, line := range strings.Split(out, "\n") {
		var seq int
		if _, err := fmt.Sscanf(line, "#%d ", &seq); err != nil {
			continue
		}
		bodies[seq] = line[strings.Index(line, ": ")+2:]
	}
	return bodies
}

// TestChannelInjectTruncatesAt280 (crit. 6): an inline body is cut at 280
// runes and marked with an ellipsis.
func TestChannelInjectTruncatesAt280(t *testing.T) {
	f := newChanFixture(t)
	for range 3 {
		f.post("W-0002", strings.Repeat("a", 400), false)
	}
	bodies := injectedBodies(f.inject("s1", true))
	if len(bodies) != 3 {
		t.Fatalf("%d inline, want 3", len(bodies))
	}
	for seq, b := range bodies {
		if b != strings.Repeat("a", 280)+"…" {
			t.Fatalf("#%d body = %d runes %q…, want 280 + …", seq, utf8.RuneCountInString(b), b[:10])
		}
	}
}

// TestChannelInjectCountLine (crit. 6): over the 5-inline cap the rest is one
// count line pointing at the full read.
func TestChannelInjectCountLine(t *testing.T) {
	f := newChanFixture(t)
	for i := range 9 {
		f.post("W-0002", fmt.Sprintf("ruído %d", i+1), false)
	}
	out := f.inject("s1", true)
	if n := len(injectedBodies(out)); n != 5 {
		t.Fatalf("%d inline, want 5:\n%s", n, out)
	}
	if !strings.Contains(out, "\n+4 more — cerne msg read\n") {
		t.Fatalf("count line missing:\n%s", out)
	}
}

// TestChannelInjectCutsBackgroundFirst (crit. 7): over the cap, human and
// addressed messages stay inline and the cut falls on background, oldest
// first; display goes human, addressed, background.
func TestChannelInjectCutsBackgroundFirst(t *testing.T) {
	f := newChanFixture(t)
	bg := []int{f.post("W-0003", "fundo 1", false), f.post("W-0003", "fundo 2", false)}
	h1 := f.post("Henrique", "humano 1", true)
	d1 := f.post("W-0002", "W-0001 segura", false)
	bg = append(bg, f.post("W-0003", "fundo 3", false))
	d2 := f.post("W-0002", "F-0325 contrato pronto", false)
	h2 := f.post("Henrique", "humano 2", true)
	bg = append(bg, f.post("W-0003", "fundo 4", false))

	out := f.inject("s1", true)
	bodies := injectedBodies(out)
	for _, seq := range []int{h1, h2, d1, d2, bg[3]} {
		if _, ok := bodies[seq]; !ok {
			t.Fatalf("#%d not inline:\n%s", seq, out)
		}
	}
	if len(bodies) != 5 || !strings.Contains(out, "+3 more") {
		t.Fatalf("want 5 inline + 3 more:\n%s", out)
	}
	var order []int
	for _, line := range strings.Split(out, "\n") {
		var seq int
		if _, err := fmt.Sscanf(line, "#%d ", &seq); err == nil {
			order = append(order, seq)
		}
	}
	if want := []int{h1, h2, d1, d2, bg[3]}; !slices.Equal(order, want) {
		t.Fatalf("display order = %v, want %v", order, want)
	}
}

// TestChannelInjectProtocolLineOnlyWithDirected (crit. 17).
func TestChannelInjectProtocolLineOnlyWithDirected(t *testing.T) {
	f := newChanFixture(t)
	f.post("W-0003", "build verde", false)
	if out := f.inject("s1", true); strings.Contains(out, "↳") {
		t.Fatalf("protocol line with background only:\n%s", out)
	}
	seq := f.post("W-0002", "W-0001 segura", false)
	out := f.inject("s1", true)
	want := fmt.Sprintf("↳ addressed to you: answer with cerne msg send \"<text>\" (any later message of yours counts) or cerne msg ack %d\n", seq)
	if strings.Count(out, "↳") != 1 || !strings.HasSuffix(out, want) {
		t.Fatalf("protocol line missing or repeated:\n%s", out)
	}
	f.post("W-0001", "ok", false)
	if out := f.inject("s1", true); out != "" {
		t.Fatalf("empty injection = %q", out)
	}
	hs := f.post("Henrique", "W-0001, para tudo", true)
	if out := f.inject("s1", true); !strings.Contains(out, fmt.Sprintf("#%d Henrique (human) → you: ", hs)) || !strings.Contains(out, "↳") {
		t.Fatalf("addressed human message without protocol line:\n%s", out)
	}
}

// TestChannelInjectRecordsDeliveries (crit. 11): the injection itself writes
// who was shown what, when, and how far the session has seen.
func TestChannelInjectRecordsDeliveries(t *testing.T) {
	f := newChanFixture(t)
	for i := range 7 {
		f.post("W-0002", fmt.Sprintf("m%d", i+1), false)
	}
	f.post("W-0001", "minha", false)
	f.inject("s1", true)
	dels := f.deliveries()
	if len(dels) != 1 {
		t.Fatalf("%d delivery lines, want 1", len(dels))
	}
	d := dels[0]
	if d.Session != "s1" || d.Work != "W-0001" || !slices.Equal(d.Seqs, []int{3, 4, 5, 6, 7}) ||
		d.Through != 8 || !d.At.Equal(f.at) {
		t.Fatalf("delivery = %+v", d)
	}
}

// TestChannelInjectPeekKeepsWatermark: a subagent's peek records what it was
// shown but leaves the parent session's watermark where it was.
func TestChannelInjectPeekKeepsWatermark(t *testing.T) {
	f := newChanFixture(t)
	f.post("W-0002", "build verde", false)
	if out := f.inject("s1", false); !strings.Contains(out, "#1 W-0002: build verde") {
		t.Fatalf("peek = %q", out)
	}
	if d := f.deliveries(); len(d) != 1 || d[0].Through != 0 || !slices.Equal(d[0].Seqs, []int{1}) {
		t.Fatalf("peek delivery = %+v", d)
	}
	if out := f.inject("s1", true); !strings.Contains(out, "#1 W-0002: build verde") {
		t.Fatalf("parent lost the background after a peek: %q", out)
	}
}

// ptBody fills prefix to n runes of multibyte pt-BR text.
func ptBody(prefix string, n int) string {
	b := []rune(prefix)
	filler := []rune(" ação não — coordenação")
	for i := 0; len(b) < n; i++ {
		b = append(b, filler[i%len(filler)])
	}
	return string(b[:n])
}

// TestChannelInjectWorstCaseWithin400Tokens (crit. 8): a saturated channel —
// humans over the cap, addressed and background traffic, every body at 400
// multibyte runes — still injects within 400 tokens (bytes/4), with every
// human message inline, the exact overflow count and the protocol line.
func TestChannelInjectWorstCaseWithin400Tokens(t *testing.T) {
	t.Run("humans over the cap", func(t *testing.T) {
		f := newChanFixture(t)
		var humans []int
		for i := range 20 {
			f.post("W-0003", ptBody(fmt.Sprintf("fundo %d", i), 400), false)
		}
		for i := range 6 {
			f.post("W-0002", ptBody(fmt.Sprintf("W-0001 dirigida %d", i), 400), false)
		}
		for i := range 8 {
			humans = append(humans, f.post("Henrique", ptBody(fmt.Sprintf("humano %d", i), 400), true))
		}
		out := f.inject("s1", true)
		if tokens := (len(out) + 3) / 4; tokens > 400 {
			t.Fatalf("injection = %d tokens (%d bytes), want <= 400:\n%s", tokens, len(out), out)
		}
		bodies := injectedBodies(out)
		for _, seq := range humans {
			if _, ok := bodies[seq]; !ok {
				t.Fatalf("human #%d not inline:\n%s", seq, out)
			}
		}
		if !strings.Contains(out, "\n+26 more — cerne msg read\n") || !strings.Contains(out, "↳ addressed to you") {
			t.Fatalf("count or protocol line missing:\n%s", out)
		}
		t.Logf("worst case: %d bytes, %d tokens", len(out), (len(out)+3)/4)
	})
	t.Run("no humans", func(t *testing.T) {
		f := newChanFixture(t)
		var directed []int
		for i := range 20 {
			f.post("W-0003", ptBody(fmt.Sprintf("fundo %d", i), 400), false)
		}
		for i := range 10 {
			directed = append(directed, f.post("W-0002", ptBody(fmt.Sprintf("W-0001 dirigida %d", i), 400), false))
		}
		out := f.inject("s1", true)
		if tokens := (len(out) + 3) / 4; tokens > 400 {
			t.Fatalf("injection = %d tokens, want <= 400:\n%s", tokens, out)
		}
		bodies := injectedBodies(out)
		if len(bodies) != 5 {
			t.Fatalf("%d inline, want 5:\n%s", len(bodies), out)
		}
		for _, seq := range directed[5:] {
			if _, ok := bodies[seq]; !ok {
				t.Fatalf("newest addressed #%d not inline:\n%s", seq, out)
			}
		}
		if !strings.Contains(out, "\n+25 more — cerne msg read\n") {
			t.Fatalf("count line missing:\n%s", out)
		}
	})
	t.Run("humans past the body floor", func(t *testing.T) {
		f := newChanFixture(t)
		var humans []int
		for i := range 30 {
			humans = append(humans, f.post("Henrique", ptBody(fmt.Sprintf("humano %d", i), 400), true))
		}
		out := f.inject("s1", true)
		if tokens := (len(out) + 3) / 4; tokens > 400 {
			t.Fatalf("injection = %d tokens, want <= 400:\n%s", tokens, out)
		}
		bodies := injectedBodies(out)
		if _, ok := bodies[humans[29]]; !ok || len(bodies) == 30 {
			t.Fatalf("want the newest humans inline and the oldest counted:\n%s", out)
		}
		if _, ok := bodies[humans[0]]; ok {
			t.Fatalf("oldest human kept over a newer one:\n%s", out)
		}
		if !strings.Contains(out, fmt.Sprintf("\n+%d more — cerne msg read\n", 30-len(bodies))) {
			t.Fatalf("count line missing:\n%s", out)
		}
	})
}

func (f *chanFixture) send(sess, body string) (room.Message, []string) {
	f.t.Helper()
	f.at = f.at.Add(time.Second)
	m, to, err := ChannelSend(f.cacheDir, f.repoDir, sess, body, f.at)
	if err != nil {
		f.t.Fatalf("ChannelSend(%s): %v", sess, err)
	}
	return m, to
}

func (f *chanFixture) read(sess string) ([]ChannelMessage, []ChannelMember) {
	f.t.Helper()
	f.at = f.at.Add(time.Second)
	msgs, roster, err := ChannelRead(f.cacheDir, f.repoDir, sess, f.at)
	if err != nil {
		f.t.Fatalf("ChannelRead: %v", err)
	}
	return msgs, roster
}

// TestChannelSendCreatesRoomOnDemand (crit. 1): the first send materializes
// the channel — there is no open.
func TestChannelSendCreatesRoomOnDemand(t *testing.T) {
	f := newChanFixture(t)
	m, _ := f.send("s1", "segura internal/server")
	if m.Seq != 1 || m.Nick != "W-0001" || m.Human {
		t.Fatalf("message = %+v", m)
	}
	if _, err := os.Stat(filepath.Join(f.cacheDir, "rooms", "channel", "messages.jsonl")); err != nil {
		t.Fatalf("channel not created on demand: %v", err)
	}
}

// TestChannelSendRequiresRegistration: a session without a live registration
// has no identity to speak as, and writes nothing.
func TestChannelSendRequiresRegistration(t *testing.T) {
	f := newChanFixture(t)
	_, _, err := ChannelSend(f.cacheDir, f.repoDir, "s-unbound", "oi", f.at)
	if !errors.Is(err, ErrNoChannelIdentity) {
		t.Fatalf("err = %v, want ErrNoChannelIdentity", err)
	}
	if _, err := os.Stat(filepath.Join(f.cacheDir, "rooms", "channel")); !os.IsNotExist(err) {
		t.Fatalf("refused send created the channel: %v", err)
	}
}

// TestChannelSendReportsLiveRecipients (crit. 12): the sender learns which
// other live sessions will receive it — none is an explicit empty answer.
func TestChannelSendReportsLiveRecipients(t *testing.T) {
	f := newChanFixture(t)
	bindTestWork(t, f.repoDir, "W-0009", domain.WorkDone)
	if err := BindSession(f.cacheDir, "s9", "W-0009"); err != nil {
		t.Fatal(err)
	}
	if _, to := f.send("s1", "oi"); !slices.Equal(to, []string{"W-0002", "W-0003"}) {
		t.Fatalf("recipients = %v, want [W-0002 W-0003]", to)
	}

	alone := newChanFixture(t)
	for _, s := range []string{"s2", "s3"} {
		if err := os.Remove(filepath.Join(alone.cacheDir, "sessions", s)); err != nil {
			t.Fatal(err)
		}
	}
	if _, to := alone.send("s1", "oi"); len(to) != 0 {
		t.Fatalf("recipients = %v, want none", to)
	}
}

func stateOf(t *testing.T, msgs []ChannelMessage, seq int) ChannelMessage {
	t.Helper()
	for _, m := range msgs {
		if m.Seq == seq {
			return m
		}
	}
	t.Fatalf("#%d not in read", seq)
	return ChannelMessage{}
}

// TestChannelStatesDerived (crit. 13/14): an addressed message moves
// not_delivered -> delivered -> answered from the injection and the target's
// own later post alone — no receipt typed by anyone, and an anonymous read
// changes nothing.
func TestChannelStatesDerived(t *testing.T) {
	f := newChanFixture(t)
	m, _ := f.send("s2", "W-0001 segura o rooms.go")
	want := func(state string) {
		t.Helper()
		msgs, _ := f.read("")
		got := stateOf(t, msgs, m.Seq)
		if got.State != state || !got.Directed || len(got.Targets) != 1 ||
			got.Targets[0] != (ChannelTarget{Work: "W-0001", State: state}) {
			t.Fatalf("#%d = %+v, want directed to W-0001 in %s", m.Seq, got, state)
		}
	}
	want(ChannelNotDelivered)
	want(ChannelNotDelivered) // reading without a session records nothing
	f.inject("s1", true)
	want(ChannelDelivered)
	f.send("s1", "ok, seguro")
	want(ChannelAnswered)

	bg, _ := f.send("s3", "build verde")
	msgs, _ := f.read("")
	if got := stateOf(t, msgs, bg.Seq); got.State != ChannelNotDelivered || got.Directed || len(got.Targets) != 0 {
		t.Fatalf("undelivered background = %+v", got)
	}
	f.inject("s1", true)
	msgs, _ = f.read("")
	if got := stateOf(t, msgs, bg.Seq); got.State != ChannelDelivered || len(got.Targets) != 1 || got.Targets[0].Work != "W-0001" {
		t.Fatalf("delivered background = %+v", got)
	}
}

// TestChannelAckSilencesDirected (crit. 10): the explicit ack stops the
// repeats and reads as answered.
func TestChannelAckSilencesDirected(t *testing.T) {
	f := newChanFixture(t)
	m, _ := f.send("s2", "W-0001 pode consumir")
	if out := f.inject("s1", true); !strings.Contains(out, "→ you") {
		t.Fatalf("injection = %q", out)
	}
	if _, _, err := ChannelAck(f.cacheDir, f.repoDir, "s1", []int{m.Seq}, f.at); err != nil {
		t.Fatalf("ChannelAck: %v", err)
	}
	if out := f.inject("s1", true); out != "" {
		t.Fatalf("after ack: %q", out)
	}
	msgs, _ := f.read("")
	if got := stateOf(t, msgs, m.Seq); got.State != ChannelAnswered {
		t.Fatalf("acked = %+v", got)
	}
	if _, _, err := ChannelAck(f.cacheDir, f.repoDir, "s-unbound", []int{m.Seq}, f.at); !errors.Is(err, ErrNoChannelIdentity) {
		t.Fatalf("unbound ack: %v", err)
	}
}

// TestChannelRosterFromActiveWorks (crit. 3): the roster is the active works,
// live when a local session is bound to them — no join.
func TestChannelRosterFromActiveWorks(t *testing.T) {
	f := newChanFixture(t)
	bindTestWork(t, f.repoDir, "W-0004", domain.WorkDone)
	if err := os.Remove(filepath.Join(f.cacheDir, "sessions", "s3")); err != nil {
		t.Fatal(err)
	}
	_, roster := f.read("")
	got := map[string]bool{}
	for _, m := range roster {
		got[m.Work] = m.Live
	}
	want := map[string]bool{"W-0001": true, "W-0002": true, "W-0003": false}
	if !maps.Equal(got, want) {
		t.Fatalf("roster = %+v, want %v", roster, want)
	}
}

// TestChannelReadFullLiveHistory (crit. 18): an explicit read is the whole
// live history, past the inline cap, minus what expired.
func TestChannelReadFullLiveHistory(t *testing.T) {
	f := newChanFixture(t)
	if _, err := f.ch.Append(room.Message{Type: "message", Nick: "W-0002", Body: "velha", Time: chanT0.Add(-25 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	bindTestWork(t, f.repoDir, "W-0005", domain.WorkDone)
	if _, err := f.ch.Append(room.Message{Type: "message", Nick: "W-0005", Body: "de work fechado", Time: chanT0}); err != nil {
		t.Fatal(err)
	}
	for i := range 12 {
		f.post("W-0002", fmt.Sprintf("viva %d", i), false)
	}
	msgs, _ := f.read("s1")
	if len(msgs) != 12 {
		t.Fatalf("read %d messages, want 12 live", len(msgs))
	}
	// The session saw everything it read: no injection repeats it.
	if out := f.inject("s1", true); out != "" {
		t.Fatalf("injection after a full read = %q", out)
	}
}

// postAs appends a message as session sess working on nick, one second later.
func (f *chanFixture) postAs(sess, nick, body string) int {
	f.t.Helper()
	f.at = f.at.Add(time.Second)
	m, err := f.ch.Append(room.Message{Type: "message", Nick: nick, Session: sess, Body: body, Time: f.at})
	if err != nil {
		f.t.Fatalf("Append: %v", err)
	}
	return m.Seq
}

// TestChannelPeekIsNoDelivery (critic C2, PEEK-PROBE): a subagent's peek is
// recorded but is no delivery — the parent's next post, about anything, must
// not answer an addressed message only the subagent saw; neither may a post
// from another session of the same work.
func TestChannelPeekIsNoDelivery(t *testing.T) {
	f := newChanFixture(t)
	if err := BindSession(f.cacheDir, "s2b", "W-0002"); err != nil {
		t.Fatal(err)
	}
	seq := f.postAs("s1", "W-0001", "W-0002 PEEK-PROBE: do not merge before my rebase")
	if out := f.inject("s2", false); !strings.Contains(out, "PEEK-PROBE") {
		t.Fatalf("peek = %q", out)
	}
	if d := f.deliveries(); len(d) != 1 || !d[0].Peek || d[0].Through != 0 {
		t.Fatalf("peek delivery = %+v, want marked as a peek", d)
	}
	f.postAs("s2", "W-0002", "FYI: tests are green on my side")
	msgs, _ := f.read("")
	if got := stateOf(t, msgs, seq); got.State != ChannelNotDelivered {
		t.Fatalf("after a peek and an unrelated post: %+v, want not_delivered", got)
	}
	if out := f.inject("s2", true); !strings.Contains(out, "#1 W-0001 → you: W-0002 PEEK-PROBE") {
		t.Fatalf("parent never got the message the subagent peeked: %q", out)
	}
	f.postAs("s2b", "W-0002", "outra sessão do mesmo work")
	if out := f.inject("s2", true); !strings.Contains(out, "PEEK-PROBE") {
		t.Fatalf("a post from another session of the work answered it: %q", out)
	}
	f.postAs("s2", "W-0002", "ok, espero o rebase")
	if out := f.inject("s2", true); strings.Contains(out, "PEEK-PROBE") {
		t.Fatalf("still repeating after the real answer: %q", out)
	}
	msgs, _ = f.read("")
	if got := stateOf(t, msgs, seq); got.State != ChannelAnswered {
		t.Fatalf("after the real answer: %+v", got)
	}
	for _, m := range msgs {
		if m.Session != "" {
			t.Fatalf("read leaks the sender's session id (it speaks as the work): %+v", m)
		}
	}
}

// TestChannelStopReminderIsADelivery (critic C3): the Stop reminder shows the
// message, so it records a delivery — a reply after it, as the reminder
// instructs, answers the message and nothing is injected again.
func TestChannelStopReminderIsADelivery(t *testing.T) {
	f := newChanFixture(t)
	seq := f.post("W-0002", "W-0001 please hold off internal/server/rooms.go until I merge", false)
	f.at = f.at.Add(time.Second)
	text, mark := ChannelStopReminder(f.cacheDir, f.repoDir, "s1", f.at)
	if !strings.Contains(text, fmt.Sprintf("#%d", seq)) {
		t.Fatalf("reminder = %q", text)
	}
	mark()
	msgs, _ := f.read("")
	if got := stateOf(t, msgs, seq); got.State != ChannelDelivered {
		t.Fatalf("after the reminder: %+v, want delivered", got)
	}
	f.post("W-0001", "ok, holding", false)
	msgs, _ = f.read("")
	if got := stateOf(t, msgs, seq); got.State != ChannelAnswered {
		t.Fatalf("reply after the reminder: %+v, want answered", got)
	}
	if out := f.inject("s1", true); out != "" {
		t.Fatalf("answered message injected again: %q", out)
	}
}

// TestChannelStopReminderWithinBudget (critic C4): the Stop reminder goes
// through the injection renderer — 5 inline, 280 runes, the 400-token
// ceiling — however many addressed messages are pending.
func TestChannelStopReminderWithinBudget(t *testing.T) {
	f := newChanFixture(t)
	for i := range 30 {
		f.post("W-0002", ptBody(fmt.Sprintf("W-0001 ação não prevista número %d", i), 400), false)
	}
	f.at = f.at.Add(time.Second)
	text, mark := ChannelStopReminder(f.cacheDir, f.repoDir, "s1", f.at)
	if tokens := (len(text) + 3) / 4; tokens > 400 {
		t.Fatalf("Stop reminder = %d tokens (%d bytes), want <= 400:\n%s", tokens, len(text), text)
	}
	if n := len(injectedBodies(text)); n != 5 || !strings.Contains(text, "\n+25 more — cerne msg read\n") {
		t.Fatalf("want 5 inline + 25 counted:\n%s", text)
	}
	mark()
	f.at = f.at.Add(time.Second)
	if again, _ := ChannelStopReminder(f.cacheDir, f.repoDir, "s1", f.at); again != "" {
		t.Fatalf("reminded again next turn: %q", again)
	}
	t.Logf("30 addressed: Stop reminder %d bytes, %d tokens", len(text), (len(text)+3)/4)
}

// TestChannelHumansNeverSilentlyConsumed (critic C6): a human message that
// does not fit inline stays pending for that session — the next injections
// show it — so a session with 23 human messages pending eventually sees every
// one inline and none ends not_delivered.
func TestChannelHumansNeverSilentlyConsumed(t *testing.T) {
	f := newChanFixture(t)
	var humans []int
	for i := range 23 {
		humans = append(humans, f.post("Henrique", ptBody(fmt.Sprintf("humano %d", i), 400), true))
	}
	seen := map[int]bool{}
	for round := 0; ; round++ {
		out := f.inject("s2", true)
		if out == "" {
			break
		}
		if round == 5 {
			t.Fatalf("still injecting after 5 rounds:\n%s", out)
		}
		if tokens := (len(out) + 3) / 4; tokens > 400 {
			t.Fatalf("round %d = %d tokens", round, tokens)
		}
		for seq := range injectedBodies(out) {
			seen[seq] = true
		}
	}
	msgs, _ := f.read("")
	for _, seq := range humans {
		if !seen[seq] {
			t.Fatalf("human #%d never shown inline (seen %v)", seq, seen)
		}
		if got := stateOf(t, msgs, seq); got.State == ChannelNotDelivered {
			t.Fatalf("human #%d left not_delivered: %+v", seq, got)
		}
	}
}

// TestChannelLongHumanNameKeepsTheCap (critic C6): channel.human_name may be
// 64 four-byte runes; in the block it is cut to 24 runes, so the prefix can't
// starve the inline set below the cap of 5.
func TestChannelLongHumanNameKeepsTheCap(t *testing.T) {
	f := newChanFixture(t)
	name := strings.Repeat("𝕳", 64)
	for i := range 8 {
		f.post(name, ptBody(fmt.Sprintf("humano %d", i), 400), true)
	}
	out := f.inject("s2", true)
	if n := len(injectedBodies(out)); n < 5 {
		t.Fatalf("%d inline with a long human name, want >= 5:\n%s", n, out)
	}
	if (len(out)+3)/4 > 400 || !strings.Contains(out, strings.Repeat("𝕳", 24)+"… (human): ") {
		t.Fatalf("name not cut to 24 runes, or over budget:\n%s", out)
	}
}

// TestChannelProtocolLineTruthful (critic C15): when the addressed message
// fell into the count line, the protocol line points at the full read, not
// at an `ack <seq>` the reader cannot see.
func TestChannelProtocolLineTruthful(t *testing.T) {
	f := newChanFixture(t)
	f.post("W-0002", "W-0001 segura o rooms.go", false)
	for i := range 5 {
		f.post("Henrique", fmt.Sprintf("humano %d", i), true)
	}
	out := f.inject("s1", true)
	if strings.Contains(out, "→ you") || !strings.Contains(out, "+1 more") {
		t.Fatalf("want the addressed message counted, not inline:\n%s", out)
	}
	if !strings.HasSuffix(out, "↳ addressed to you, not shown above: read it with cerne msg read\n") || strings.Contains(out, "ack") {
		t.Fatalf("protocol line not pointing at the read:\n%s", out)
	}
}

// TestChannelSameWorkSessionsSeeEachOther (critic C12): a message is kept
// from its own session only, so two sessions on one work coordinate over the
// channel (as background: naming your own work is no address).
func TestChannelSameWorkSessionsSeeEachOther(t *testing.T) {
	f := newChanFixture(t)
	if err := BindSession(f.cacheDir, "s2b", "W-0002"); err != nil {
		t.Fatal(err)
	}
	seq := f.postAs("s2", "W-0002", "SAME-WORK-PROBE: I take internal/app, you take ui")
	if out := f.inject("s2b", true); !strings.Contains(out, fmt.Sprintf("#%d W-0002: SAME-WORK-PROBE", seq)) {
		t.Fatalf("the other session of the work did not get it: %q", out)
	}
	if out := f.inject("s2", true); out != "" {
		t.Fatalf("the sender's own session got its message back: %q", out)
	}
}

// TestChannelSendReportsUniqueWorks (critic C10/C12): the send report names
// each receiving work once, only works still open, and includes the sender's
// own work when another of its sessions is live.
func TestChannelSendReportsUniqueWorks(t *testing.T) {
	f := newChanFixture(t)
	bindTestWork(t, f.repoDir, "W-0009", domain.WorkDone)
	for sess, id := range map[string]string{"s2b": "W-0002", "s2c": "W-0002", "s9": "W-0009", "s1b": "W-0001"} {
		if err := BindSession(f.cacheDir, sess, id); err != nil {
			t.Fatal(err)
		}
	}
	if _, to := f.send("s2", "oi"); !slices.Equal(to, []string{"W-0001", "W-0002", "W-0003"}) {
		t.Fatalf("recipients = %v, want each open work once, the sender's sibling session included", to)
	}
}

// TestChannelAckValidates (critic C9): ack refuses a seq that is not a live
// message (unknown, future or expired) and writes nothing, so a typo cannot
// silence a message before it arrives; acking a message not addressed to you,
// or already answered, is a no-op with a note.
func TestChannelAckValidates(t *testing.T) {
	f := newChanFixture(t)
	if _, err := f.ch.Append(room.Message{Type: "message", Nick: "W-0002", Session: "s2", Body: "W-0001 velha", Time: chanT0.Add(-25 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	addressed := f.post("W-0002", "W-0001 segura", false)
	background := f.post("W-0002", "build verde", false)
	for _, seqs := range [][]int{{9999}, {1}, {addressed, 9999}} {
		if _, _, err := ChannelAck(f.cacheDir, f.repoDir, "s1", seqs, f.at); !errors.Is(err, ErrNoSuchMessage) {
			t.Fatalf("ack %v: err = %v, want ErrNoSuchMessage", seqs, err)
		}
	}
	if d := f.deliveries(); len(d) != 0 {
		t.Fatalf("a refused ack wrote %+v", d)
	}
	acked, notes, err := ChannelAck(f.cacheDir, f.repoDir, "s1", []int{background, addressed}, f.at)
	if err != nil || !slices.Equal(acked, []int{addressed}) || len(notes) != 1 || !strings.Contains(notes[0], "not addressed to W-0001") {
		t.Fatalf("ack = %v %v %v", acked, notes, err)
	}
	acked, notes, err = ChannelAck(f.cacheDir, f.repoDir, "s1", []int{addressed}, f.at)
	if err != nil || len(acked) != 0 || len(notes) != 1 || !strings.Contains(notes[0], "already answered") {
		t.Fatalf("second ack = %v %v %v", acked, notes, err)
	}
	if n := len(f.deliveries()); n != 1 {
		t.Fatalf("%d delivery lines, want only the one real ack", n)
	}
}
