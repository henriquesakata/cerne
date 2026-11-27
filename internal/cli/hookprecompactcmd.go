package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// Compaction plumbing (F-0368). The PreCompact hook asks the compaction
// summary to keep the session's W- and its latest handoff code verbatim, and
// records the instant so the Stop can remind a handoff after a compaction.
// Local only (cache dir + local backend clone), never a backend write, and
// fail-open: a compaction is never blocked.

// compactionsSubdir holds one file per session, one RFC 3339 instant per
// compaction. Outside sessions/, where every file is read as a binding.
const compactionsSubdir = "compactions"

// compactionMaxAge is how long a recorded compaction stays eligible for the
// one-shot post-compaction resume (F-0525).
const compactionMaxAge = 24 * time.Hour

func newHookPrecompactCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "precompact",
		Short:  "PreCompact plumbing: keep the bound W- and its latest HO- code through the compaction (F-0368)",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			runHookPrecompact(cmd)
			return nil
		},
	}
}

func runHookPrecompact(cmd *cobra.Command) {
	in, err := decodeHookInput(cmd.InOrStdin())
	if err != nil || in.SessionID == "" {
		return
	}
	dir := in.CWD
	if dir == "" {
		dir, _ = os.Getwd()
	}
	cacheDir, repoDir, workID, ok := boundActiveWork(dir, in.SessionID)
	if !ok {
		return
	}
	now := time.Now().UTC()
	recordCompaction(cacheDir, in.SessionID, workID, now)
	if in.cursor() { // F-0525: Cursor's hook host expects JSON stdout, not plain text
		return
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "cerne: this session is bound to work %s. The compaction summary MUST keep these lines verbatim:\n", workID)
	fmt.Fprintf(out, "- work: %s\n", workID)
	switch h, ok, err := latestHandoff(repoDir, workID); {
	case err != nil: // unreadable store: say nothing rather than guess "no handoff"
	case ok:
		fmt.Fprintf(out, "- latest handoff: %s (%s)\n- resume with: cerne show %s\n", h.ID, h.State, h.ID)
	default:
		fmt.Fprintf(out, "- latest handoff: no handoff yet — record one with `cerne work handoff %s --next \"…\"`\n", workID)
	}
}

// boundActiveWork resolves the session's live binding when it is an active
// W-, from the project at dir — all local.
func boundActiveWork(dir, sess string) (cacheDir, repoDir, workID string, ok bool) {
	cfg, projDir, err := project.LoadConfig(dir)
	if err != nil {
		return "", "", "", false
	}
	cacheDir, err = project.CacheDir(cfg.ProjectID)
	if err != nil {
		return "", "", "", false
	}
	repoDir, err = guardedRepoDir(cacheDir, cfg, projDir)
	if err != nil {
		return "", "", "", false
	}
	id, live, err := app.CurrentBinding(cacheDir, repoDir, sess, "") // validates sess as a file name
	if err != nil || !live || !strings.HasPrefix(id, "W-") {
		return "", "", "", false
	}
	return cacheDir, repoDir, id, true
}

// latestHandoff is the most recently created HO- of work id in the local
// clone; err when the store cannot be read (then "none" is unknown).
func latestHandoff(repoDir, workID string) (store.Handoff, bool, error) {
	all, err := store.LoadAllHandoffs(repoDir)
	if err != nil {
		return store.Handoff{}, false, err
	}
	var best store.Handoff
	found := false
	for _, h := range all {
		if h.FromWork == workID && (!found || h.CreatedAt.After(best.CreatedAt) || h.CreatedAt.Equal(best.CreatedAt) && h.ID > best.ID) {
			best, found = h, true
		}
	}
	return best, found, nil
}

// recordCompaction appends "<workID> <instant>" to the session's compaction
// file; best effort. sess was already validated by CurrentBinding.
func recordCompaction(cacheDir, sess, workID string, at time.Time) {
	dir := filepath.Join(cacheDir, compactionsSubdir)
	if os.MkdirAll(dir, 0o755) != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, sess), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(workID + " " + at.Format(time.RFC3339Nano) + "\n")
}

