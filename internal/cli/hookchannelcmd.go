package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/hooks"
	"github.com/henriquesakata/cerne/internal/materialize"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/skills"
	"github.com/henriquesakata/cerne/internal/store"
)

// Channel contact-point plumbing (F-0325), run by the SessionStart and
// SubagentStart scripts with the hook payload on stdin. 100% local (cache dir
// + backend clone, no network) and fail-open like every hook subcommand.

// envSessionRe admits only session ids that are inert when CLAUDE_ENV_FILE is
// sourced by bash.
var envSessionRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

func newHookChannelCmd() *cobra.Command {
	var point string
	cmd := &cobra.Command{
		Use:    "channel",
		Short:  "SessionStart/SubagentStart plumbing: export CERNE_SESSION and deliver pending channel messages (F-0325)",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			runHookChannel(cmd, point)
			return nil
		},
	}
	cmd.Flags().StringVar(&point, "point", "session-start", "contact point: session-start|subagent")
	return cmd
}

// newHookEnvelopeCmd wraps stdin text in the additionalContext envelope of
// --event, built with encoding/json: a shell hook that only has text (the
// SubagentStart script) never hand-escapes JSON, so a control character in a
// channel message cannot break the envelope (critic C1).
func newHookEnvelopeCmd() *cobra.Command {
	var event string
	cmd := &cobra.Command{
		Use:    "envelope",
		Short:  "Hook plumbing: wrap stdin text in a hookSpecificOutput additionalContext envelope (F-0325)",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			text, err := readOptionalBody(cmd, "")
			if err != nil {
				return err
			}
			var c sessionContext
			c.HookSpecificOutput.HookEventName = event
			c.HookSpecificOutput.AdditionalContext = text
			return json.NewEncoder(cmd.OutOrStdout()).Encode(c)
		},
	}
	cmd.Flags().StringVar(&event, "event", "SubagentStart", "hookEventName of the envelope")
	return cmd
}

// boardDeadline bounds the session-start board (F-0409 crit. 3); loadBoard is
// a variable so tests can fail or stall it.
var (
	boardDeadline = 2 * time.Second
	loadBoard     = loadBoardFromClone
)

// loadBoardFromClone renders the in-progress board from the shared clone.
func loadBoardFromClone(repoDir string, recentDays int, now time.Time) (string, error) {
	works, err := store.LoadAllWorks(repoDir)
	if err != nil {
		return "", err
	}
	handoffs, err := store.LoadAllHandoffs(repoDir)
	if err != nil {
		return "", err
	}
	events, err := store.ReadEvents(repoDir)
	if err != nil {
		return "", err
	}
	return materialize.RenderBoard(works, events, handoffs, now, recentDays)
}

// fetchAgeNotice flags a clone last fetched more than 24 h ago (F-0409 crit. 2):
// the board is only as new as the clone. "" when recent or never fetched.
func fetchAgeNotice(repoDir string, now time.Time) string {
	at, ok := gitback.FetchedAt(repoDir)
	if !ok || now.Sub(at) <= 24*time.Hour {
		return ""
	}
	h := int(now.Sub(at).Hours())
	return fmt.Sprintf("cerne: the shared clone was last fetched %d day(s) ago — run `cerne pull` to refresh it.\n", h/24)
}

// sessionBoard is the board of the project's clone, "" on any error or after
// boardDeadline (fail-open, N-0021).
func sessionBoard(projectID string, recentDays int) string {
	cacheDir, err := project.CacheDir(projectID)
	if err != nil {
		return ""
	}
	repoDir := filepath.Join(cacheDir, "repo")
	done := make(chan string, 1)
	go func() {
		defer func() {
			if recover() != nil { // a panic fails open too
				done <- ""
			}
		}()
		out, err := loadBoard(repoDir, recentDays, time.Now())
		if err != nil {
			out = ""
		}
		done <- out
	}()
	select {
	case out := <-done:
		if out != "" {
			out += fetchAgeNotice(repoDir, time.Now())
		}
		return out
	case <-time.After(boardDeadline):
		return ""
	}
}

