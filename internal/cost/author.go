package cost

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// authorTail bounds how much of each transcript file AuthorModel reads: the
// running tool_use is the newest call of its file, so only the tail can hold
// it. ponytail: a sibling output past 1 MiB written after it hides it — the
// read then falls back like any unmatched call.
const authorTail = 1 << 20

// AuthorModel returns the model of the assistant running cerne with args (this
// process's os.Args[1:]) right now in Claude Code session sess (F-0375): the
// one whose Bash tool_use calls cerne with the same verb words (verbWords) and
// has no tool_result yet, in the main file or in a
// subagent's — the CLI does not know which subagent called it, so the pending
// call is what tells them apart. Pending calls with different models are
// ambiguous and give "" (never a guess); no pending call gives the model of
// the session's last assistant message. found is false when the session has
// no readable transcript with an assistant message.
func AuthorModel(roots []string, sess string, args []string) (model string, found bool) {
	files, err := sessionFiles(roots, []string{sess})
	if err != nil {
		return "", false
	}
	var pending []string
	var last time.Time
	for _, f := range files[sess] {
		p, lm, lts, ok := scanAuthor(f.abs, sess, args)
		if !ok {
			continue
		}
		pending = append(pending, p...)
		if !found || lts.After(last) {
			model, last, found = lm, lts, true
		}
	}
	slices.Sort(pending)
	switch pending = slices.Compact(pending); {
	case len(pending) == 1:
		return pending[0], true
	case len(pending) > 1:
		return "", true
	}
	return model, found
}

// authorLine is the part of a transcript line AuthorModel reads.
type authorLine struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	SessionID string `json:"sessionId"`
	Message   struct {
		Model   string          `json:"model"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// authorBlock is one content block: a tool_use or a tool_result.
type authorBlock struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	Name      string `json:"name"`
	ToolUseID string `json:"tool_use_id"`
	Input     struct {
		Command string `json:"command"`
	} `json:"input"`
}

var (
	assistantKey  = []byte(`"assistant"`)
	toolResultKey = []byte(`"tool_result"`)
)

// scanAuthor reads the tail of one transcript file: the models of its pending
// Bash calls running cerne with args' verb, and its last assistant message.
// ok is false when the tail holds no assistant message of sess.
func scanAuthor(path, sess string, args []string) (pending []string, lastModel string, lastTS time.Time, ok bool) {
	tail, err := readTail(path, authorTail)
	if err != nil {
		return nil, "", time.Time{}, false
	}
	calls := map[string]string{} // tool_use id → model, while unanswered
	for line := range bytes.SplitSeq(tail, []byte("\n")) {
		if !bytes.Contains(line, assistantKey) && !bytes.Contains(line, toolResultKey) {
			continue
		}
		var l authorLine
		if json.Unmarshal(line, &l) != nil || (l.SessionID != "" && l.SessionID != sess) {
			continue
		}
		var blocks []authorBlock
		_ = json.Unmarshal(l.Message.Content, &blocks) // a string content has no blocks
		switch {
		case l.Type == "assistant" && l.Message.Model != "" && l.Message.Model != "<synthetic>":
			ts, _ := time.Parse(time.RFC3339Nano, l.Timestamp)
			if !ok || !ts.Before(lastTS) {
				lastModel, lastTS, ok = l.Message.Model, ts, true
			}
			for _, b := range blocks {
				if b.Type == "tool_use" && b.Name == "Bash" && runs(b.Input.Command, args) {
					calls[b.ID] = l.Message.Model
				}
			}
		case l.Type == "user":
			for _, b := range blocks {
				if b.Type == "tool_result" {
					delete(calls, b.ToolUseID)
				}
			}
		}
	}
	for _, m := range calls {
		pending = append(pending, m)
	}
	return pending, lastModel, lastTS, ok
}

// runs reports whether a shell command calls cerne with the verb words of
// args: a simple command that starts with cerne (any path ending in /cerne),
// after VAR=value or env prefixes, whose own verb words begin with those.
// Text in quotes or in another command's arguments is no call, and args
// with no verb words match nothing.
func runs(command string, args []string) bool {
	want := verbWords(args)
	if len(want) == 0 {
		return false
	}
	for _, seg := range commandSegments(command) {
		i := 0
		for i < len(seg) && (seg[i] == "env" || envAssign.MatchString(seg[i])) {
			i++
		}
		if i < len(seg) && filepath.Base(seg[i]) == "cerne" {
			if got := verbWords(seg[i+1:]); len(got) >= len(want) && slices.Equal(got[:len(want)], want) {
				return true
			}
		}
	}
	return false
}

// envAssign is a VAR=value prefix of a simple command.
var envAssign = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// valueless are the flags that take no value before cerne's verb.
var valueless = map[string]bool{"--verbose": true, "-h": true, "--help": true, "--version": true}

// verbWords is the command a cerne argument list names: its first two words
// ("push", "feature spec"), skipping flags and, but for the valueless ones,
// the value after a flag written without '='.
func verbWords(args []string) []string {
	var words []string
	for i := 0; i < len(args) && len(words) < 2; i++ {
		switch a := args[i]; {
		case strings.HasPrefix(a, "-"):
			if !strings.Contains(a, "=") && !valueless[a] {
				i++
			}
		default:
			words = append(words, a)
		}
	}
	return words
}

// commandSegments splits a shell command into the words of its simple
// commands: quotes group and are removed, a backslash escapes, and an unquoted
// ; & | ( ) ` or newline ends a command. ponytail: no heredoc parsing — a
// line inside a heredoc body that starts with cerne reads as a call.
func commandSegments(command string) [][]string {
	var segs [][]string
	var words []string
	var cur strings.Builder
	inWord, esc := false, false
	var quote rune
	flush := func() {
		if inWord {
			words = append(words, cur.String())
			cur.Reset()
			inWord = false
		}
	}
	for _, r := range command {
		switch {
		case esc:
			cur.WriteRune(r)
			inWord, esc = true, false
		case quote != 0 && r == quote:
			quote = 0
		case quote == '"' && r == '\\':
			esc = true
		case quote != 0:
			cur.WriteRune(r)
		case r == '\\':
			esc, inWord = true, true
		case r == '\'' || r == '"':
			quote, inWord = r, true
		case r == ' ' || r == '\t':
			flush()
		case strings.ContainsRune(";&|()`\n", r):
			flush()
			if len(words) > 0 {
				segs = append(segs, words)
				words = nil
			}
		default:
			cur.WriteRune(r)
			inWord = true
		}
	}
	flush()
	if len(words) > 0 {
		segs = append(segs, words)
	}
	return segs
}

// readTail returns the last n bytes of path, starting at a line boundary.
func readTail(path string, n int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	off := max(st.Size()-n, 0)
	buf, err := io.ReadAll(io.NewSectionReader(f, off, st.Size()-off))
	if err != nil {
		return nil, err
	}
	if off > 0 {
		if i := bytes.IndexByte(buf, '\n'); i >= 0 {
			buf = buf[i+1:]
		}
	}
	return buf, nil
}
