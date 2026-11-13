package cost

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/henriquesakata/cerne/internal/gauntlet"
)

// Gauntlet roles by transcript (F-0396): a critic or judge usually runs as a
// general-purpose subagent, so its agentType says nothing; what it read and
// what it answered does.
var (
	// fenceRe is the start fence of the diff in a Gauntlet artifact, alone on
	// its line (a `cat -n` number prefix allowed): source code quoting the
	// fence sits inside quotes and is no diff. TestFenceMatchesTheArtifact pins
	// it to gauntlet.Assemble.
	fenceRe = regexp.MustCompile(`(?m)^[ \t]*(?:\d+[→\t])?=== DIFF \((?:início|start)\) ===[ \t]*$`)
	// critRe is the criteria fence of the same package: the diff fence alone
	// also shows in a pasted diff or a quoted example, and would make a
	// builder a critic for good (F-0396).
	critRe = regexp.MustCompile(`(?m)^[ \t]*(?:\d+[→\t])?=== (?:CRITÉRIOS \(início\)|CRITERIA \(start\)) ===[ \t]*$`)
	// askedA and askedB: the first message asks for the verdict line when it
	// spells out both letters ("VEREDITO: A|B|EMPATE" in a brief is no ask).
	askedA = regexp.MustCompile("(?:VEREDITO|VERDICT):[ \\t]*[*_`]*A(?:[^|\\w]|$)")
	askedB = regexp.MustCompile("(?:VEREDITO|VERDICT):[ \\t]*[*_`]*B(?:[^|\\w]|$)")

	userKey = []byte(`"type":"user"`)
	textKey = []byte(`"type":"text"`)
	diffKey = []byte(`DIFF (`)
)

// roleScan reads one subagent transcript line by line, in the pass readSession
// already makes, and keeps only what classifyRole needs: no line is kept but
// the last reply's.
type roleScan struct {
	seenFirst     bool
	asked, fenced bool
	lastReply     []byte // the last assistant line holding a text block
}

func (s *roleScan) feed(line []byte) {
	switch {
	case bytes.Contains(line, userKey):
		first := !s.seenFirst
		s.seenFirst = true
		if !first && !(bytes.Contains(line, toolResultKey) && bytes.Contains(line, diffKey)) {
			return
		}
		var l authorLine
		if json.Unmarshal(line, &l) != nil {
			return
		}
		for _, b := range blockTexts(l.Message.Content, !first) {
			s.fenced = s.fenced || fenceRe.MatchString(b) && critRe.MatchString(b)
			s.asked = s.asked || first && askedA.MatchString(b) && askedB.MatchString(b)
		}
	case bytes.Contains(line, assistantKey) && bytes.Contains(line, textKey):
		s.lastReply = line
	}
}

// classifyRole is the subagent's role by its transcript: "judge" when its last
// reply ends in the verdict line or its first message asked for it; "critic"
// when its agentType is code-reviewer or the diff fence reached it, in the
// first message or in a tool result; both signals, judge; else "".
func (s *roleScan) classifyRole(kind string) string {
	var l authorLine
	judge := s.asked
	if s.lastReply != nil && json.Unmarshal(s.lastReply, &l) == nil {
		_, ok := gauntlet.VerdictLine(strings.Join(blockTexts(l.Message.Content, false), "\n"))
		judge = judge || ok
	}
	switch {
	case judge:
		return "judge"
	case kind == "code-reviewer" || s.fenced:
		return "critic"
	}
	return ""
}

// blockTexts is the text of a message's content: a string, or the text blocks
// of a list — with results, the tool_result blocks' text instead.
func blockTexts(raw json.RawMessage, results bool) []string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return []string{s}
	}
	var blocks []struct {
		Type    string          `json:"type"`
		Text    string          `json:"text"`
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return nil
	}
	var out []string
	for _, b := range blocks {
		switch {
		case b.Type == "text" && !results:
			out = append(out, b.Text)
		case b.Type == "tool_result" && results:
			out = append(out, blockTexts(b.Content, false)...)
		}
	}
	return out
}
