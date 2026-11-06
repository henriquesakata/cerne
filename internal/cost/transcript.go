package cost

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ClaudeRoots returns the Claude Code config dirs whose projects/ hold the
// session transcripts: $CLAUDE_CONFIG_DIR when set (Claude Code's own
// override, seen by the CLI and the hooks alike), else ~/.config/claude and
// ~/.claude — the two places ccusage scans too.
func ClaudeRoots() []string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return []string{dir}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(home, ".config", "claude"), filepath.Join(home, ".claude")}
}

// tfile is one transcript file of a session: rel is its path under projects/
// (stable across roots), abs where it is read.
type tfile struct{ rel, abs string }

// sessionFiles indexes the transcripts of sessions under every root: the main
// <slug>/<sess>.jsonl plus <slug>/<sess>/subagents/*.jsonl, under ANY slug —
// the slug is the launch cwd, which need not be this project's (a session
// launched in the main checkout working in a worktree, and vice versa).
// Lookups go through directory listings, so a session id is never used to
// build a path it could escape with.
func sessionFiles(roots, sessions []string) (map[string][]tfile, error) {
	files := map[string][]tfile{}
	seen := map[string]bool{} // rel: the first root wins
	add := func(sess, rel, abs string) {
		if !seen[rel] {
			seen[rel] = true
			files[sess] = append(files[sess], tfile{rel: rel, abs: abs})
		}
	}
	for _, root := range roots {
		projects := filepath.Join(root, "projects")
		slugs, err := readDir(projects)
		if err != nil {
			return nil, err
		}
		for _, slug := range slugs {
			if !slug.IsDir() {
				continue
			}
			dir := filepath.Join(projects, slug.Name())
			entries, err := readDir(dir)
			if err != nil {
				return nil, err
			}
			names := make(map[string]bool, len(entries))
			for _, e := range entries {
				names[e.Name()] = true
			}
			for _, sess := range sessions {
				if names[sess+".jsonl"] {
					add(sess, slug.Name()+"/"+sess+".jsonl", filepath.Join(dir, sess+".jsonl"))
				}
				if !names[sess] {
					continue
				}
				subs, err := subagentFiles(dir, slug.Name(), sess)
				if err != nil {
					return nil, err
				}
				for _, f := range subs {
					add(sess, f.rel, f.abs)
				}
			}
		}
	}
	return files, nil
}

// subagentFiles lists <dir>/<sess>/subagents/*.jsonl, dir being the project
// slug's directory.
func subagentFiles(dir, slug, sess string) ([]tfile, error) {
	subDir := filepath.Join(dir, sess, "subagents")
	entries, err := readDir(subDir)
	var files []tfile
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
			files = append(files, tfile{rel: slug + "/" + sess + "/subagents/" + e.Name(), abs: filepath.Join(subDir, e.Name())})
		}
	}
	return files, err
}

// readDir lists dir: a missing dir is empty; any other failure is
// ErrTranscriptsUnreadable naming it — a denied read must never read as a
// pruned transcript (that would print a false zero).
func readDir(dir string) ([]os.DirEntry, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTranscriptsUnreadable, err)
	}
	return entries, nil
}

// msg is one deduplicated assistant message of a session.
type msg struct {
	key       string    // message.id + requestId; "" never deduplicates
	ts        time.Time // zero when unreadable: counted as unattributed
	session   string
	agent     string // subagent id; "" on the main thread
	model     string
	file      string // tfile.rel
	sidechain bool
	c         Counters
}

// rawLine is the part of a transcript line cost reads.
type rawLine struct {
	Type        string `json:"type"`
	Timestamp   string `json:"timestamp"`
	RequestID   string `json:"requestId"`
	IsSidechain bool   `json:"isSidechain"`
	AgentID     string `json:"agentId"`
	Message     struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage *struct {
			Input         int64 `json:"input_tokens"`
			CacheCreation int64 `json:"cache_creation_input_tokens"`
			CacheRead     int64 `json:"cache_read_input_tokens"`
			Output        int64 `json:"output_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

var usageKey = []byte(`"usage"`)

// readSession reads the assistant messages of one session's files. Streaming
// writes one line per content block, each repeating the usage, so lines with
// the same message.id + requestId are ONE message: the max per counter (the
// last chunk carries the real output), the earliest ts, the rest from the
// first line. <synthetic> messages and non-assistant lines are skipped; a file
// gone since the index is skipped, any other read failure is an error.
func readSession(sess string, files []tfile) ([]msg, error) {
	var msgs []msg
	byKey := map[string]int{}
	for _, f := range files {
		err := eachLine(f.abs, func(line []byte) {
			if !bytes.Contains(line, usageKey) {
				return
			}
			var l rawLine
			if json.Unmarshal(line, &l) != nil || l.Type != "assistant" || l.Message.Usage == nil || l.Message.Model == "<synthetic>" {
				return
			}
			u := l.Message.Usage
			m := msg{session: sess, agent: l.AgentID, model: l.Message.Model, file: f.rel, sidechain: l.IsSidechain,
				c: Counters{Input: u.Input, CacheCreation: u.CacheCreation, CacheRead: u.CacheRead, Output: u.Output}}
			m.ts, _ = time.Parse(time.RFC3339Nano, l.Timestamp)
			if l.Message.ID != "" && l.RequestID != "" {
				m.key = l.Message.ID + "\x00" + l.RequestID
				if i, ok := byKey[m.key]; ok {
					prev := &msgs[i]
					prev.c = prev.c.max(m.c)
					if !m.ts.IsZero() && (prev.ts.IsZero() || m.ts.Before(prev.ts)) {
						prev.ts = m.ts
					}
					return
				}
				byKey[m.key] = len(msgs)
			}
			msgs = append(msgs, m)
		})
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrTranscriptsUnreadable, err)
		}
	}
	return msgs, nil
}

// eachLine calls fn with every line of path. bufio.Reader, not Scanner:
// transcript lines run to several MB.
func eachLine(path string, fn func([]byte)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	r := bufio.NewReader(f)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			fn(line)
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}
