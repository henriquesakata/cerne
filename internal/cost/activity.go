package cost

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"
)

// The activities a transcript action is classified into (F-0528 crit. 3).
const (
	actResearch = "pesquisa"
	actSpec     = "escrita de spec"
	actPlan     = "escrita de plano"
	actResult   = "escrita de result"
	actCode     = "código"
	actTest     = "teste/build"
	actDelegate = "delegação"
	actReply    = "resposta"
	actOther    = "outros"
	// actWait marks a call whose result is the person's answer: the pause up
	// to it is waiting, never an activity (F-0528 crit. 8).
	actWait = "espera"
)

// tickType says what a transcript line is for the gap engine (F-0528): the
// pause up to an assistant line is the model generating, up to a tool result
// the tool running, up to a human prompt the person.
type tickType int

const (
	tickAssistant tickType = iota
	tickResult
	tickPrompt
)

// tick is one timed line of a session: an assistant message (once, at its
// earliest line), a tool result or a human prompt. m carries only what
// attribution needs (ts, session, agent, file, sidechain, kind).
type tick struct {
	m        msg
	typ      tickType
	activity string // assistant: its first tool_use's; result: the answered call's
	tool     string // result: the tool_use_id it answers
	human    bool   // prompt: a person wrote it — what a Turn bind's claim reaches back to (crit. 9)
}

var (
	docPathRe = regexp.MustCompile(`features/[A-Z]+-\d+[^/]*/(spec|plan|result)\.md$`)
	docFlagRe = regexp.MustCompile(`--doc[ =](spec|plan|result)\b`)
	prefixRe  = regexp.MustCompile(`^(?:[A-Za-z_][A-Za-z0-9_]*=(?:"[^"]*"|'[^']*'|\S*)|cd\s+\S+\s*&&|rtk\s+proxy)\s*`)
	testCmdRe = regexp.MustCompile(`(?:^|[\s;&|("'])(?:go (?:test|vet|build)|npm (?:run|test|ci)|npx vitest|vitest)\b`) // anywhere in the line: `cd x && …`, `sh -c "npm ci && …"`
	readCmdRe = regexp.MustCompile(`^(?:grep|rg|cat|sed -n|head|tail|ls|find|wc|git (?:log|show|diff|status|blame)|(?:\./bin/)?cerne (?:search|ask|context|next|status|brief|show|tags|guide|lint|cost|note list))\b`)
	// A heredoc's first line names the file it writes: `cat > f <<EOF`,
	// `cat <<EOF > f`, `tee [-a] f <<EOF`. A shell path may hold a variable,
	// so only its base name tells a feature document.
	redirectRe  = regexp.MustCompile(`(?:^|\s)>>?\s*["']?([^\s<>|;&'"]+)`)
	teeRe       = regexp.MustCompile(`\btee\s+(?:-a\s+)?["']?([^\s<>|;&'"]+)`)
	docTargetRe = regexp.MustCompile(`(?:^|/)(spec|plan|result)\.md$`)
	cerneDocRe  = regexp.MustCompile(`^(?:\./bin/)?cerne feature (new\b.*--file|spec\b|amend\b)`)
)

var docActivity = map[string]string{"spec": actSpec, "plan": actPlan, "result": actResult}

// classify names the activity of one tool call. Only file_path, command and
// subagent_type of input are decoded: a Write's content is never read.
func classify(name string, input json.RawMessage) string {
	var in struct {
		FilePath     string `json:"file_path"`
		Command      string `json:"command"`
		SubagentType string `json:"subagent_type"`
	}
	_ = json.Unmarshal(input, &in)
	switch name {
	case "Read", "Grep", "Glob", "WebSearch", "WebFetch", "ToolSearch":
		return actResearch
	case "Agent", "Task":
		if in.SubagentType == "Explore" {
			return actResearch
		}
		return actDelegate
	case "Write", "Edit", "MultiEdit", "NotebookEdit":
		if m := docPathRe.FindStringSubmatch(in.FilePath); m != nil {
			return docActivity[m[1]]
		}
		return actCode
	case "Bash":
		return classifyBash(in.Command)
	case "AskUserQuestion", "ExitPlanMode":
		return actWait
	}
	return actOther
}

