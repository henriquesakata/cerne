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
// session's child token (CLAUDE_CODE_MESSAGING_TOKEN), which it never reads.
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
	WakeWriteFailed       = "write_failed"
)

// wakeModes are the permission modes measured to take a message with no
// from-mode (N-0139). bypassPermissions holds it, and so does plan when bypass
// is available, which cerne cannot tell.
var wakeModes = []string{"default", "acceptEdits", "auto", "dontAsk"}

// wakeTimeout bounds the connect and the write: the channel lock is held
// meanwhile, and a hook waiting on it gives up after 300 ms (fail-open).
var wakeTimeout = 500 * time.Millisecond

// wakeSender names the channel in the envelope: the model sees who wrote, and
// the terminal shows a one-line notice instead of the whole text (N-0139).
const wakeSender = "cerne channel"

// ChannelWakeIdle wakes the idle sessions message m is addressed to (criteria
// 1, 2, 4 and 6). Each one gets ChannelWake's text — what is addressed to it,
// unanswered and never shown, in F-0490's report form — and the write is a
// delivery; a session that cannot be woken gets a reason row instead. It never
// fails a post: every error is a reason.
func ChannelWakeIdle(cacheDir, repoDir string, m room.Message, now time.Time) {
	for _, sess := range wakeTargets(cacheDir, repoDir, m) {
		socket, reason := wakeRoute(cacheDir, sess)
		if socket == "" && reason == "" {
			continue // busy: it hears the channel at its next tool call or its Stop
		}
		_, _ = channelInject(cacheDir, repoDir, sess, injectOpts{consume: true, mode: injWake, deliver: func(text string) string {
			if reason != "" {
				return reason
			}
			return writeReason(writeInbox(socket, sess, text))
		}}, now)
	}
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
}

// registryFileRe admits <pid>.json only: the <pid>.<hash>.key files beside the
// entries are the sessions' credentials and are never opened.
var registryFileRe = regexp.MustCompile(`^[0-9]+\.json$`)

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
			if e.IsDir() || !registryFileRe.MatchString(e.Name()) {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(root, "sessions", e.Name()))
			if err != nil {
				continue
			}
			var s claudeSession
			if json.Unmarshal(raw, &s) != nil || s.SessionID != sess {
				continue
			}
			if !found || s.UpdatedAt > best.UpdatedAt {
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

// writeInbox writes text to the inbox at socket, for session sess. The inbox
// never answers on the connection, so a nil error is the whole receipt.
func writeInbox(socket, sess, text string) error {
	line, err := json.Marshal(inboxLine{Type: "user", SessionID: sess, Message: inboxMessage{Role: "user", Content: wakeEnvelope(text)}})
	if err != nil {
		return err
	}
	conn, err := net.DialTimeout("unix", socket, wakeTimeout)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := conn.SetWriteDeadline(time.Now().Add(wakeTimeout)); err != nil {
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
