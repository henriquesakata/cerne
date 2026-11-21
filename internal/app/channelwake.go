package app

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/henriquesakata/cerne/internal/cost"
	"github.com/henriquesakata/cerne/internal/room"
)

// Waking an idle session (F-0491). A session waiting for its user passes no
// contact point, so a message addressed to it waits until someone types there.
// Claude Code's cross-session messaging opens a turn in an idle session for one
// JSON line written to its inbox socket (N-0139, R-0130); the channel writes
// that line right after a post, to the sessions the post is addressed to.
//
// It never gets around the harness's hold: a session in bypassPermissions holds
// a message with no from-mode, and cerne neither declares one nor presents the
// session's child messaging token, which it never reads.
// Such a session — and one whose mode cerne does not know — is not written to;
// it hears the channel at its next contact point, and the UI says why.

// Why a target was not woken (served as ChannelTarget.Reason; the UI labels it).
const (
	WakeBypass            = "bypass"             // bypassPermissions holds a message with no from-mode
	WakeUnknownMode       = "unknown_mode"       // no recorded mode, plan, or a mode cerne does not know: treated as bypass
	WakeNoWake            = "no_wake"            // the session's harness cannot open a turn in an idle session
	WakeNoRegistry        = "no_registry"        // Claude Code's session registry has no usable entry: ended, or no cross-session messaging
	WakeSocketMissing     = "socket_missing"     // ENOENT
	WakeConnectionRefused = "connection_refused" // ECONNREFUSED
	WakeUntrustedSocket   = "untrusted_socket"   // not a socket of this user's, in a directory of this user's
	WakeCooldown          = "cooldown"           // woken less than wakeCooldown ago (criterion 9)
	WakeCap               = "cap"                // the post already woke wakeCap sessions (criterion 9)
	WakeWriteFailed       = "write_failed"
)

// wakeModes are the permission modes measured to take a message with no
// from-mode (N-0139). bypassPermissions holds it, and so does plan when bypass
// is available, which cerne cannot tell.
var wakeModes = []string{"default", "acceptEdits", "auto", "dontAsk"}

// Criterion 9: two agents citing each other must not ping-pong turns, and one
// post must not open a turn in every session. Vars so tests can lower them.
var (
	wakeCooldown = 2 * time.Minute // a session gets at most one wake attempt per window
	wakeCap      = 5               // sessions one post may wake
)

// wakeBudget is the one deadline every write of a post shares: the writes run
// outside the channel lock, so a hung peer delays only the post.
var wakeBudget = time.Second

// wakeWrite writes to an inbox; tests stand in a slow or hung peer.
var wakeWrite = writeInbox

// wakeSender names the channel in the envelope: the model sees who wrote, and
// the terminal shows a one-line notice instead of the whole text (N-0139).
const wakeSender = "cerne channel"

// wakeClaim is one wake decided under the lock and written after it.
type wakeClaim struct {
	sess, work, socket, text string
	seqs                     []int
}

// ChannelWakeIdle wakes the idle sessions message m is addressed to (criteria
// 1, 2, 4, 6 and 9). In three steps, so the channel lock is never held across
// a socket: under the lock each session's text — what is addressed to it,
// unanswered, never shown and never claimed, in F-0490's report form — is
// rendered and claimed (a concurrent post leaves a claimed seq alone); then
// every write runs at once under one deadline; then the write is recorded as
// a delivery, or its failure as a reason row. A session that cannot be woken
// gets a reason row for m alone. It never fails a post.
func ChannelWakeIdle(cacheDir, repoDir string, m room.Message, now time.Time) {
	ch := channelOf(cacheDir)
	var claims []wakeClaim
	for _, sess := range wakeTargets(cacheDir, repoDir, m) {
		id, _ := sessionWork(cacheDir, repoDir, sess)
		socket, reason := wakeRoute(cacheDir, sess)
		if socket == "" && reason == "" {
			continue // busy: it hears the channel at its next tool call or its Stop
		}
		if reason == "" {
			var c *wakeClaim
			c, reason = claimWake(ch, repoDir, sess, id, m, len(claims), now)
			if c != nil {
				c.socket = socket
				claims = append(claims, *c)
				continue
			}
		}
		if reason != "" {
			recordUnwoken(ch, sess, id.Work, m.Seq, reason, now)
		}
	}
	deadline := time.Now().Add(wakeBudget)
	errs := make([]error, len(claims))
	var wg sync.WaitGroup
	for i, c := range claims {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = wakeWrite(deadline, c.socket, c.sess, c.text)
		}()
	}
	wg.Wait()
	for i, c := range claims {
		if why := writeReason(errs[i]); why != "" {
			recordUnwoken(ch, c.sess, c.work, m.Seq, why, now)
			continue
		}
		_ = ch.Update(func([]room.Message, []room.Delivery) (*room.Delivery, error) {
			return &room.Delivery{Session: c.sess, Work: c.work, Seqs: c.seqs, At: now}, nil
		})
	}
}

