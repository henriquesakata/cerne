package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gitlab.com/rockt/cerne/internal/clarify"
)

// ErrOpenClarifications is the sentinel for an approve attempted over a spec
// that still carries open [ESCLARECER] markers (F-0175). Like the human gate
// (F-0131), it is enforced at the CLI: the UI approve is a human looking at
// the doc with the markers rendered as chips, and answering them there is the
// designed path.
var ErrOpenClarifications = errors.New("open clarifications in the spec")

// CheckOpenClarifications refuses when the feature's spec has unresolved
// [ESCLARECER] markers, listing the pending questions. A missing spec passes —
// doc presence is the document gate's concern, not this one's.
func CheckOpenClarifications(repoDir, id string) error {
	data, err := os.ReadFile(filepath.Join(repoDir, "content", "features", id, "spec.md"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	open := clarify.Open(string(data))
	if len(open) == 0 {
		return nil
	}
	var b strings.Builder
	for _, c := range open {
		q := c.Question
		if q == "" {
			q = c.Raw
		}
		fmt.Fprintf(&b, "\n  - %s", q)
	}
	return fmt.Errorf("%w — %d pending question(s):%s\nanswer them with 'cerne clarify list %s' + 'cerne clarify answer %s <n> --answer \"...\"', in the UI (each marker is a clickable chip), or re-run with --force", ErrOpenClarifications, len(open), b.String(), id, id)
}
