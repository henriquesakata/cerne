package skills

import (
	"strings"
	"testing"
)

// TestCerneAuthoringDocumentsHumanGates pins the canonical authoring skill to
// the F-0131 human-gate rule (crit. 12): under the default human gate, the
// agent writes the spec, asks for approval and stops — it never runs
// `cerne feature approve`/`accept` on its own. It also documents the new
// `done` state and the `stale-ready` lint rule.
func TestCerneAuthoringDocumentsHumanGates(t *testing.T) {
	c := All()[0].Content

	contains := func(crit, sub string) {
		t.Helper()
		if !strings.Contains(c, sub) {
			t.Errorf("%s: skill missing %q", crit, sub)
		}
	}

	// The agent asks for approval and stops — never approves its own spec.
	contains("crit12", "ask for approval and stop")
	contains("crit12", "Do not run `cerne feature approve` yourself")

	// done is documented as the human-accepted successor of implemented
	// ("Ready"), reached only via cerne feature accept.
	contains("crit12", "cerne feature accept")
	contains("crit12", "\"Ready\"")
	contains("crit12", "`done`")

	// The agent never runs accept itself either, under the default gate.
	contains("crit12", "Do not run `cerne feature accept` yourself")

	// [gates] auto is the only case where the agent may run these itself.
	contains("crit12", "[gates]")
	contains("crit12", "\"auto\"")

	// stale-ready is documented as the signal to ask a human to accept.
	contains("crit12", "stale-ready")
}
