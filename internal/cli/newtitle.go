package cli

import (
	"errors"
	"strings"

	"github.com/henriquesakata/cerne/internal/doccheck"
)

// errNewTitleRequired is the refusal of a `new` command given neither a title
// argument nor a body that opens with a `# ` heading (B-0018).
var errNewTitleRequired = errors.New("give a title or start the body with a `# ` heading")

// newTitle is the title of a `new` command: the argument when given, trimmed (it always
// wins), else the first H1 of the body — after any frontmatter — from --file or
// stdin. The H1 stays in the stored body: it is the document's own heading, and
// a body written with an explicit title argument keeps it the same way, so the
// projection never shows it twice.
func newTitle(args []string, body string) (string, error) {
	if len(args) > 0 {
		if t := strings.TrimSpace(args[0]); t != "" {
			return t, nil
		}
		return "", errNewTitleRequired // an explicit blank title is no title
	}
	if t := strings.TrimSpace(doccheck.BodyTitle([]byte(body))); t != "" {
		return t, nil
	}
	return "", errNewTitleRequired
}