func classifyBash(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	for {
		loc := prefixRe.FindStringIndex(cmd)
		if loc == nil {
			break
		}
		cmd = cmd[loc[1]:]
	}
	if act, ok := heredocWrite(cmd); ok {
		return act
	}
	switch {
	case cerneDocRe.MatchString(cmd):
		if m := cerneDocRe.FindStringSubmatch(cmd); strings.HasPrefix(m[1], "amend") {
			if d := docFlagRe.FindStringSubmatch(cmd); d != nil {
				return docActivity[d[1]]
			}
			return actOther
		}
		return actSpec
	case testCmdRe.MatchString(cmd):
		return actTest
	case readCmdRe.MatchString(cmd):
		return actResearch
	}
	return actOther
}

// heredocWrite classifies a command that writes a heredoc into a file, by the
// first line opening one (`set -e` may come before): a feature document by its
// base name, any other file as code; ok is false when no line opens one.
func heredocWrite(cmd string) (string, bool) {
	var first string
	for _, line := range strings.Split(cmd, "\n") {
		if strings.Contains(line, "<<") {
			first = line
			break
		}
	}
	if first == "" {
		return "", false
	}
	m := teeRe.FindStringSubmatch(first)
	if m == nil {
		m = redirectRe.FindStringSubmatch(first)
	}
	if m == nil {
		return "", false
	}
	if d := docTargetRe.FindStringSubmatch(m[1]); d != nil {
		return docActivity[d[1]], true
	}
	return actCode, true
}

// toolUse is the part of an assistant content block the time reader needs.
type toolUse struct {
	Type  string          `json:"type"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// firstActivity classifies the content's first tool_use, recording each call's
// own activity in acts; "" when the content has none.
func firstActivity(content json.RawMessage, acts map[string]string) string {
	if len(content) == 0 || content[0] != '[' {
		return ""
	}
	var blocks []toolUse
	if json.Unmarshal(content, &blocks) != nil {
		return ""
	}
	first := ""
	for _, b := range blocks {
		if b.Type != "tool_use" {
			continue
		}
		a := classify(b.Name, b.Input)
		acts[b.ID] = a
		if first == "" {
			first = a
		}
	}
	return first
}

// resultID is the tool_use_id the user content answers; ok is false when the
// content is a human prompt.
func resultID(content json.RawMessage) (id string, ok bool) {
	if len(content) == 0 || content[0] != '[' {
		return "", false
	}
	var blocks []struct {
		Type string `json:"type"`
		ID   string `json:"tool_use_id"`
	}
	if json.Unmarshal(content, &blocks) != nil {
		return "", false
	}
	for _, b := range blocks {
		if b.Type == "tool_result" {
			return b.ID, true
		}
	}
	return "", false
}

// appendUserTick adds the tick of one user line: a tool result or a prompt.
// A meta line is a prompt too: another session's message or a task
// notification wakes the session as a person does, and dropping it would count
// the idle pause before it as work. Compact-summary lines, the lines a tool
// call produced (a skill's body, mid-turn), other sessions' copies and lines
// without a readable timestamp add nothing.
func appendUserTick(ticks []tick, line []byte, sess, file, kind string) []tick {
	var l rawLine
	if json.Unmarshal(line, &l) != nil || l.Type != "user" || l.IsCompact || (l.SessionID != "" && l.SessionID != sess) {
		return ticks
	}
	ts, err := time.Parse(time.RFC3339Nano, l.Timestamp)
	if err != nil {
		return ticks
	}
	agent := l.AgentID
	if agent == "" && strings.Contains(file, "/subagents/agent-") {
		agent = strings.TrimSuffix(strings.TrimPrefix(file[strings.LastIndex(file, "/")+1:], "agent-"), ".jsonl")
	}
	id, isResult := resultID(l.Message.Content)
	if !isResult && (l.SourceToolUseID != "" || l.SourceToolAssistantUUID != "") {
		return ticks // a skill's body: produced by a call, mid-turn — no prompt
	}
	tk := tick{typ: tickPrompt, human: !l.IsMeta && l.PromptSource != "system", m: msg{ts: ts, session: sess, agent: agent, file: file, sidechain: l.IsSidechain, kind: kind}}
	if isResult {
		tk.typ, tk.tool = tickResult, id
	}
	return append(ticks, tk)
}
