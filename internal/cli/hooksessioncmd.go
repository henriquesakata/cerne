package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/doclang"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/hooks"
	"github.com/henriquesakata/cerne/internal/materialize"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/skills"
	"github.com/henriquesakata/cerne/internal/store"
)

// Session context plumbing (F-0174). Emits the canonical registration rules
// plus the LIVE work list in the envelope of the requested provider — the
// SessionStart injection every provider gets, so an agent on Cursor, Codex or
// Gemini starts a session as informed as one on Claude Code. Local-only (the
// work list comes from the backend clone; a missing projection is built from
// that clone too, B-0017) and fail-open: outside a cerne
// project, or on any error loading works, it degrades gracefully.

// cursorContext is Cursor's native sessionStart output envelope.
type cursorContext struct {
	AdditionalContext string `json:"additional_context"`
}

// copilotContext is Copilot's SessionStart/PostToolUse context envelope:
// top-level, not nested under hookSpecificOutput (F-0444).
type copilotContext struct {
	AdditionalContext string `json:"additionalContext"`
}

// sessionContext is the Claude Code-style SessionStart envelope, also
// accepted by Codex (documented identical) and Gemini (Claude-compatible
// hooks contract, `hooks migrate --from-claude`).
type sessionContext struct {
	HookSpecificOutput struct {
		HookEventName     string `json:"hookEventName"`
		AdditionalContext string `json:"additionalContext"`
	} `json:"hookSpecificOutput"`
}

// newHookGauntletRuleCmd prints the GAUNTLET session rule for Claude Code's
// SessionStart script (F-0364): nothing when the project runs in light mode,
// the rule otherwise — including outside a project or on an unreadable
// .cernerc (fail-safe toward the Gauntlet). It never reads stdin, so the
// script's later call still gets the hook payload.
func newHookGauntletRuleCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "gauntlet-rule",
		Short:  "SessionStart plumbing: the GAUNTLET rule, or nothing in light mode (F-0364)",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := os.Getenv("CLAUDE_PROJECT_DIR")
			if dir == "" {
				dir, _ = os.Getwd()
			}
			if cfg, _, err := project.LoadConfig(dir); err == nil && !cfg.Gates.GauntletEnabled() {
				return nil
			}
			fmt.Fprintln(cmd.OutOrStdout(), hooks.GauntletRule)
			return nil
		},
	}
}

// sessionDocsLang is the project's documentation language for the session
// text, or "" when it cannot be resolved (no clone yet, a foreign clone, no
// project.json): the hooks fail open, so the text then stays neutral (F-0475).
func sessionDocsLang(cfg project.Config, projDir string) doclang.Lang {
	cacheDir, err := project.CacheDir(cfg.ProjectID)
	if err != nil {
		return ""
	}
	repoDir, err := guardedRepoDir(cacheDir, cfg, projDir)
	if err != nil {
		return ""
	}
	if _, err := store.LoadProject(repoDir); err != nil {
		return ""
	}
	return store.DocsLanguage(repoDir)
}

// newHookLangRulesCmd prints, for Claude Code's SessionStart script, the
// planning item and the DOCS LANGUAGE line in the project's language
// (F-0475); outside a project, or with the language unresolved, only the
// neutral planning item. It never reads stdin, like gauntlet-rule.
func newHookLangRulesCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "lang-rules",
		Short:  "SessionStart plumbing: the planning item and the DOCS LANGUAGE line (F-0475)",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := os.Getenv("CLAUDE_PROJECT_DIR")
			if dir == "" {
				dir, _ = os.Getwd()
			}
			var l doclang.Lang
			if cfg, projDir, err := project.LoadConfig(dir); err == nil {
				l = sessionDocsLang(cfg, projDir)
			}
			fmt.Fprintln(cmd.OutOrStdout(), hooks.LangRules(l))
			return nil
		},
	}
}

func newHookSessionContextCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:    "session-context",
		Short:  "SessionStart plumbing: registration rules + live work list, per provider envelope (F-0174)",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			runHookSessionContext(cmd, format)
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "claude", "output envelope: claude|codex|cursor|gemini|copilot")
	return cmd
}

// sessionLockWait bounds the wait for the backend lock when SessionStart
// materializes a missing projection (B-0017). The hook never touches the
// network, so this is the only wait it can have.
var sessionLockWait = 3 * time.Second

// materializeHook runs after the projection is built and before it is
// installed; tests use it to move the clone mid-build.
var materializeHook = func(repoDir string) {}

const (
	docsFromCacheNotice = "cerne: .docs/ was missing and was just materialized from the local cache without contacting the backend, so it may be stale — `cerne pull` refreshes it."
	docsMissingNotice   = "cerne: .docs/ is missing and could not be materialized from the local cache — run `cerne pull` before reading .docs/CONTEXT.md."
)

