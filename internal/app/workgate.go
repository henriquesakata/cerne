package app

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// WorkGateCheck decides one Edit/Write tool call under the work gate (F-0151):
// edits are allowed outside the project tree, under the built-in allowlist
// (.docs/, .claude/ — doc projection and harness config are never gated) or
// the project's extra globs, or when the session holds a live registration
// binding. Everything else is denied with an actionable reason. The check is
// fail-open: any internal error reads as allow — a broken cache must never
// lock the user out of their own files. An allow carries a reason only when
// the session cannot be bound at all (an unsafe id, F-0376): the gate is then
// off for it, and the caller says so.
func WorkGateCheck(cacheDir, repoDir, projDir, sess, agent, filePath string, extraAllow []string) (allow bool, reason string) {
	rel, err := filepath.Rel(projDir, filePath)
	if err != nil {
		return true, ""
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return true, "" // outside the project: not ours to gate
	}
	if strings.HasPrefix(rel, ".docs/") || strings.HasPrefix(rel, ".claude/") {
		return true, ""
	}
	for _, g := range extraAllow {
		if globsOverlap(g, rel) {
			return true, ""
		}
	}
	if sess != "" {
		_, ok, err := CurrentBinding(cacheDir, repoDir, sess, agent)
		if errors.Is(err, ErrUnsafeSessionID) {
			return true, fmt.Sprintf("work gate (F-0151) not enforced: %v — this session can never be bound, so its edits pass ungated", err)
		}
		if err != nil || ok {
			return true, "" // bound, or fail-open on a cache error
		}
	}
	return false, "work gate (F-0151): no live cerne registration for this session — run `cerne work start \"<description>\"` (or advance a feature via `cerne feature start F-...`) before editing code, so the board shows who is on what; paths can be allowlisted via [gates] work_gate_allow in .cernerc"
}