func runHookChannel(cmd *cobra.Command, point string) {
	in, err := decodeHookInput(cmd.InOrStdin())
	if err != nil {
		return
	}
	// A subagent shares its parent's session id and a context that will be
	// discarded: it peeks, so consuming there never steals the parent's
	// background.
	consume := point == "session-start"
	if consume {
		exportSession(in.SessionID)
	} else if point != "subagent" {
		return
	}
	cfg, projDir, err := project.LoadConfig(in.CWD)
	if err != nil {
		return
	}
	if consume {
		fmt.Fprint(cmd.OutOrStdout(), staleManagedNotice(projDir, cfg.SyncSkills))
	}
	if !cloneIsOurs(cfg, projDir) { // F-0434
		return
	}
	if consume {
		fmt.Fprint(cmd.OutOrStdout(), sessionBoard(cfg.ProjectID, cfg.RecentWorkWindowDays()))
	}
	block := startBlock(cfg.ProjectID, in.SessionID, consume)
	if point == "subagent" {
		if task := sessionTask(cfg.ProjectID, in.SessionID, in.AgentID); task != "" {
			if block != "" {
				task += "\n"
			}
			block = task + block
		}
	}
	fmt.Fprint(cmd.OutOrStdout(), block)
}

// sessionTask is what a subagent learns of its session's task (E-0005
// criterion 10): the task the session is bound to — a feature-family id, or
// the first one a bound work refs — the index of the notes that inform it and
// the package command. "" without such a binding or on any error (fail-open).
func sessionTask(projectID, sess, agent string) string {
	cacheDir, err := project.CacheDir(projectID)
	if err != nil {
		return ""
	}
	repoDir := filepath.Join(cacheDir, "repo")
	bound, ok, err := app.CurrentBinding(cacheDir, repoDir, sess, agent)
	if err != nil || !ok {
		return ""
	}
	id, through := bound, ""
	if w, err := store.LoadWork(repoDir, bound); err == nil {
		id, through = taskRef(w.Refs), " (bound through "+bound+")"
	}
	f, err := store.LoadFeature(repoDir, id)
	if id == "" || err != nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "THIS SESSION'S TASK: %s — %s [%s]%s\n", f.ID, f.Title, f.State, through)
	printTaskIndex(&b, repoDir, f.ID, "")
	return b.String()
}

// staleManagedNotice is the session-start check of the managed assets the
// binary embeds (F-0405): a stale canonical skill is rewritten when the project
// opted into sync_skills; then the automatic refresh (F-0477) renews the agents
// whose files no one edited, and what is left is only flagged — it may have
// been edited or be versioned in the project's git. Local, no network, "" when
// everything matches or on any error (fail-open, N-0021).
func staleManagedNotice(projDir string, syncSkills bool) string {
	var b strings.Builder
	// sync_skills keeps its own path for the guide, ahead of the refresh (F-0477).
	if _, staleSkills := staleManaged(projDir); syncSkills && len(staleSkills) > 0 {
		if _, err := skills.Install(projDir, staleProviders(projDir, staleSkills)...); err == nil {
			fmt.Fprintf(&b, "cerne: updated canonical skill(s) from this cerne build: %s — the new version applies from the next session (or after reloading skills).\n", strings.Join(staleSkills, ", "))
		}
	}
	line, leftover := autoRefresh(projDir)
	if line != "" {
		fmt.Fprintln(&b, line)
	}
	staleHooks, staleSkills := staleManaged(projDir)
	var warns []string
	if len(staleSkills) > 0 && !syncSkills {
		warns = append(warns, fmt.Sprintf("cerne: canonical skill(s) out of date with this cerne build: %s — run `cerne update`.", strings.Join(staleSkills, ", ")))
	}
	if n := len(staleHooks); n > 0 {
		warns = append(warns, fmt.Sprintf("cerne: %d managed hook(s) out of date with this cerne build — run `cerne update` and review the diff.", n))
	}
	if len(warns) > 0 && len(leftover) > 0 { // crit. 2: the warning names what was left and why
		warns[len(warns)-1] += " (" + leftoverNote(leftover) + ")"
	}
	for _, w := range warns {
		fmt.Fprintln(&b, w)
	}
	return b.String()
}

// fromPlugin reports whether this process runs from the Claude Code plugin,
// whose entry script sets CERNE_PLUGIN=1 (F-0454).
func fromPlugin() bool { return os.Getenv("CERNE_PLUGIN") == "1" }