// materializeIfMissing builds .docs/ from the local backend clone AS IT IS —
// no fetch, no reset, so unpushed local state is never discarded — when projDir
// has no .docs/CONTEXT.md (a fresh git worktree: .docs/ is gitignored). It
// returns the one-line notice to inject, "" when nothing is wrong (B-0017).
//
// It builds only when .docs/ does not exist at all: an existing one without
// CONTEXT.md is left alone (unpushed edits, .conflicts/) and only noticed. It
// runs under the backend lock and, for writers that do not take it, discards
// the build if the clone HEAD moved meanwhile. It builds in a git-invisible
// staging directory renamed into place only when complete: an interrupted run
// leaves no .docs/ at all, never one that passes the CONTEXT.md check without
// its manifest. Unlike `cerne pull` it does not touch docs.lock, the search
// index or the merge driver.
func materializeIfMissing(cfg project.Config, projDir string) string {
	docs := filepath.Join(projDir, ".docs")
	present := func() bool {
		_, err := os.Stat(filepath.Join(docs, "CONTEXT.md"))
		return err == nil
	}
	if present() {
		return ""
	}
	if _, err := os.Lstat(docs); err == nil {
		return docsMissingNotice // exists without CONTEXT.md: may hold unpushed edits, never touched
	}
	cacheDir, err := project.CacheDir(cfg.ProjectID)
	if err != nil {
		return docsMissingNotice
	}
	repoDir := filepath.Join(cacheDir, "repo")
	if _, err := os.Stat(filepath.Join(repoDir, ".git")); err != nil || !cloneIsOurs(cfg, projDir) {
		return docsMissingNotice
	}
	err = gitback.WithLockWait(gitback.LockPath(repoDir), sessionLockWait, func() error {
		if present() { // another session finished while we waited
			return nil
		}
		return stageDocs(cfg, projDir, repoDir)
	})
	switch {
	case err == nil:
		if present() {
			return docsFromCacheNotice
		}
		return docsMissingNotice
	case errors.Is(err, gitback.ErrLockTimeout) && present():
		return ""
	default:
		return docsMissingNotice
	}
}

// errCloneMoved reports the clone HEAD changing under a build.
var errCloneMoved = errors.New("the backend clone moved while the projection was built")

// stageDocs materializes the clone into <projDir>/.docs-staging-*/.docs with
// the manifest and session snapshot `cerne pull` writes after its reset (both
// land in <stage>/.docs: the helpers take the parent of .docs), then renames it
// to .docs/. Stale staging dirs from killed runs are swept first (the caller
// holds the lock). The staging dir starts with a `.gitignore` of `*`, which
// hides the dir and the file itself, so a `git add -A` never picks it up.
func stageDocs(cfg project.Config, projDir, repoDir string) error {
	if old, _ := filepath.Glob(filepath.Join(projDir, ".docs-staging-*")); len(old) > 0 {
		for _, d := range old {
			_ = os.RemoveAll(d)
		}
	}
	stage, err := os.MkdirTemp(projDir, ".docs-staging-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err := os.WriteFile(filepath.Join(stage, ".gitignore"), []byte("*\n"), 0o644); err != nil {
		return err
	}
	headBefore, err := gitback.HeadSHA(repoDir)
	if errors.Is(err, gitback.ErrNoCommits) {
		headBefore = ""
	} else if err != nil {
		return err
	}
	manifest, err := materialize.MaterializeAt(repoDir, filepath.Join(stage, ".docs"), cfg.DefaultScope, cfg.OKF, cfg.RecentWorkWindowDays(), time.Now())
	if err != nil {
		return err
	}
	materializeHook(repoDir)
	head, err := gitback.HeadSHA(repoDir)
	if errors.Is(err, gitback.ErrNoCommits) {
		head = ""
	} else if err != nil {
		return err
	}
	if head != headBefore {
		return errCloneMoved
	}
	if err := project.SaveSessionSnapshot(stage, head); err != nil {
		return err
	}
	if err := project.SaveManifest(stage, manifest); err != nil {
		return err
	}
	docs := filepath.Join(projDir, ".docs")
	if _, err := os.Lstat(docs); err == nil {
		return fmt.Errorf(".docs/ appeared while the projection was built")
	}
	if err := os.Rename(filepath.Join(stage, ".docs"), docs); err != nil {
		return err
	}
	_ = app.RecordProjectHead(projDir) // drift baseline, as pull records it
	return nil
}

