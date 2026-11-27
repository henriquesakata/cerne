package app

import "os"

// CurrentSession resolves the agent session identity for War Room, channel,
// work/handoff and parallel-guard commands (F-0525): CERNE_SESSION when set
// and safe; in a Cursor agent shell (CURSOR_AGENT=1) without CERNE_SESSION,
// CURSOR_CONVERSATION_ID when set and safe; "" otherwise.
//
// Never used for Claude Code transcript readers (docauthor.go:91,
// costcmd.go:93) which must read CERNE_SESSION directly — Cursor transcripts
// do not carry the tool_use/usage schema that cost.AuthorModel expects.
func CurrentSession() string {
	if s := os.Getenv("CERNE_SESSION"); s != "" && safeSessionID(s) == nil {
		return s
	}
	if os.Getenv("CURSOR_AGENT") == "1" {
		if s := os.Getenv("CURSOR_CONVERSATION_ID"); s != "" && safeSessionID(s) == nil {
			return s
		}
	}
	return ""
}