// withoutPluginAbsent drops, under the plugin, the canonical Claude Code
// skills and hook scripts the project does not have at all: the plugin
// delivers them, so they are not stale. A present copy that diverges stays.
func withoutPluginAbsent(projDir string, names []string) []string {
	if !fromPlugin() {
		return names
	}
	absent := map[string]bool{}
	missing := func(p string) bool { _, err := os.Stat(p); return errors.Is(err, fs.ErrNotExist) }
	for _, s := range skills.All() {
		absent[s.Name] = missing(filepath.Join(projDir, ".claude", "skills", s.Name, "SKILL.md"))
	}
	for _, a := range hooks.Assets() {
		absent[a.Script] = missing(filepath.Join(projDir, ".claude", "hooks", a.Script))
	}
	return slices.DeleteFunc(names, func(n string) bool { return absent[n] })
}

// staleProviders is the provider set a sync rewrites: every detected provider,
// plus Copilot when its copies are among the stale names (F-0444).
func staleProviders(projDir string, names []string) []hooks.Provider {
	ps := hooks.DetectProviders(projDir)
	for _, n := range names {
		if !strings.Contains(n, "copilot") {
			continue
		}
		for _, p := range ps {
			if p == hooks.ProviderCopilot {
				return ps
			}
		}
		return append(ps, hooks.ProviderCopilot)
	}
	return ps
}

// channelBlock is session sess's pending channel block for the project, ""
// when there is nothing to deliver or on any error (fail-open, N-0021).
func channelBlock(projectID, sess string, consume bool) string {
	cacheDir, err := project.CacheDir(projectID)
	if err != nil {
		return ""
	}
	block, err := app.ChannelInject(cacheDir, filepath.Join(cacheDir, "repo"), sess, consume, time.Now().UTC())
	if err != nil {
		return ""
	}
	return block
}

// startBlock is channelBlock at a hook's session start, led by the session's
// card notices (F-0504). Only hooks take the notices: a hook tells a subagent
// apart (its peek, an agent_id), while `cerne next` inside a subagent runs
// under the parent's CERNE_SESSION and would hand the parent's notices away.
func startBlock(projectID, sess string, consume bool) string {
	block := channelBlock(projectID, sess, consume)
	if !consume {
		return block
	}
	cacheDir, err := project.CacheDir(projectID)
	if err != nil {
		return block
	}
	return withChannel(app.TakeNotices(cacheDir, sess, time.Now().UTC()), nil, block)
}

// channelThen puts session sess's card notices (F-0504) and pending channel
// block (F-0325) ahead of
// block, consuming it, at a tool call — an edit, a shell command (B-0054) —
// where an addressed message already shown waits for the next turn (F-0490).
// The prompt has its own (promptChannel). Fail-open: any error is no channel.
func channelThen(cacheDir, repoDir, sess, agent, block string) string {
	now := time.Now().UTC()
	ch, err := app.ChannelInjectTool(cacheDir, repoDir, sess, now)
	block = withChannel(ch, err, block)
	if agent != "" { // a subagent carries its parent's session id: the notices are the parent's
		return block
	}
	return withChannel(app.TakeNotices(cacheDir, sess, now), nil, block)
}

// withChannel puts the channel block ch ahead of block; an error is no channel.
func withChannel(ch string, err error, block string) string {
	switch {
	case err != nil || ch == "":
		return block
	case block == "":
		return ch
	}
	return ch + "\n\n" + block
}

// exportSession appends `export CERNE_SESSION=<id>` to CLAUDE_ENV_FILE, which
// Claude Code sources before every Bash call of the session (R-0034): the
// agent's own `cerne msg|next|status` then know which session they run in.
// From the plugin it also exports CERNE_ACTOR=agent, which `cerne hook
// install` puts in settings.json, so the human gates hold (F-0454).
func exportSession(sess string) {
	var lines string
	if envSessionRe.MatchString(sess) {
		lines = "export CERNE_SESSION=" + sess + "\n"
	}
	if fromPlugin() {
		lines += "export CERNE_ACTOR=agent\n"
	}
	path := os.Getenv("CLAUDE_ENV_FILE")
	if path == "" || lines == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprint(f, lines)
}

// printChannelLead opens an agent's text view (next, status --active) with its
// session's pending channel block and a blank line. Only CERNE_SESSION names
// the session — a human's terminal has none, so it never consumes anyone's
// watermark — and any failure prints nothing.
// Known limit (critic C7): a subagent shares its parent's CERNE_SESSION and
// the CLI cannot tell it runs in one, so `next`/`status --active`/`msg read`
// inside a subagent consume the parent's watermark; only the SubagentStart
// hook peeks.
func printChannelLead(out io.Writer, projectID string) {
	if block := channelBlock(projectID, app.CurrentSession(), true); block != "" { // F-0525
		fmt.Fprintln(out, block)
	}
}