func runHookSessionContext(cmd *cobra.Command, format string) {
	in, _ := decodeHookInput(cmd.InOrStdin()) // best effort: some providers send no payload
	if in.cursor() {
		format = "cursor" // F-0428: a Cursor payload, whatever the wrapper passed
	}
	dir := in.CWD
	if dir == "" {
		for _, env := range []string{"GEMINI_PROJECT_DIR", "GEMINI_CWD", "CURSOR_PROJECT_DIR", "CLAUDE_PROJECT_DIR"} {
			if v := os.Getenv(env); v != "" {
				dir = v
				break
			}
		}
	}
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return
		}
		dir = wd
	}
	cfg, projDir, err := project.LoadConfig(dir)
	if err != nil {
		return // not a cerne project: never inject
	}

	if in.SessionID != "" {
		if cacheDir, err := project.CacheDir(cfg.ProjectID); err == nil {
			_ = app.TouchBinding(cacheDir, in.SessionID, "") // F-0361: sign of life
		}
	}

	missingDocs := materializeIfMissing(cfg, projDir) // B-0017

	stale := ""
	if format == "copilot" {
		stale = copilotStaleNotice(projDir)
	} else { // F-0477: every agent's session start renews its untouched files
		stale, _ = autoRefresh(projDir)
	}
	ours := cloneIsOurs(cfg, projDir) // F-0434: never list another backend's work
	work := "- none"
	if ours {
		work = activeWorkLines(cfg.ProjectID)
	}
	text := hooks.SessionRules(cfg.Gates.GauntletEnabled(), sessionDocsLang(cfg, projDir)) + "\n\nACTIVE WORK RIGHT NOW (cerne work list):\n" + work
	if stale != "" {
		text += "\n\n" + stale
	}
	if missingDocs != "" {
		text += "\n\n" + missingDocs
	}
	if in.Source == "compact" && ours {
		if block := compactBlock(dir, in.SessionID); block != "" {
			text += "\n\n" + block
			if cacheDir, err := project.CacheDir(cfg.ProjectID); err == nil {
				at := time.Now().UTC()
				if _, _, workID, ok := boundActiveWork(dir, in.SessionID); ok {
					if _, last := sessionCompactions(cacheDir, in.SessionID, workID); !last.IsZero() {
						at = last
					}
				}
				markCompactionShown(cacheDir, in.SessionID, at)
			}
		}
	}
	if ours {
		if block := startBlock(cfg.ProjectID, in.SessionID, true); block != "" {
			text += "\n\n" + block
		}
	}

	enc := json.NewEncoder(cmd.OutOrStdout())
	var payload any
	if format == "cursor" {
		payload = cursorContext{AdditionalContext: text}
	} else if format == "copilot" {
		payload = copilotContext{AdditionalContext: text}
	} else {
		var sc sessionContext
		sc.HookSpecificOutput.HookEventName = "SessionStart"
		sc.HookSpecificOutput.AdditionalContext = text
		payload = sc
	}
	if err := enc.Encode(payload); err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), err)
	}
}

// activeWorkLines renders the live activity roster from the local backend
// clone — "- none" when empty or unreadable (a session must start even when
// the clone is missing).
func activeWorkLines(projectID string) string {
	cacheDir, err := project.CacheDir(projectID)
	if err != nil {
		return "- none"
	}
	works, err := store.LoadAllWorks(filepath.Join(cacheDir, "repo"))
	if err != nil {
		return "- none"
	}
	var lines []string
	for _, w := range works {
		if w.State != domain.WorkActive {
			continue
		}
		lines = append(lines, "- "+w.ID+" — "+w.Title+" ("+w.Actor+")")
	}
	if len(lines) == 0 {
		return "- none"
	}
	return strings.Join(lines, "\n")
}

// handoffNextRunes caps the next step in the compact block, like a channel
// message body.
const handoffNextRunes = 280

// compactBlock is the post-compaction handoff pointer (F-0368 crit. 12) for a
// session bound to an active W-: its latest HO and how to read it, or the one
// line telling how to record one. "" when unbound.
func compactBlock(dir, sess string) string {
	_, repoDir, workID, ok := boundActiveWork(dir, sess)
	if !ok {
		return ""
	}
	h, ok, err := latestHandoff(repoDir, workID)
	if err != nil { // unreadable store: the W- only, no guess about its HO
		return fmt.Sprintf("AFTER COMPACTION — this session is bound to %s.", workID)
	}
	if !ok {
		return fmt.Sprintf("AFTER COMPACTION — this session is bound to %s, with no handoff yet: `cerne work handoff %s --next \"…\"` lets the next stretch, or another session, resume by the code.", workID, workID)
	}
	return fmt.Sprintf("AFTER COMPACTION — this session is bound to %s.\nLatest handoff: %s %s %s — next: %s\nRead it first: cerne show %s",
		workID, h.ID, h.State, store.ShortAge(time.Since(h.CreatedAt)), app.TruncateBody(h.Next, handoffNextRunes, 4*handoffNextRunes), h.ID)
}

// copilotStaleNotice is the F-0405 session-start check over the Copilot files:
// only the Copilot entries of the shared stale lists, so a Copilot-only
// project is not told about the Claude Code skills it never installed.
func copilotStaleNotice(projDir string) string {
	line, leftover := autoRefresh(projDir)
	var names []string
	for _, n := range append(hooks.Stale(projDir), skills.Stale(projDir)...) {
		if strings.Contains(n, "copilot") {
			names = append(names, n)
		}
	}
	notice := ""
	if len(names) > 0 {
		notice = "cerne: managed Copilot file(s) out of date with this cerne build: " + strings.Join(names, ", ") + " — run `cerne update`."
		if len(leftover) > 0 {
			notice += " (" + leftoverNote(leftover) + ")"
		}
	}
	return strings.TrimSpace(line + "\n" + notice)
}
