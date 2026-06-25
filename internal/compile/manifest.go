package compile

import "time"

// ManifestVersion is the schema version of the delegation manifest. Bump only
// for additive, backward-compatible evolutions; consumers (notably F-0074, which
// reuses this as the self-describing delegation contract) pin on it.
const ManifestVersion = 1

// ManifestName is the file name of the delegation manifest, written alongside
// the prompts under the pending directory.
const ManifestName = "manifest.json"

// Manifest is the machine-readable contract emitted by `cerne compile`
// (delegation mode): it declares, per page, where the host agent must write its
// response and how `--ingest` verifies the response belongs to this run. It is
// the single source of the delegation round-trip — the help (F-0074) points at
// it rather than re-describing the convention in prose.
type Manifest struct {
	Version int             `json:"version"`
	RunID   string          `json:"run_id"`
	Entries []ManifestEntry `json:"entries"`
}

// ManifestEntry declares one page's delegation contract: the prompt to process,
// the path the response must be written to, the digest of the sources the prompt
// was built from, and the run id the response must echo to be ingested. Paths are
// relative to the project directory so the manifest is portable.
type ManifestEntry struct {
	Page         string `json:"page"`
	PromptPath   string `json:"prompt_path"`
	ResponsePath string `json:"response_path"`
	SourceHash   string `json:"source_hash"`
	RunID        string `json:"run_id"`
}

// NewRunID builds the run id for a prepare invocation: a compact UTC timestamp
// suffixed by the scope ("all" when no single module is targeted). now is a
// parameter so callers inject the clock and tests stay deterministic. The id is
// echoed back by the agent in the response frontmatter and checked on ingest, so
// a stale page from a previous run is detected deterministically (not by mtime).
func NewRunID(scope string, now time.Time) string {
	if scope == "" {
		scope = "all"
	}
	return now.UTC().Format("20060102T150405Z") + "-" + scope
}
