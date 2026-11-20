package cli

import (
	"fmt"
	"io"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/domain"
)

// indexShown caps the notes a binding point lists (E-0005 criterion 9).
const indexShown = 10

// printTaskIndex ends a binding point — feature start, work start --ref,
// handoff take — with the notes that inform task id (rings a to c of the
// package: ten, then the count of the rest; never a body) and the package
// command. A work start or a take passes its W- id: the last line then names
// the created work too, so `… | tail -1` keeps binding (runHookBindWork's
// workCreatedRe). Fail-open: an index it cannot build prints nothing.
func printTaskIndex(w io.Writer, repoDir, id, work string) {
	items, err := app.TaskIndex(repoDir, id)
	if err != nil {
		return
	}
	if len(items) == 0 {
		fmt.Fprintf(w, "No note informs %s by a link, a cited path or a shared component.\n", id)
	} else {
		fmt.Fprintf(w, "Notes that inform %s (by a link, a cited path or a shared component; a body: cerne show <id>):\n", id)
		for _, it := range items[:min(len(items), indexShown)] {
			line := fmt.Sprintf("  [%s] %s  %s", it.Ring, it.ID, it.Title)
			if it.Gist != "" {
				line += " — " + it.Gist
			}
			fmt.Fprintln(w, line)
		}
		if rest := len(items) - indexShown; rest > 0 {
			fmt.Fprintf(w, "  and %d more: cerne context %s\n", rest, id)
		}
	}
	if work != "" {
		fmt.Fprintf(w, "%s created (active) — task package: cerne context %s\n", work, id)
		return
	}
	fmt.Fprintf(w, "Task package: cerne context %s\n", id)
}

// taskRef is the first feature-family id of a work's refs: the task it is for.
func taskRef(refs []string) string {
	for _, r := range refs {
		if domain.IsFeatureID(r) {
			return r
		}
	}
	return ""
}
