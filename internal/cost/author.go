package cost

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// authorTail bounds how much of each transcript file AuthorModel reads: the
// running tool_use is the newest call of its file, so only the tail can hold
// it. ponytail: a sibling output past 1 MiB written after it hides it — the
// read then falls back like any unmatched call.
const authorTail = 1 << 20

// AuthorModel returns the model of the assistant running `cerne <invocation>`
// right now in Claude Code session sess (F-0375): the one whose Bash tool_use
// carrying that invocation has no tool_result yet, in the main file or in a
// subagent's — the CLI does not know which subagent called it, so the pending
// call is what tells them apart. Pending calls with different models are
// ambiguous and give "" (never a guess); no pending call gives the model of
// the session's last assistant message. found is false when the session has
// no readable transcript with an assistant message.
func AuthorModel(roots []string, sess string, invocation []string) (model string, found bool) {
	files, err := sessionFiles(roots, []string{sess})
	if err != nil {
		return "", false
	}
	var pending []string
	var last time.Time
	for _, f := range files[sess] {
		p, lm, lts, ok := scanAuthor(f.abs, sess, invocation)
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
// Bash calls running invocation, and its last assistant message. ok is false
// when the tail holds no assistant message of sess.
func scanAuthor(path, sess string, invocation []string) (pending []string, lastModel string, lastTS time.Time, ok bool) {
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
				if b.Type == "tool_use" && b.Name == "Bash" && runs(b.Input.Command, invocation) {
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

// runs reports whether a shell command calls `cerne` followed by the words of
// invocation: a cerne token (any path ending in /cerne) then exactly those
// tokens, split on whitespace and shell separators.
func runs(command string, invocation []string) bool {
	tokens := strings.FieldsFunc(command, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || strings.ContainsRune(";|&()`'\"", r)
	})
	for i, tok := range tokens {
		if filepath.Base(tok) == "cerne" && len(tokens)-i-1 >= len(invocation) && slices.Equal(tokens[i+1:i+1+len(invocation)], invocation) {
			return true
		}
	}
	return false
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