// claimWake renders and claims, under the channel lock, the wake of session
// sess for m: nil and "" when m is no longer due to it (shown, answered,
// claimed), nil and a reason when the cooldown or the post's cap stops it.
func claimWake(ch room.Channel, repoDir, sess string, id channelIdentity, m room.Message, claimed int, now time.Time) (*wakeClaim, string) {
	var c *wakeClaim
	var why string
	_ = ch.Update(func(msgs []room.Message, dels []room.Delivery) (*room.Delivery, error) {
		cs := channelState{msgs: msgs, dels: dels}
		pending, _ := cs.pending(liveMessages(repoDir, msgs, now), sess, id)
		pending = slices.DeleteFunc(pending, func(it injItem) bool { return !it.directed || it.shown || cs.claimed(sess, it.m.Seq) })
		if !slices.ContainsFunc(pending, func(it injItem) bool { return it.m.Seq == m.Seq }) {
			return nil, nil
		}
		switch {
		case cs.claimedSince(sess, now.Add(-wakeCooldown)):
			why = WakeCooldown
			return nil, nil
		case claimed >= wakeCap:
			why = WakeCap
			return nil, nil
		}
		text, inline := renderInjection(pending, "cerne channel:\n")
		seqs := make([]int, len(inline))
		for i, im := range inline {
			seqs[i] = im.Seq
		}
		if !slices.Contains(seqs, m.Seq) {
			return nil, nil // m did not fit inline: it waits for a contact point
		}
		c = &wakeClaim{sess: sess, work: id.Work, text: text, seqs: seqs}
		return &room.Delivery{Session: sess, Work: id.Work, Waking: seqs, At: now}, nil
	})
	return c, why
}

// recordUnwoken records that session sess was not woken for seq, and why.
func recordUnwoken(ch room.Channel, sess, work string, seq int, why string, now time.Time) {
	_ = ch.Update(func([]room.Message, []room.Delivery) (*room.Delivery, error) {
		return &room.Delivery{Session: sess, Work: work, Unwoken: []int{seq}, Reason: why, At: now}, nil
	})
}

// claimed reports whether a wake of session sess ever claimed seq: each
// (session, seq) gets one wake attempt; a failed one waits for a contact point.
func (cs channelState) claimed(sess string, seq int) bool {
	return slices.ContainsFunc(cs.dels, func(d room.Delivery) bool { return d.Session == sess && slices.Contains(d.Waking, seq) })
}

// claimedSince reports a wake attempt on session sess after since.
func (cs channelState) claimedSince(sess string, since time.Time) bool {
	return slices.ContainsFunc(cs.dels, func(d room.Delivery) bool {
		return d.Session == sess && len(d.Waking) > 0 && d.At.After(since)
	})
}

// wakeTargets are the sessions m may wake (criterion 6): the sessions a card's
// page message is for; for a human's message, the sessions whose work it cites
// by W- (the work is the session's nick on the channel) or by a card the work
// refs — a human's message citing nobody wakes nobody; for an agent's, the
// sessions it is addressed to. Never the sender, never a subagent's thread.
func wakeTargets(cacheDir, repoDir string, m room.Message) []string {
	if len(m.To) > 0 {
		return slices.DeleteFunc(slices.Clone(m.To), func(s string) bool { return s == m.Session })
	}
	if m.Ref != "" && !m.Human {
		return nil // a reply on a card's thread is for the page
	}
	var out []string
	for name := range sessionBindingsIn(cacheDir) {
		sess, agent := splitBindingName(name)
		if agent != "" || sess == m.Session || slices.Contains(out, sess) {
			continue
		}
		id, ok := sessionWork(cacheDir, repoDir, sess)
		if ok && (m.Human && citesWork(m, id) || !m.Human && directedTo(m, id)) {
			out = append(out, sess)
		}
	}
	slices.Sort(out)
	return out
}

// wakeRoute is session sess's inbox socket, or why it cannot be woken; both
// empty when it is busy.
func wakeRoute(cacheDir, sess string) (socket, reason string) {
	mode, known := SessionModeOf(cacheDir, sess)
	// The Windows inbox demands the auth line, which needs the token: never.
	if known && mode.Harness != cost.HarnessClaudeCode || runtime.GOOS == "windows" {
		return "", WakeNoWake
	}
	reg, ok := registeredSession(sess)
	switch {
	case !ok || reg.Socket == "" || reg.Status != "idle" && reg.Status != "busy":
		return "", WakeNoRegistry
	case reg.Status == "busy":
		return "", ""
	case mode.Mode == "bypassPermissions":
		return "", WakeBypass
	case !slices.Contains(wakeModes, mode.Mode):
		return "", WakeUnknownMode
	}
	if why := socketTrust(reg.Socket); why != "" {
		return "", why
	}
	return reg.Socket, ""
}