// sessionCompactions counts the session's compactions recorded under workID
// and returns the last instant. A bare-instant line (the first format) counts
// for any binding; unreadable lines are skipped.
// compactionShownPath is the sidecar that records the last compaction instant
// already delivered to the model (F-0525, N-0134).
func compactionShownPath(cacheDir, sess string) string {
	return filepath.Join(cacheDir, compactionsSubdir, sess+".shown")
}

func compactionShownAt(cacheDir, sess string) time.Time {
	b, err := os.ReadFile(compactionShownPath(cacheDir, sess))
	if err != nil {
		return time.Time{}
	}
	ts, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(string(b)))
	if err != nil {
		return time.Time{}
	}
	return ts
}

// markCompactionShown records that the compaction at at was injected (atomic).
func markCompactionShown(cacheDir, sess string, at time.Time) {
	if at.IsZero() {
		return
	}
	dir := filepath.Join(cacheDir, compactionsSubdir)
	if os.MkdirAll(dir, 0o755) != nil {
		return
	}
	tmp, err := os.CreateTemp(dir, sess+".shown-*")
	if err != nil {
		return
	}
	name := tmp.Name()
	if _, err := tmp.WriteString(at.UTC().Format(time.RFC3339Nano) + "\n"); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return
	}
	_ = os.Rename(name, compactionShownPath(cacheDir, sess))
}

// consumePendingCompactionBlock returns the post-compaction resume block when
// the session has an undelivered compaction recorded for its bound work
// (F-0525). It marks the compaction delivered on success.
func consumePendingCompactionBlock(dir, cacheDir, repoDir, sess string, now time.Time) string {
	_, _, workID, ok := boundActiveWork(dir, sess)
	if !ok {
		return ""
	}
	n, last := sessionCompactions(cacheDir, sess, workID)
	if n == 0 || last.IsZero() {
		return ""
	}
	if compactionMaxAge > 0 && now.Sub(last) > compactionMaxAge {
		return ""
	}
	shown := compactionShownAt(cacheDir, sess)
	if !last.After(shown) {
		return ""
	}
	block := compactionResumeText(repoDir, workID)
	if block == "" {
		return ""
	}
	markCompactionShown(cacheDir, sess, last)
	return block
}

// compactionResumeText is the English resume block after compaction, with the
// active work title when still known (F-0525).
func compactionResumeText(repoDir, workID string) string {
	workLine := workID
	if w, err := store.LoadWork(repoDir, workID); err == nil && w.State == domain.WorkActive {
		workLine = workID + " — " + w.Title
	}
	h, ok, err := latestHandoff(repoDir, workID)
	if err != nil { // unreadable store: the W- only, no guess about its HO
		return fmt.Sprintf("AFTER COMPACTION — this session is bound to %s.", workLine)
	}
	if !ok {
		return fmt.Sprintf("AFTER COMPACTION — this session is bound to %s, with no handoff yet: `cerne work handoff %s --next \"…\"` lets the next stretch, or another session, resume by the code.", workLine, workID)
	}
	return fmt.Sprintf("AFTER COMPACTION — this session is bound to %s.\nLatest handoff: %s %s %s — next: %s\nRead it first: cerne show %s",
		workLine, h.ID, h.State, store.ShortAge(time.Since(h.CreatedAt)), app.TruncateBody(h.Next, handoffNextRunes, 4*handoffNextRunes), h.ID)
}

func sessionCompactions(cacheDir, sess, workID string) (int, time.Time) {
	b, err := os.ReadFile(filepath.Join(cacheDir, compactionsSubdir, sess))
	if err != nil {
		return 0, time.Time{}
	}
	n, last := 0, time.Time{}
	for _, l := range strings.Split(string(b), "\n") {
		f := strings.Fields(l)
		if len(f) == 2 && f[0] != workID || len(f) == 0 || len(f) > 2 {
			continue
		}
		if ts, err := time.Parse(time.RFC3339Nano, f[len(f)-1]); err == nil {
			n++
			if ts.After(last) {
				last = ts
			}
		}
	}
	return n, last
}
