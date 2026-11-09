package store

import (
	"regexp"
	"strings"
)

// F-0874: the verifiable preconditions a runbook declares per step. The parser
// lives here (not in app) so the lint can read the same lines. A line that
// names a verifiable kind but is malformed, or a manual line with no step, is
// kept with Problem set: the preflight lists it as manual and never runs it.

// Precondition is one declared line.
type Precondition struct {
	Step    string // "" = every step
	Kind    string // "result" | "registro" | "mr" | "manual"
	Args    []string
	Text    string // what the author wrote after the colon
	Problem string // non-empty: the line is malformed (reason); Kind is "manual"
}

var (
	preLineRe   = regexp.MustCompile(`(?i)^\s*(?:[-*+>]\s*)*(?:\*\*)?pr[eé]-?condi[cç][aã]o(?:\*\*)?\s*(?:\(?\s*((?-i:S\d+[a-z]?(?:\.\d+)*))\s*\)?)?\s*:\s*(?:\*\*)?\s*(.+?)\s*$`)
	featureIDRe = regexp.MustCompile(`^F-\d{4,}$`)
	entityIDRe  = regexp.MustCompile(`^[A-Z]+-\d{4,}$`)
	mrProjectRe = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*(?:/[A-Za-z0-9_][A-Za-z0-9_.-]*)+$`)
	mrIIDRe     = regexp.MustCompile(`^[0-9]+$`)
)

// ValidFeatureID: the only shape a result precondition may name (no path).
func ValidFeatureID(id string) bool { return featureIDRe.MatchString(id) }

// ValidEntityID: PREFIX-NNNN.
func ValidEntityID(id string) bool { return entityIDRe.MatchString(id) }

// ValidMRRef: project is group/project (never starts with '-'), iid digits.
func ValidMRRef(project, iid string) bool {
	return mrProjectRe.MatchString(project) && mrIIDRe.MatchString(iid)
}

// ParsePreconditions reads the declared preconditions of a runbook body, in
// order, skipping code fences.
func ParsePreconditions(body string) []Precondition {
	var out []Precondition
	fence := false
	for _, l := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "```") {
			fence = !fence
			continue
		}
		if fence {
			continue
		}
		m := preLineRe.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		p := Precondition{Step: m[1], Text: m[2], Kind: "manual"}
		f := strings.Fields(strings.Trim(m[2], "*"))
		for i := range f {
			f[i] = strings.Trim(f[i], "`.,;")
		}
		bad := func(want string) { p.Problem = "malformed, expected '" + want + "'" }
		switch {
		case len(f) == 0:
		case strings.EqualFold(f[0], "result"):
			if len(f) == 2 && ValidFeatureID(f[1]) {
				p.Kind, p.Args = "result", f[1:]
			} else {
				bad("result F-NNNN")
			}
		case strings.EqualFold(f[0], "registro"):
			if len(f) == 2 && ValidEntityID(f[1]) {
				p.Kind, p.Args = "registro", f[1:]
			} else {
				bad("registro <ID>")
			}
		case strings.EqualFold(f[0], "mr"):
			proj, iid, _ := strings.Cut(strings.Join(f[1:min(2, len(f))], ""), "!")
			if len(f) == 3 && ValidMRRef(proj, iid) {
				p.Kind, p.Args = "mr", f[1:]
			} else {
				bad("mr <group/project>!<n> <state>")
			}
		}
		if p.Step == "" && p.Kind == "manual" && p.Problem == "" {
			p.Problem = "no step (S<n>): a manual line without a step warns on every step"
		}
		out = append(out, p)
	}
	return out
}
