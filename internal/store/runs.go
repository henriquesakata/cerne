package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Runbook execution records (F-0873). A run is a Work (the visibility record,
// N-0154) plus content/runs/<W-id>.json with the steps and what was recorded
// against them. Like content/revisions/ the directory sits outside what lint,
// the materializer and the index read, and holds only metadata and hashes:
// never the content of a piece of evidence.

// RunEvidence is the only thing kept about a piece of evidence: where it was
// and the sha256 of what was there.
type RunEvidence struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// RunEntry is one recorded step: "ok" (with the human's word, quoted) or "pare".
type RunEntry struct {
	Seq        int          `json:"seq"`
	Step       string       `json:"step"`
	State      string       `json:"state"` // "ok" | "pare"
	Word       string       `json:"word,omitempty"`
	Expected   string       `json:"expected,omitempty"`
	Output     string       `json:"output,omitempty"` // the PARE exit, as reported
	By         string       `json:"by,omitempty"`     // a nickname only
	UTC        string       `json:"utc"`
	BRT        string       `json:"brt"`
	OutOfOrder bool         `json:"out_of_order,omitempty"`
	Evidence   *RunEvidence `json:"evidence,omitempty"`
	Drafted    string       `json:"drafted,omitempty"` // errata item id once reported
}

// Run is the persisted execution record of one runbook window.
type Run struct {
	ID       string   `json:"id"` // the W- id
	Runbook  string   `json:"runbook"`
	Window   string   `json:"window"`
	Expected string   `json:"expected_word,omitempty"`
	Steps    []string `json:"steps"`
	// Explicit: Steps came from --passos. Only then is order judged: the body
	// also names steps in unrelated titles. A run written before this field
	// existed has it false, so it is treated as inferred.
	Explicit bool `json:"steps_explicit,omitempty"`
	// State is the run's own life cycle (RunOpen, RunStopped, RunClosed). Empty
	// in a run written before it existed: see Status.
	State   string     `json:"state,omitempty"`
	Entries []RunEntry `json:"entries,omitempty"`
}

// Run life cycle: aberta -> parada (a PARE was recorded) -> concluida (the work
// was closed, done or abandoned). A stopped or closed run takes no more steps
// and there is no reopening.
const (
	RunOpen    = "aberta"
	RunStopped = "parada"
	RunClosed  = "concluida"
)

// Status is the run's state; for a legacy run with none, it is derived from
// the entries (a last PARE = parada). A legacy run whose work was closed reads
// as aberta/parada here: only the work state tells otherwise.
func (r Run) Status() string {
	if r.State != "" {
		return r.State
	}
	if n := len(r.Entries); n > 0 && r.Entries[n-1].State == "pare" {
		return RunStopped
	}
	return RunOpen
}

func runsDir(repoDir string) string { return filepath.Join(repoDir, "content", "runs") }

// SaveRun writes content/runs/<id>.json atomically.
func SaveRun(repoDir string, r Run) error {
	return writeJSONFile(runsDir(repoDir), r.ID+".json", r)
}

// LoadRun reads one run; ErrNotFound when the work has no run record.
func LoadRun(repoDir, id string) (Run, error) {
	var r Run
	data, err := os.ReadFile(filepath.Join(runsDir(repoDir), id+".json"))
	if errors.Is(err, fs.ErrNotExist) {
		return r, fmt.Errorf("%w: run %s", ErrNotFound, id)
	}
	if err != nil {
		return r, err
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return r, err
	}
	return r, nil
}

var (
	stepTokenRe = regexp.MustCompile(`S\d+[a-z]?(?:\.\d+)*`)
	stepRangeRe = regexp.MustCompile(`(?i)\bS(\d+)\s+(?:a|ao|até)\s+S(\d+)\b`)
)

// RunbookSteps lists the steps a runbook body names: every token S<n>[.<m>…]
// outside code fences, as a whole token, with "S3 a S7" expanded to S3..S7. It
// does not rely on headings: in a real runbook the steps may sit inside a block
// heading, have none of their own, or be mentioned in an unrelated title. The
// result is deduplicated and in natural order (S2, S3, S3.1, S10).
func RunbookSteps(body string) []string {
	set := map[string]bool{}
	fence := false
	for _, l := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "```") {
			fence = !fence
			continue
		}
		if fence {
			continue
		}
		for _, m := range stepRangeRe.FindAllStringSubmatch(l, -1) {
			a, _ := strconv.Atoi(m[1])
			b, _ := strconv.Atoi(m[2])
			for n := a; n <= b && b-a <= 200; n++ {
				set["S"+strconv.Itoa(n)] = true
			}
		}
		for _, loc := range stepTokenRe.FindAllStringIndex(l, -1) {
			i, j := loc[0], loc[1]
			if i > 0 && isAlnum(l[i-1]) {
				continue
			}
			if j < len(l) && isAlnum(l[j]) {
				continue
			}
			set[l[i:j]] = true
		}
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	slices.SortFunc(out, CompareSteps)
	return out
}

// CompareSteps orders step ids naturally, part by part, each part as (number,
// optional letter): S3 < S3.1 < S3.10 < S3a < S3a.1 < S3b < S4.
func CompareSteps(a, b string) int {
	pa, pb := strings.Split(a[1:], "."), strings.Split(b[1:], ".")
	for i := 0; i < len(pa) && i < len(pb); i++ {
		x, lx := splitStepPart(pa[i])
		y, ly := splitStepPart(pb[i])
		if x != y {
			return x - y
		}
		if c := strings.Compare(lx, ly); c != 0 {
			return c
		}
	}
	return len(pa) - len(pb)
}

// splitStepPart splits "4b" into (4, "b").
func splitStepPart(p string) (int, string) {
	n := len(p)
	for n > 0 && (p[n-1] < '0' || p[n-1] > '9') {
		n--
	}
	v, _ := strconv.Atoi(p[:n])
	return v, p[n:]
}
