package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/index"
	"github.com/henriquesakata/cerne/internal/project"
)

// Prompt notes (B-0020). The UserPromptSubmit hook matches the user's prompt
// against the titles of the active notes and runbooks in the local index and
// hands the agent up to 3 `id — title` lines, never the body, never twice in
// one session (the seen list is shared with the notes-on-edit block). Local
// only — it never pulls or reindexes — and fail-open: any error is silence.

// promptNotesBudget is the hard deadline of the lookup: a prompt is never held.
const promptNotesBudget = 200 * time.Millisecond

// promptTitleMax caps a title in the block, in runes.
const promptTitleMax = 100

// promptNotesMax is the most lines one prompt gets.
const promptNotesMax = 3

func newHookPromptNotesCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "prompt-notes",
		Short:  "UserPromptSubmit plumbing: remind the active notes and runbooks whose titles the prompt matches (B-0020)",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			in, err := decodeHookInput(cmd.InOrStdin())
			// Cursor runs the .claude hooks but discards their context: nothing to
			// print, and no id may be marked as shown (B-0020).
			if err != nil || in.cursor() || in.SessionID == "" || strings.TrimSpace(in.Prompt) == "" {
				return nil
			}
			block := promptNotesBlock(in.CWD, in.SessionID, in.Prompt)
			if block == "" {
				return nil
			}
			var c sessionContext
			c.HookSpecificOutput.HookEventName = "UserPromptSubmit"
			c.HookSpecificOutput.AdditionalContext = block
			_ = json.NewEncoder(cmd.OutOrStdout()).Encode(c)
			return nil
		},
	}
}

// promptNotesBlock is the reminder for prompt in the project at dir, or "".
func promptNotesBlock(dir, sess, prompt string) string {
	cfg, projDir, err := project.LoadConfig(dir)
	if err != nil || !project.PromptNotes(projDir) || !cloneIsOurs(cfg, projDir) {
		return ""
	}
	cacheDir, err := project.CacheDir(cfg.ProjectID)
	if err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), promptNotesBudget)
	defer cancel()
	hits, err := index.PromptTitles(ctx, filepath.Join(cacheDir, "index.db"), prompt, 20)
	if err != nil || ctx.Err() != nil {
		return ""
	}
	// Read both lists (what the edit block showed is not repeated), write only
	// the prompt's own: a reminder the agent may ignore must never silence the
	// more specific on-edit warning.
	seen := app.SeenNotes(cacheDir, sess)
	for id := range app.SeenPromptNotes(cacheDir, sess) {
		seen[id] = true
	}
	var fresh []index.TitleHit
	for _, h := range hits {
		if !seen[h.ID] && len(fresh) < promptNotesMax {
			fresh = append(fresh, h)
		}
	}
	if len(fresh) == 0 {
		return ""
	}
	var ids []string
	for _, h := range fresh {
		ids = append(ids, h.ID)
	}
	// No usable session id: dedup is impossible, and the same lines on every
	// prompt are worse than silence.
	if app.MarkPromptNotesSeen(cacheDir, sess, ids) != nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("cerne: notes that may apply to this prompt (run `cerne show <id>` to read one):")
	for _, h := range fresh {
		fmt.Fprintf(&b, "\n- %s — %s", h.ID, capRunes(flatTitle(h.Title), promptTitleMax))
	}
	return b.String()
}
