package cli

import (
	"fmt"
	"slices"
	"strings"

	"github.com/henriquesakata/cerne/internal/hooks"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/refresh"
	"github.com/henriquesakata/cerne/internal/version"
)

// autoRefresh renews the agents whose managed files are all current or ones a
// past release wrote (F-0477), at the points where the out-of-date warning
// shows. It returns the one line that says what it renewed ("" when nothing)
// and the paths of the agents it left alone, which the warning names. It reads
// files and writes only the renewed agents' own; it opens no connection, never
// replaces the binary and connects no agent that was not connected. Off with
// hooks.auto_refresh = false, and fail-open: any problem renews nothing.
func autoRefresh(projDir string) (line string, leftover []string) {
	if !project.AutoRefresh(projDir) {
		return "", nil
	}
	var renewable []hooks.Provider
	for _, p := range refresh.Connected(projDir) {
		// The plugin delivers Claude Code's hooks and guide: absent copies are
		// not stale there, and creating them would run every hook twice.
		if p == hooks.ProviderClaude && fromPlugin() {
			continue
		}
		switch v := refresh.Classify(projDir, p); v.State {
		case refresh.Renewable:
			renewable = append(renewable, p)
		case refresh.Unrecognized:
			leftover = append(leftover, v.Unknown...)
		}
	}
	slices.Sort(leftover)
	leftover = slices.Compact(leftover)
	if len(renewable) == 0 {
		return "", leftover
	}
	hookPart, guidePart := renewAgents(projDir, renewable)
	if hookPart.state == stNotDone || guidePart.state == stNotDone {
		return "", leftover // the warning stays: nothing is claimed
	}
	if hookPart.state == stCurrent && guidePart.state == stCurrent {
		return "", leftover
	}
	names := make([]string, len(renewable))
	for i, p := range renewable {
		names[i] = string(p)
	}
	line = fmt.Sprintf("cerne: renewed %s to %s; hook changes apply from the next session.", strings.Join(names, ", "), version.Version)
	if slices.Contains(renewable, hooks.ProviderCodex) {
		line += " Codex asks to review changed hooks again (/hooks)."
	}
	return line, leftover
}

// leftoverNote says why the leftover paths were not renewed.
func leftoverNote(leftover []string) string {
	return "left as they are, edited in the project or written by a newer cerne: " + strings.Join(leftover, ", ")
}
