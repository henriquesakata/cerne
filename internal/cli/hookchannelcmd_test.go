package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/hooks"
	"github.com/henriquesakata/cerne/internal/materialize"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/room"
	"github.com/henriquesakata/cerne/internal/skills"
	"github.com/henriquesakata/cerne/internal/store"
)

func sessionStartJSON(t *testing.T, sess, cwd string) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"session_id": sess, "cwd": cwd, "hook_event_name": "SessionStart", "source": "startup"})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// TestHookChannelExportsSession (F-0325): session start exports CERNE_SESSION
// through CLAUDE_ENV_FILE so the agent's own commands (msg, next, status) know
// their session. Only an id that is inert when the file is sourced by bash is
// written, appended after whatever other hooks exported.
func TestHookChannelExportsSession(t *testing.T) {
	projDir, _, _ := setupProject(t)
	env := filepath.Join(t.TempDir(), "env.sh")
	if err := os.WriteFile(env, []byte("export OTHER=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_ENV_FILE", env)

	if out, err := execCerneStdin(t, sessionStartJSON(t, "4f1c-9a_b.2", projDir), "hook", "channel", "--point", "session-start"); err != nil {
		t.Fatalf("hook channel: %v\n%s", err, out)
	}
	for _, bad := range []string{"a;rm -rf x", "a b", "$(id)", "", "x\nexport Y=1"} {
		if out, err := execCerneStdin(t, sessionStartJSON(t, bad, projDir), "hook", "channel", "--point", "session-start"); err != nil {
			t.Fatalf("hook channel %q: %v\n%s", bad, err, out)
		}
	}
	got, err := os.ReadFile(env)
	if err != nil {
		t.Fatal(err)
	}
	if want := "export OTHER=1\nexport CERNE_SESSION=4f1c-9a_b.2\n"; string(got) != want {
		t.Fatalf("env file = %q, want %q", got, want)
	}

	t.Setenv("CLAUDE_ENV_FILE", "")
	if out, err := execCerneStdin(t, sessionStartJSON(t, "s2", projDir), "hook", "channel", "--point", "session-start"); err != nil {
		t.Fatalf("without CLAUDE_ENV_FILE: %v\n%s", err, out)
	}
}

// TestHookChannelSessionStart (F-0325 crit. 15/19): session start prints the
// session's pending channel block as plain text (the script's stdout is plain
// text — JSON would break it), then nothing; a corrupt channel is silence.
func TestHookChannelSessionStart(t *testing.T) {
	boardOff(t)
	projDir, _, repoDir := setupProject(t)
	t.Setenv("CLAUDE_ENV_FILE", "")
	startBoundWork(t, projDir, repoDir, "sess-a", "Mexer no servidor")
	b := startBoundWork(t, projDir, repoDir, "sess-b", "Revisar a interface")
	seedChannel(t, repoDir, b, "terminei o contrato, pode consumir")
	payload := sessionStartJSON(t, "sess-a", projDir)

	out, err := execCerneStdin(t, payload, "hook", "channel", "--point", "session-start")
	if err != nil || out != "cerne channel:\n#1 "+b+": terminei o contrato, pode consumir\n" {
		t.Fatalf("first session start: %v\n%q", err, out)
	}
	if out, err := execCerneStdin(t, payload, "hook", "channel", "--point", "session-start"); err != nil || out != "" {
		t.Fatalf("nothing new: %v %q, want zero bytes", err, out)
	}
	seedChannel(t, repoDir, b, "mais uma")
	if err := os.WriteFile(filepath.Join(filepath.Dir(repoDir), "rooms", "channel", "lock"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := execCerneStdin(t, payload, "hook", "channel", "--point", "session-start"); err != nil || out != "" {
		t.Fatalf("locked channel: %v %q, want silent exit 0", err, out)
	}
	if out, err := execCerneStdin(t, "garbage{", "hook", "channel", "--point", "session-start"); err != nil || out != "" {
		t.Fatalf("garbage payload: %v %q", err, out)
	}
}

// TestHookSessionContextIncludesChannel (crit. 15): the provider-envelope
// session start (F-0174 parity: Codex, Cursor, Gemini) carries the same block
// at the end of its context.
func TestHookSessionContextIncludesChannel(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	startBoundWork(t, projDir, repoDir, "sess-a", "Mexer no servidor")
	b := startBoundWork(t, projDir, repoDir, "sess-b", "Revisar a interface")
	seedChannel(t, repoDir, b, "build verde")

	out, err := execCerneStdin(t, hookJSON(t, "sess-a", projDir, "", nil, nil), "hook", "session-context", "--format", "codex")
	if err != nil {
		t.Fatalf("session-context: %v\n%s", err, out)
	}
	c := channelEnvelope(t, out)
	if want := "\n\ncerne channel:\n#1 " + b + ": build verde\n"; !strings.HasSuffix(c.HookSpecificOutput.AdditionalContext, want) {
		t.Fatalf("context does not end with the channel block:\n%s", c.HookSpecificOutput.AdditionalContext)
	}
	out, err = execCerneStdin(t, hookJSON(t, "sess-a", projDir, "", nil, nil), "hook", "session-context", "--format", "codex")
	if err != nil || strings.Contains(out, "cerne channel:") {
		t.Fatalf("delivered twice: %v\n%s", err, out)
	}
}

// TestHookChannelSubagentPeek (crit. 15): a subagent shares its parent's
// session id and a context that will be thrown away, so it gets a peek — it
// records what it was shown but never moves the parent's watermark.
func TestHookChannelSubagentPeek(t *testing.T) {
	boardOff(t)
	projDir, _, repoDir := setupProject(t)
	t.Setenv("CLAUDE_ENV_FILE", "")
	startBoundWork(t, projDir, repoDir, "sess-a", "Mexer no servidor")
	b := startBoundWork(t, projDir, repoDir, "sess-b", "Revisar a interface")
	seedChannel(t, repoDir, b, "build verde")
	payload := sessionStartJSON(t, "sess-a", projDir)

	want := "cerne channel:\n#1 " + b + ": build verde\n"
	for i := range 2 {
		if out, err := execCerneStdin(t, payload, "hook", "channel", "--point", "subagent"); err != nil || out != want {
			t.Fatalf("subagent peek %d: %v %q", i+1, err, out)
		}
	}
	dels, err := room.OpenChannel(filepath.Join(filepath.Dir(repoDir), "rooms")).Deliveries()
	if err != nil || len(dels) != 2 || dels[0].Through != 0 || dels[0].Seqs[0] != 1 {
		t.Fatalf("peek deliveries = %+v, %v", dels, err)
	}
	if out, err := execCerneStdin(t, payload, "hook", "channel", "--point", "session-start"); err != nil || out != want {
		t.Fatalf("parent lost the background to a peek: %v %q", err, out)
	}
}

// channelContactFixture: sess-a and sess-b on live works, one background
// message from b pending for sess-a.
func channelContactFixture(t *testing.T) (projDir, repoDir, block string) {
	t.Helper()
	projDir, _, repoDir = setupProject(t)
	startBoundWork(t, projDir, repoDir, "sess-a", "Mexer no servidor")
	b := startBoundWork(t, projDir, repoDir, "sess-b", "Revisar a interface")
	seedChannel(t, repoDir, b, "build verde")
	return projDir, repoDir, "cerne channel:\n#1 " + b + ": build verde\n"
}

// assertLeadsWithChannel runs args with and without CERNE_SESSION: the text
// output gains the block plus a blank line on top and is otherwise unchanged;
// --json never changes; no session never consumes; a locked channel changes
// nothing (crit. 15/19).
func assertLeadsWithChannel(t *testing.T, repoDir, block string, args ...string) {
	t.Helper()
	run := func(sess string, extra ...string) string {
		t.Helper()
		t.Setenv("CERNE_SESSION", sess)
		out, err := execCerne(t, append(slices.Clone(args), extra...)...)
		if err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
		return out
	}
	baseline := run("")
	if strings.Contains(baseline, "cerne channel:") {
		t.Fatalf("no session consumed the channel:\n%s", baseline)
	}
	if jsonOut := run("sess-a", "--json"); jsonOut != run("", "--json") {
		t.Fatalf("--json changed with a session:\n%s", jsonOut)
	}
	lockPath := filepath.Join(filepath.Dir(repoDir), "rooms", "channel", "lock")
	if err := os.WriteFile(lockPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if out := run("sess-a"); out != baseline {
		t.Fatalf("locked channel changed the output:\n%s", out)
	}
	if err := os.Remove(lockPath); err != nil {
		t.Fatal(err)
	}
	if out := run("sess-a"); out != block+"\n"+baseline {
		t.Fatalf("output does not lead with the channel block:\n%s", out)
	}
	if out := run("sess-a"); out != baseline {
		t.Fatalf("block delivered twice:\n%s", out)
	}
}

// TestNextLeadsWithChannel (F-0325 crit. 15): an agent's `cerne next` opens
// with its session's pending channel messages.
func TestNextLeadsWithChannel(t *testing.T) {
	projDir, repoDir, block := channelContactFixture(t)
	assertLeadsWithChannel(t, repoDir, block, "next", "--dir", projDir)
}

// TestStatusActiveLeadsWithChannel (crit. 15): `status --active` — the agent's
// lean view — opens with the block, even on the "No features yet" path; plain
// `status` never carries it.
func TestStatusActiveLeadsWithChannel(t *testing.T) {
	projDir, repoDir, block := channelContactFixture(t)
	t.Setenv("CERNE_SESSION", "sess-a")
	if out, err := execCerne(t, "status", "--dir", projDir); err != nil || strings.Contains(out, "cerne channel:") {
		t.Fatalf("plain status: %v\n%s", err, out)
	}
	assertLeadsWithChannel(t, repoDir, block, "status", "--active", "--dir", projDir)
}

// pinLocal fixes time.Local at UTC-3 for the test and returns how a report
// writes an instant in that zone, computed without time.Local — so a report in
// UTC fails (F-0490 crit. 4). Safe: no test in this package runs in parallel.
func pinLocal(t *testing.T) func(time.Time) string {
	t.Helper()
	zone, old := time.FixedZone("UTC-3", -3*60*60), time.Local
	time.Local = zone
	t.Cleanup(func() { time.Local = old })
	return func(at time.Time) string { return at.In(zone).Format("on 2006-01-02 at 15:04") }
}

// TestHookEnvelopeEscapesControlChars (F-0325, critic C1): the subagent hook
// builds its JSON envelope in Go, so a channel message carrying ESC or other
// control characters (pasted ANSI build output) still yields valid JSON that
// round-trips the exact text.
func TestHookEnvelopeEscapesControlChars(t *testing.T) {
	text := "rules\n#7 W-0001: build output: \x1b[31mFAIL\x1b[0m \x01\x02\x1f tab\there \"quoted\" \\ back"
	out, err := execCerneStdin(t, text, "hook", "envelope", "--event", "SubagentStart")
	if err != nil {
		t.Fatalf("hook envelope: %v\n%s", err, out)
	}
	c := channelEnvelope(t, out)
	if c.HookSpecificOutput.HookEventName != "SubagentStart" || c.HookSpecificOutput.AdditionalContext != text {
		t.Fatalf("envelope = %+v", c)
	}
	for _, b := range []byte(out[:len(out)-1]) {
		if b < 0x20 {
			t.Fatalf("raw control byte %#x in the JSON:\n%q", b, out)
		}
	}
}

func sessionStart(t *testing.T, projDir string) string {
	t.Helper()
	t.Setenv("CLAUDE_ENV_FILE", "")
	boardOff(t)
	return sessionStartBoard(t, projDir)
}

// boardOff makes the session-start board load fail, so tests of the other
// session-start output see zero bytes of board.
func boardOff(t *testing.T) {
	t.Helper()
	old := loadBoard
	loadBoard = func(string, int, time.Time) (string, error) { return "", errors.New("board off") }
	t.Cleanup(func() { loadBoard = old })
}

// sessionStartBoard is sessionStart with the real board loader.
func sessionStartBoard(t *testing.T, projDir string) string {
	t.Helper()
	t.Setenv("CLAUDE_ENV_FILE", "")
	out, err := execCerneStdin(t, sessionStartJSON(t, "sess-f0409", projDir), "hook", "channel", "--point", "session-start")
	if err != nil {
		t.Fatalf("hook channel: %v\n%s", err, out)
	}
	return out
}

func setSyncSkills(t *testing.T, projDir string, on bool) {
	t.Helper()
	cfg, _, err := project.LoadConfig(projDir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.SyncSkills = on
	if err := project.SaveConfig(projDir, cfg); err != nil {
		t.Fatal(err)
	}
}

// TestHookChannelSessionStartStaleSkills (F-0405 crit. 2, 3, 6): a stale
// canonical skill is rewritten with a one-line note when sync_skills is on,
// only flagged (nothing written) when it is off, and an up-to-date project
// prints nothing.
func TestHookChannelSessionStartStaleSkills(t *testing.T) {
	projDir, _, _ := setupProject(t)
	if out := sessionStart(t, projDir); out != "" {
		t.Fatalf("everything current must print nothing, got %q", out)
	}
	stale := filepath.Join(projDir, ".claude", "skills", "cerne-self-learning", "SKILL.md")
	if err := os.WriteFile(stale, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := sessionStart(t, projDir)
	if !strings.Contains(out, "run `cerne update`") || strings.Count(out, "\n") != 1 || !strings.HasSuffix(out, "\n") {
		t.Fatalf("without sync_skills want one warning line naming the fix, got %q", out)
	}
	if raw, _ := os.ReadFile(stale); string(raw) != "old" {
		t.Fatal("warning mode must not write")
	}

	setSyncSkills(t, projDir, true)
	out = sessionStart(t, projDir)
	if !strings.Contains(out, "cerne-self-learning") || !strings.Contains(out, "next session") {
		t.Fatalf("with sync_skills want an updated note, got %q", out)
	}
	if raw, _ := os.ReadFile(stale); string(raw) == "old" {
		t.Fatal("sync_skills must rewrite the skill")
	}
	if out := sessionStart(t, projDir); out != "" {
		t.Fatalf("second start must be silent, got %q", out)
	}
}

// TestHookChannelSessionStartStaleHooks (F-0405 crit. 4): a diverging managed
// hook only produces a counted warning line; the file stays untouched. The
// subagent point does not run the check.
func TestHookChannelSessionStartStaleHooks(t *testing.T) {
	projDir, _, _ := setupProject(t)
	setSyncSkills(t, projDir, true)
	script := filepath.Join(projDir, ".claude", "hooks", hooks.Assets()[1].Script)
	if err := os.WriteFile(script, []byte("#!/bin/sh\n# local\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	out := sessionStart(t, projDir)
	if !strings.Contains(out, "1 managed hook") || !strings.Contains(out, "run `cerne update`") || strings.Count(out, "\n") != 1 {
		t.Fatalf("want a counted hook warning, got %q", out)
	}
	if raw, _ := os.ReadFile(script); string(raw) != "#!/bin/sh\n# local\n" {
		t.Fatal("the hook script was rewritten at session start")
	}
	sub, err := execCerneStdin(t, sessionStartJSON(t, "s", projDir), "hook", "channel", "--point", "subagent")
	if err != nil || sub != "" {
		t.Fatalf("subagent point: %v %q, want silence", err, sub)
	}
}

// BenchmarkHookChannelSessionStart (F-0405 crit. 5) measures what the feature
// adds to the session-start contact point: the managed-asset staleness check,
// with everything current and with one stale skill (sync_skills off, so the
// stale case does not write).
func BenchmarkHookChannelSessionStart(b *testing.B) {
	proj := b.TempDir()
	if _, err := skills.Install(proj); err != nil {
		b.Fatal(err)
	}
	if _, err := hooks.Install(proj); err != nil {
		b.Fatal(err)
	}
	b.Run("current", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if staleManagedNotice(proj, false) != "" {
				b.Fatal("unexpected notice")
			}
		}
	})
	b.Run("stale-skill", func(b *testing.B) {
		path := filepath.Join(proj, ".claude", "skills", "cerne-self-learning", "SKILL.md")
		if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
			b.Fatal(err)
		}
		for i := 0; i < b.N; i++ {
			if staleManagedNotice(proj, false) == "" {
				b.Fatal("expected notice")
			}
		}
	})
}

// TestHookChannelSessionStartFailOpen (F-0405 crit. 5): with sync_skills on and
// an unwritable skills tree, the rewrite fails silently — empty output, exit 0.
func TestHookChannelSessionStartFailOpen(t *testing.T) {
	projDir, _, _ := setupProject(t)
	setSyncSkills(t, projDir, true)
	skillsDir := filepath.Join(projDir, ".claude", "skills")
	stale := filepath.Join(skillsDir, "cerne-self-learning", "SKILL.md")
	if err := os.WriteFile(stale, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(stale)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(stale, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755); os.Chmod(stale, 0o644) })
	if out := sessionStart(t, projDir); out != "" {
		t.Fatalf("unwritable skills tree must be silent, got %q", out)
	}
}

// TestHookChannelSessionStartUnreadableIsUnknown (F-0405): a read error other
// than not-exist is not staleness — nothing printed, nothing rewritten.
func TestHookChannelSessionStartUnreadableIsUnknown(t *testing.T) {
	projDir, _, _ := setupProject(t)
	for _, on := range []bool{false, true} {
		setSyncSkills(t, projDir, on)
		skill := filepath.Join(projDir, ".claude", "skills", "cerne-self-learning", "SKILL.md")
		if err := os.Chmod(skill, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(skill, 0o644) })
		if out := sessionStart(t, projDir); out != "" {
			t.Fatalf("sync_skills=%v: unreadable skill must be silent, got %q", on, out)
		}
		os.Chmod(skill, 0o644)
	}
}

// boardSection cuts the "In progress" and "Pending handoffs" sections out of a
// generated CONTEXT.md.
func boardSection(t *testing.T, ctx string) string {
	t.Helper()
	a, b := strings.Index(ctx, "## In progress"), strings.Index(ctx, "## Features")
	if a < 0 || b < a {
		t.Fatalf("no board in CONTEXT.md:\n%s", ctx)
	}
	return ctx[a:b]
}

// TestHookChannelSessionStartBoard (F-0409 crit. 1): session start prints the
// board read from the clone, equal as a whole to the CONTEXT.md sections
// generated from the same clone with the same now and the same recent window
// (cfg.RecentWorkWindowDays(), the source `pull` uses), on a fixture with an
// active work, an idle work and pending handoffs.
func TestHookChannelSessionStartBoard(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	cfg, _, err := project.LoadConfig(projDir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.RecentWorkDays = 3
	if err := project.SaveConfig(projDir, cfg); err != nil {
		t.Fatal(err)
	}
	days := cfg.RecentWorkWindowDays()
	now := time.Now().UTC()
	for _, w := range []store.Work{
		{ID: "W-0001", Title: "Mexer no servidor", State: domain.WorkActive, Actor: "a", CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour)},
		{ID: "W-0002", Title: "Parado", State: domain.WorkActive, Actor: "a", CreatedAt: now.AddDate(0, 0, -5), UpdatedAt: now.AddDate(0, 0, -5)},
	} {
		if err := store.SaveWork(repoDir, w); err != nil {
			t.Fatal(err)
		}
	}
	for i, from := range []string{"W-0001", "W-0009"} {
		at := now.Add(-time.Duration(i+2) * time.Hour)
		h := store.Handoff{ID: fmt.Sprintf("HO-%04d", i+1), State: domain.HandoffPending, FromWork: from, Actor: "agent:a@b", CreatedAt: at, UpdatedAt: at}
		if err := store.SaveHandoff(repoDir, h); err != nil {
			t.Fatal(err)
		}
	}
	old := loadBoard
	loadBoard = func(dir string, d int, _ time.Time) (string, error) { return old(dir, d, now) }
	t.Cleanup(func() { loadBoard = old })

	docs := filepath.Join(t.TempDir(), ".docs")
	if _, err := materialize.MaterializeAt(repoDir, docs, "", false, days, now); err != nil {
		t.Fatal(err)
	}
	ctx, err := os.ReadFile(filepath.Join(docs, "CONTEXT.md"))
	if err != nil {
		t.Fatal(err)
	}
	want := strings.TrimLeft(boardSection(t, string(ctx)), "\n")
	want = strings.TrimRight(want, "\n") + "\n"
	out := sessionStartBoard(t, projDir)
	if out != want || !strings.Contains(out, "idle for over 3 days") || !strings.Contains(out, "## Pending handoffs") {
		t.Fatalf("board differs from CONTEXT.md sections\nwant:\n%s\ngot:\n%s", want, out)
	}
}

// F-0409 crit. 4: no active work is one line, not silence.
func TestHookChannelSessionStartBoardEmpty(t *testing.T) {
	projDir, _, _ := setupProject(t)
	if out := sessionStartBoard(t, projDir); out != "- no active work\n" {
		t.Fatalf("got %q", out)
	}
}

// F-0409 crit. 3: a failing or stalled load prints no board and exits 0.
func TestHookChannelSessionStartBoardFailOpen(t *testing.T) {
	projDir, _, _ := setupProject(t)
	old, oldD := loadBoard, boardDeadline
	t.Cleanup(func() { loadBoard, boardDeadline = old, oldD })
	loadBoard = func(string, int, time.Time) (string, error) { return "", errors.New("boom") }
	if out := sessionStartBoard(t, projDir); out != "" {
		t.Fatalf("error must print no board, got %q", out)
	}
	boardDeadline = 50 * time.Millisecond
	loadBoard = func(string, int, time.Time) (string, error) { time.Sleep(2 * time.Second); return "late\n", nil }
	start := time.Now()
	if out := sessionStartBoard(t, projDir); out != "" || time.Since(start) > time.Second {
		t.Fatalf("stalled load: out %q after %v", out, time.Since(start))
	}
}

// F-0409 crit. 6: the subagent point prints no board.
func TestHookChannelSubagentNoBoard(t *testing.T) {
	projDir, _, _ := setupProject(t)
	t.Setenv("CLAUDE_ENV_FILE", "")
	out, err := execCerneStdin(t, sessionStartJSON(t, "sess-sub", projDir), "hook", "channel", "--point", "subagent")
	if err != nil || out != "" {
		t.Fatalf("subagent: %v %q", err, out)
	}
}

// F-0409 crit. 2: a clone fetched more than 24 h ago is flagged after the
// board; a recent fetch, or none recorded, prints nothing extra.
func TestHookChannelSessionStartBoardFetchAge(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	fh := filepath.Join(repoDir, ".git", "FETCH_HEAD")
	if err := os.WriteFile(fh, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	set := func(age time.Duration) {
		at := time.Now().Add(-age)
		if err := os.Chtimes(fh, at, at); err != nil {
			t.Fatal(err)
		}
	}
	set(time.Hour)
	if out := sessionStartBoard(t, projDir); out != "- no active work\n" {
		t.Fatalf("recent fetch: %q", out)
	}
	set(50 * time.Hour)
	want := "- no active work\ncerne: the shared clone was last fetched 2 day(s) ago — run `cerne pull` to refresh it.\n"
	if out := sessionStartBoard(t, projDir); out != want {
		t.Fatalf("old fetch: got %q want %q", out, want)
	}
	os.Remove(fh)
	if out := sessionStartBoard(t, projDir); out != "- no active work\n" {
		t.Fatalf("no FETCH_HEAD: %q", out)
	}
}

// BenchmarkHookChannelSessionStartBoard (F-0409 crit. 3): what the board adds
// to the session-start contact point — load + render — on a fixture backend
// with 300 works (60 active) and 3000 events.
func BenchmarkHookChannelSessionStartBoard(b *testing.B) {
	dir := b.TempDir()
	if err := store.InitLayout(dir, store.Project{Name: "Bench"}); err != nil {
		b.Fatal(err)
	}
	now := time.Now().UTC()
	for i := 1; i <= 300; i++ {
		st := domain.WorkDone
		if i%5 == 0 {
			st = domain.WorkActive
		}
		at := now.Add(-time.Duration(i) * time.Hour)
		w := store.Work{ID: fmt.Sprintf("W-%04d", i), Title: "bench work", State: st, Actor: "agent", CreatedAt: at, UpdatedAt: at}
		if err := store.SaveWork(dir, w); err != nil {
			b.Fatal(err)
		}
	}
	for i := 0; i < 3000; i++ {
		ev := domain.Event{ID: fmt.Sprintf("e-%d", i), At: now.Add(-time.Duration(i) * time.Minute), EntityID: "F-0001", Type: "feature.doc_edited", Actor: "x"}
		if err := store.AppendEvent(dir, ev); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if out, err := loadBoardFromClone(dir, 14, time.Now()); err != nil || out == "" {
			b.Fatalf("%v %q", err, out)
		}
	}
}

// A panic in the loader fails open: no board, exit unchanged.
func TestHookChannelSessionStartBoardPanicFailsOpen(t *testing.T) {
	projDir, _, _ := setupProject(t)
	old := loadBoard
	t.Cleanup(func() { loadBoard = old })
	loadBoard = func(string, int, time.Time) (string, error) { panic("boom") }
	if out := sessionStartBoard(t, projDir); out != "" {
		t.Fatalf("got %q", out)
	}
}

// F-0409 crit. 2: exactly 24 h is recent, 25 h is not; days round down.
func TestFetchAgeNoticeBoundary(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	fh := filepath.Join(repo, ".git", "FETCH_HEAD")
	if err := os.WriteFile(fh, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for age, want := range map[time.Duration]string{
		24 * time.Hour: "",
		25 * time.Hour: "1 day(s) ago",
		47 * time.Hour: "1 day(s) ago",
		48 * time.Hour: "2 day(s) ago",
	} {
		at := now.Add(-age)
		if err := os.Chtimes(fh, at, at); err != nil {
			t.Fatal(err)
		}
		got := fetchAgeNotice(repo, now)
		if (want == "") != (got == "") || !strings.Contains(got, want) {
			t.Fatalf("age %v: got %q, want it to contain %q", age, got, want)
		}
	}
}

// TestHookChannelSessionStartOrder (F-0409): the F-0405 line, the board, the
// age line and the channel block come out in that order, real loader.
func TestHookChannelSessionStartOrder(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	t.Setenv("CLAUDE_ENV_FILE", "")
	startBoundWork(t, projDir, repoDir, "sess-f0409", "Mexer no servidor")
	b := startBoundWork(t, projDir, repoDir, "sess-other", "Revisar a interface")
	seedChannel(t, repoDir, b, "pode consumir")
	stale := filepath.Join(projDir, ".claude", "skills", "cerne-self-learning", "SKILL.md")
	if err := os.WriteFile(stale, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	fh := filepath.Join(repoDir, ".git", "FETCH_HEAD")
	if err := os.WriteFile(fh, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(-50 * time.Hour)
	if err := os.Chtimes(fh, at, at); err != nil {
		t.Fatal(err)
	}
	out := sessionStartBoard(t, projDir)
	last := -1
	for _, marker := range []string{"canonical skill(s) out of date", "## In progress", "last fetched 2 day(s) ago", "cerne channel:"} {
		i := strings.Index(out, marker)
		if i <= last {
			t.Fatalf("%q out of order (at %d, previous %d):\n%s", marker, i, last, out)
		}
		last = i
	}
}

// TestHookChannelPluginMode (F-0454 criterion 7): run from the Claude Code
// plugin (CERNE_PLUGIN=1), session start does not flag the project's hooks and
// skills that are simply absent — the plugin delivers them — nor writes them,
// even with sync_skills; a present copy that diverges is still flagged. It also
// stamps CERNE_ACTOR=agent for the session's Bash calls, which `cerne hook
// install`'s settings.json would otherwise carry, so the human gates hold.
func TestHookChannelPluginMode(t *testing.T) {
	projDir, _, _ := setupProject(t)
	setSyncSkills(t, projDir, true)
	for _, d := range []string{"hooks", "skills"} {
		if err := os.RemoveAll(filepath.Join(projDir, ".claude", d)); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("CERNE_PLUGIN", "1")
	if out := sessionStart(t, projDir); out != "" {
		t.Fatalf("plugin mode with absent assets must print nothing, got %q", out)
	}
	if _, err := os.Stat(filepath.Join(projDir, ".claude", "skills")); !os.IsNotExist(err) {
		t.Fatalf("plugin mode must not install the skills the plugin delivers (stat err %v)", err)
	}

	script := filepath.Join(projDir, ".claude", "hooks", hooks.Assets()[1].Script)
	if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("#!/bin/sh\n# local\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if out := sessionStart(t, projDir); !strings.Contains(out, "1 managed hook") {
		t.Fatalf("a present diverging hook must still be flagged, got %q", out)
	}

	env := filepath.Join(t.TempDir(), "env.sh")
	t.Setenv("CLAUDE_ENV_FILE", env)
	if out, err := execCerneStdin(t, sessionStartJSON(t, "f9cf13e4-914f-48fb-adfa-0cdced7a210e", projDir), "hook", "channel", "--point", "session-start"); err != nil {
		t.Fatalf("hook channel: %v\n%s", err, out)
	}
	if got, _ := os.ReadFile(env); string(got) != "export CERNE_SESSION=f9cf13e4-914f-48fb-adfa-0cdced7a210e\nexport CERNE_ACTOR=agent\n" {
		t.Fatalf("env file = %q, want the session and the agent actor", got)
	}
	t.Setenv("CERNE_PLUGIN", "")
	if err := os.Remove(env); err != nil {
		t.Fatal(err)
	}
	if out, err := execCerneStdin(t, sessionStartJSON(t, "s2", projDir), "hook", "channel", "--point", "session-start"); err != nil {
		t.Fatalf("hook channel: %v\n%s", err, out)
	}
	if got, _ := os.ReadFile(env); string(got) != "export CERNE_SESSION=s2\n" {
		t.Fatalf("outside the plugin the actor comes from settings.json, env file = %q", got)
	}
}

// TestHookChannelReachesNeverBoundSession (F-0486 crit. 8, critic finding 3):
// a session that only opened a card or wrote one of its documents has no
// binding file at all; a message from the card's page still reaches it at
// session start, naming the card and how to answer.
func TestHookChannelReachesNeverBoundSession(t *testing.T) {
	boardOff(t)
	when := pinLocal(t)
	projDir, _, repoDir := setupProject(t)
	t.Setenv("CLAUDE_ENV_FILE", "")
	at := time.Now().UTC()
	if _, err := app.ChannelPost(filepath.Dir(repoDir), "Henrique", "F-0001", []string{"sess-opener"}, "o plano está certo?", at); err != nil {
		t.Fatal(err)
	}
	out, err := execCerneStdin(t, sessionStartJSON(t, "sess-opener", projDir), "hook", "channel", "--point", "session-start")
	// F-0490 crit. 4: the report, with the clock in the human's local zone.
	report := "Henrique (human) wrote to this session from the page of F-0001 " + when(at) + ` local time: "o plano está certo?"`
	if err != nil || !strings.Contains(out, report) || !strings.Contains(out, "cerne msg send --ref F-0001") {
		t.Fatalf("session start of a never-bound addressed session: %v\n%q", err, out)
	}
	if out, err := execCerneStdin(t, sessionStartJSON(t, "sess-other", projDir), "hook", "channel", "--point", "session-start"); err != nil || strings.Contains(out, "plano") {
		t.Fatalf("an unaddressed session got it: %v\n%q", err, out)
	}
}
