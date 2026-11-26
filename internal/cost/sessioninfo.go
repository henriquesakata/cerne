package cost

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"strings"
	"time"
)

// SessionInfo is where a session is, read from its transcript (F-0486): the
// title the app shows, the folder, the branch, the PR and the last activity,
// plus each subagent's type. Never the conversation: only these keys are
// decoded, so no text of it can reach a struct, a log or a response.
type SessionInfo struct {
	Title  string            `json:"title,omitempty"`
	Cwd    string            `json:"cwd,omitempty"`
	Branch string            `json:"branch,omitempty"`
	PR     string            `json:"pr,omitempty"`
	Last   time.Time         `json:"last,omitzero"`
	Found  bool              `json:"found"`
	Agents map[string]string `json:"agents,omitempty"` // subagent id → agentType ("" when unknown)
}

// locationLine is the part of a transcript line SessionInfos decodes.
type locationLine struct {
	Type        string `json:"type"`
	CustomTitle string `json:"customTitle"`
	Cwd         string `json:"cwd"`
	GitBranch   string `json:"gitBranch"`
	PRURL       string `json:"prUrl"`
}

var locationKeys = [][]byte{[]byte(`"customTitle"`), []byte(`"prUrl"`), []byte(`"cwd"`)}

// SessionInfos reads the location of every session under roots — Claude
// Code's transcripts and Cursor's agent-transcripts (F-0513). A session with
// no main transcript here (another harness, a pruned transcript) is
// Found=false. Last is the newest mtime of the session's files: both harnesses
// append on every activity.
func SessionInfos(roots, sessions []string) (map[string]SessionInfo, error) {
	files, err := sessionFiles(roots, sessions)
	if err != nil {
		return nil, err
	}
	cursor, err := cursorSessionFiles(roots, sessions)
	if err != nil {
		return nil, err
	}
	out := make(map[string]SessionInfo, len(sessions))
	for _, sess := range sessions {
		var info SessionInfo
		var mainMod time.Time
		for _, f := range files[sess] {
			st, err := os.Stat(f.abs)
			if err != nil {
				continue // gone since the listing
			}
			if st.ModTime().After(info.Last) {
				info.Last = st.ModTime()
			}
			if strings.Contains(f.rel, "/subagents/") {
				if id, ok := strings.CutPrefix(strings.TrimSuffix(path.Base(f.rel), ".jsonl"), "agent-"); ok {
					if info.Agents == nil {
						info.Agents = map[string]string{}
					}
					info.Agents[id] = agentType(f.abs)
				}
				continue
			}
			// The same session under two slugs (a resume copy): the newest wins.
			if info.Found && !st.ModTime().After(mainMod) {
				continue
			}
			if loc, err := readLocation(f.abs, st.Size()); err == nil {
				info.Title, info.Cwd, info.Branch, info.PR = loc.CustomTitle, loc.Cwd, loc.GitBranch, loc.PRURL
				info.Found, mainMod = true, st.ModTime()
			}
		}
		// A Cursor session (F-0513): its lines carry no title, folder or
		// branch, so only the files' metadata is read — never a line.
		for _, f := range cursor[sess] {
			st, err := os.Stat(f.abs)
			if err != nil {
				continue // no main transcript, or gone since the listing
			}
			if st.ModTime().After(info.Last) {
				info.Last = st.ModTime()
			}
			if !strings.Contains(f.rel, "/subagents/") {
				info.Found = true
				continue
			}
			if info.Agents == nil {
				info.Agents = map[string]string{}
			}
			info.Agents[strings.TrimSuffix(path.Base(f.rel), ".jsonl")] = ""
		}
		out[sess] = info
	}
	return out, nil
}

// maxLocationWindow caps how much of a transcript's end readLocation holds in
// memory (the agents panel reads it on every reload).
const maxLocationWindow = 8 << 20

// readLocation scans the end of a transcript for the last title, folder,
// branch and PR. Claude Code repeats the title and PR lines every turn and
// stamps cwd/gitBranch on every message, so the last MiB almost always holds
// them; without a folder there, the window widens once, to 8 MiB.
// ponytail: a title, folder or PR seen only before the last 8 MiB is missed —
// a streaming full scan if that ever matters.
func readLocation(p string, size int64) (locationLine, error) {
	f, err := os.Open(p)
	if err != nil {
		return locationLine{}, err
	}
	defer f.Close()
	var loc locationLine
	for window := int64(1 << 20); window <= maxLocationWindow; window *= 8 {
		start := max(0, size-window)
		buf := make([]byte, size-start)
		if _, err := f.ReadAt(buf, start); err != nil && !errors.Is(err, io.EOF) {
			return locationLine{}, err
		}
		if start > 0 { // the first line is cut: drop it
			if i := bytes.IndexByte(buf, '\n'); i >= 0 {
				buf = buf[i+1:]
			} else {
				buf = nil
			}
		}
		loc = locationLine{}
		for _, line := range bytes.Split(buf, []byte("\n")) {
			if !containsAny(line, locationKeys) {
				continue
			}
			var l locationLine
			if json.Unmarshal(line, &l) != nil {
				continue
			}
			switch {
			case l.Type == "custom-title" && l.CustomTitle != "":
				loc.CustomTitle = l.CustomTitle
			case l.Type == "pr-link" && l.PRURL != "":
				loc.PRURL = l.PRURL
			case l.Cwd != "":
				loc.Cwd, loc.GitBranch = l.Cwd, l.GitBranch
			}
		}
		if loc.Cwd != "" || start == 0 {
			break
		}
	}
	return loc, nil
}

func containsAny(line []byte, keys [][]byte) bool {
	for _, k := range keys {
		if bytes.Contains(line, k) {
			return true
		}
	}
	return false
}
