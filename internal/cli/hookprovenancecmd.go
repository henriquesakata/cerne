package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/cost"
	"github.com/henriquesakata/cerne/internal/lint"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// Provenance reminder plumbing (F-0103, reworked by F-0172). The shell asset
// used to remind only on decision gates — but `feature approve` is a HUMAN
// gate (F-0131) the agent never runs, and at `feature complete` the source has
// long gone cold. The hot moment is the successful `cerne push` that just sent
// an authored doc citing an external URL nobody registered: this subcommand
// runs the SAME detector as the unregistered-source lint rule over the local
// authored docs, so the reminder fires exactly when (and only when) the
// backstop would later flag a debt. Fail-open like every hook subcommand.

var (
	pushRe             = regexp.MustCompile(`\bpush\b`)
	frontmatterCerneID = regexp.MustCompile(`(?m)^cerne_id:\s*(\S+)`)
)

// runsGateVerb reports whether the command invokes cerne with a verb that
// records or closes a decision (B-0038): a mention in a grep, heredoc or
// commit message is no call.
func runsGateVerb(command string) bool {
	for _, v := range []string{"feature complete", "decision new", "decision supersede", "decision substituir"} {
		if cost.Runs(command, strings.Fields(v)) {
			return true
		}
	}
	return false
}

func newHookProvenanceCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:    "provenance",
		Short:  "PostToolUse plumbing: provenance reminder on push/decision gates (F-0103/F-0172)",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			runHookProvenance(cmd, format)
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "claude", "output envelope: claude|copilot")
	return cmd
}

func runHookProvenance(cmd *cobra.Command, format string) {
	in, err := decodeHookInput(cmd.InOrStdin())
	if err != nil {
		return
	}
	if in.cursor() {
		format = "cursor" // F-0428
	}
	var ti struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(in.ToolInput, &ti)
	if !strings.Contains(ti.Command, "cerne") {
		return
	}

	// F-0527: dedup between .cursor/hooks.json and .claude/settings.json.
	if in.cursor() {
		if cfg, _, cerr := project.LoadConfig(in.CWD); cerr == nil {
			if cd, cerr := project.CacheDir(cfg.ProjectID); cerr == nil {
				if !claimCursorHookCall(cd, "provenance", in) {
					return
				}
			}
		}
	}

	var msg string
	switch {
	case runsGateVerb(ti.Command):
		msg = "cerne provenance (F-0103): you just recorded/advanced a decision. If an external source (article, RFC, vendor doc, spec) SHAPED it, register it now — without being asked — with: cerne reference new \"<title>\" --url <url> --informs <id>. Register only the useful sources (the ones that moved the outcome); dead-ends never become references."
	case pushRe.MatchString(ti.Command) && strings.Contains(string(in.ToolResponse), "Sent:"):
		msg = orphanPushMessage(in.CWD)
		if msg == "" {
			return
		}
	default:
		return
	}

	if err := writeToolContext(cmd, in, format, msg); err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), err)
	}
}

// orphanPushMessage scans the project's authored docs (feature spec/plan/result
// and decision ADRs under .docs/current/) with the lint rule's own detector and
// returns a reminder naming each entity and orphan URL — or "" when the push
// was provenance-clean, so the reminder never becomes noise.
func orphanPushMessage(cwd string) string {
	cfg, projDir, err := project.LoadConfig(cwd)
	if err != nil {
		return ""
	}
	cacheDir, err := project.CacheDir(cfg.ProjectID)
	if err != nil {
		return ""
	}
	repoDir, err := guardedRepoDir(cacheDir, cfg, projDir)
	if err != nil {
		return ""
	}
	refs, err := store.LoadAllReferences(repoDir)
	if err != nil {
		return ""
	}

	var docs []string
	for _, glob := range []string{
		filepath.Join(projDir, ".docs", "current", "features", "*", "*.md"),
		filepath.Join(projDir, ".docs", "current", "decisions", "*.md"),
	} {
		m, _ := filepath.Glob(glob)
		docs = append(docs, m...)
	}

	orphans := map[string][]string{}
	for _, p := range docs {
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		m := frontmatterCerneID.FindSubmatch(raw)
		if m == nil {
			continue
		}
		entity := string(m[1])
		if urls := lint.OrphanSourceURLs(entity, string(raw), refs, cfg.SourceIgnore()); len(urls) > 0 {
			orphans[entity] = append(orphans[entity], urls...)
		}
	}
	if len(orphans) == 0 {
		return ""
	}

	ids := make([]string, 0, len(orphans))
	for id := range orphans {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var b strings.Builder
	b.WriteString("cerne provenance (F-0172): the docs you just pushed cite external URLs with no registered reference. If a source SHAPED the doc, register it now — `cerne reference new \"<title>\" --url <url> --informs <id>` — only the useful ones; dead-ends never become references (or drop the bare URL). Orphans: ")
	for i, id := range ids {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(id + " → " + strings.Join(orphans[id], ", "))
	}
	return b.String()
}
