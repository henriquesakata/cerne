package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/app"
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
	cacheDir, repoDir, workID, ok := boundActiveWork(in.CWD, in.SessionID)
	if !ok {
		return
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "cerne: this session is bound to work %s. The compaction summary MUST keep these lines verbatim:\n", workID)
	fmt.Fprintf(out, "- work: %s\n", workID)
	if h, ok := latestHandoff(repoDir, workID); ok {
		fmt.Fprintf(out, "- latest handoff: %s (%s)\n- resume with: cerne show %s\n", h.ID, h.State, h.ID)
	} else {
		fmt.Fprintf(out, "- latest handoff: no handoff yet — record one with `cerne work handoff %s --next \"…\"`\n", workID)
	}
	recordCompaction(cacheDir, in.SessionID, time.Now().UTC())
}

// boundActiveWork resolves the session's live binding when it is an active
// W-, from the project at dir — all local.
func boundActiveWork(dir, sess string) (cacheDir, repoDir, workID string, ok bool) {
	cfg, _, err := project.LoadConfig(dir)
	if err != nil {
		return "", "", "", false
	}
	cacheDir, err = project.CacheDir(cfg.ProjectID)
	if err != nil {
		return "", "", "", false
	}
	repoDir = filepath.Join(cacheDir, "repo")
	id, live, err := app.CurrentBinding(cacheDir, repoDir, sess) // validates sess as a file name
	if err != nil || !live || !strings.HasPrefix(id, "W-") {
		return "", "", "", false
	}
	return cacheDir, repoDir, id, true
}

// latestHandoff is the most recently created HO- of work id in the local clone.
func latestHandoff(repoDir, workID string) (store.Handoff, bool) {
	all, err := store.LoadAllHandoffs(repoDir)
	if err != nil {
		return store.Handoff{}, false
	}
	var best store.Handoff
	found := false
	for _, h := range all {
		if h.FromWork == workID && (!found || h.CreatedAt.After(best.CreatedAt) || h.CreatedAt.Equal(best.CreatedAt) && h.ID > best.ID) {
			best, found = h, true
		}
	}
	return best, found
}

// recordCompaction appends at to the session's compaction file; best effort.
// sess was already validated by CurrentBinding.
func recordCompaction(cacheDir, sess string, at time.Time) {
	dir := filepath.Join(cacheDir, compactionsSubdir)
	if os.MkdirAll(dir, 0o755) != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, sess), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(at.Format(time.RFC3339Nano) + "\n")
}

// sessionCompactions counts the session's recorded compactions and returns
// the last instant; unreadable lines are skipped.
func sessionCompactions(cacheDir, sess string) (int, time.Time) {
	b, err := os.ReadFile(filepath.Join(cacheDir, compactionsSubdir, sess))
	if err != nil {
		return 0, time.Time{}
	}
	n, last := 0, time.Time{}
	for _, l := range strings.Fields(string(b)) {
		if ts, err := time.Parse(time.RFC3339Nano, l); err == nil {
			n++
			if ts.After(last) {
				last = ts
			}
		}
	}
	return n, last
}