// claudeSession is what the wake reads of Claude Code's session registry
// (<config dir>/sessions/<pid>.json). It is undocumented: a field missing or
// renamed reads as no entry.
type claudeSession struct {
	SessionID string `json:"sessionId"`
	Socket    string `json:"messagingSocketPath"`
	Status    string `json:"status"`
	UpdatedAt int64  `json:"updatedAt"`
	pid       int
	mtime     time.Time
}

// newerThan orders two entries of one session: newest updatedAt, then newest
// file mtime, then highest pid — never the directory's order.
func (s claudeSession) newerThan(o claudeSession) bool {
	switch {
	case s.UpdatedAt != o.UpdatedAt:
		return s.UpdatedAt > o.UpdatedAt
	case !s.mtime.Equal(o.mtime):
		return s.mtime.After(o.mtime)
	}
	return s.pid > o.pid
}

// registryFileRe admits <pid>.json only: the <pid>.<hash>.key files beside the
// entries are the sessions' credentials and are never opened.
var registryFileRe = regexp.MustCompile(`^([0-9]+)\.json$`)

// registeredSession is sess's newest entry in the registry of every Claude
// Code config dir (CostRoots: $CLAUDE_CONFIG_DIR, ~/.config/claude, ~/.claude).
func registeredSession(sess string) (claudeSession, bool) {
	var best claudeSession
	found := false
	for _, root := range CostRoots() {
		entries, err := os.ReadDir(filepath.Join(root, "sessions"))
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := registryFileRe.FindStringSubmatch(e.Name())
			if e.IsDir() || name == nil {
				continue
			}
			path := filepath.Join(root, "sessions", e.Name())
			raw, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			var s claudeSession
			if json.Unmarshal(raw, &s) != nil || s.SessionID != sess {
				continue
			}
			s.pid, _ = strconv.Atoi(name[1])
			if info, err := os.Stat(path); err == nil {
				s.mtime = info.ModTime()
			}
			if !found || s.newerThan(best) {
				best, found = s, true
			}
		}
	}
	return best, found
}

// inboxLine is the one line the inbox takes (N-0139): a user message for the
// session by id — a pid reused by another session drops it. No auth line, no
// from, no from-mode.
type inboxLine struct {
	Type      string       `json:"type"`
	SessionID string       `json:"session_id"`
	Message   inboxMessage `json:"message"`
}

type inboxMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// writeInbox writes text to the inbox at socket, for session sess, by
// deadline. The inbox never answers on the connection, so a nil error is the
// whole receipt.
func writeInbox(deadline time.Time, socket, sess, text string) error {
	line, err := json.Marshal(inboxLine{Type: "user", SessionID: sess, Message: inboxMessage{Role: "user", Content: wakeEnvelope(text)}})
	if err != nil {
		return err
	}
	conn, err := net.DialTimeout("unix", socket, time.Until(deadline))
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := conn.SetWriteDeadline(deadline); err != nil {
		return err
	}
	_, err = conn.Write(append(line, '\n'))
	return err
}

// A message body must not close the envelope and open one of its own, nor
// spell a from-mode anywhere a parser might take it for the envelope's: the
// tag's "<" and the attribute's hyphen are swapped for look-alikes.
var (
	envelopeTagRe = regexp.MustCompile(`(?i)<(\s*/?\s*cross-session-message)`)
	fromModeRe    = regexp.MustCompile(`(?i)from-mode`)
)

// wakeEnvelope wraps the channel's report in the cross-session envelope, named
// as the channel and asserting no permission mode (criterion 3).
func wakeEnvelope(text string) string {
	text = envelopeTagRe.ReplaceAllString(strings.TrimRight(text, "\n"), "‹$1")
	text = fromModeRe.ReplaceAllString(text, "from_mode")
	return "<cross-session-message from-name=\"" + wakeSender + "\">\n" + text + "\n</cross-session-message>"
}

// wokenRe finds the channel's envelope in a prompt — Claude Code may add
// attributes (from=) — and wokenSeqRe the report lines' seqs inside it.
var (
	wokenRe    = regexp.MustCompile(`(?s)<cross-session-message\b[^>]*\bfrom-name="` + wakeSender + `"[^>]*>(.*?)</cross-session-message>`)
	wokenSeqRe = regexp.MustCompile(`(?m)^#([0-9]+) `)
)

// wokenSeqs are the seqs a woken turn's prompt shows (criterion 5): every
// report line of every cerne channel envelope in it.
func wokenSeqs(prompt string) []int {
	var out []int
	for _, env := range wokenRe.FindAllStringSubmatch(prompt, -1) {
		for _, m := range wokenSeqRe.FindAllStringSubmatch(env[1], -1) {
			if n, err := strconv.Atoi(m[1]); err == nil {
				out = append(out, n)
			}
		}
	}
	return out
}

// writeReason is the reason a failed write leaves on the target; "" for none.
func writeReason(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, syscall.ENOENT):
		return WakeSocketMissing
	case errors.Is(err, syscall.ECONNREFUSED):
		return WakeConnectionRefused
	}
	return WakeWriteFailed
}
