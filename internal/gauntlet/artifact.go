// Package gauntlet builds the inputs of the Gauntlet protocol's roles. It is
// pure text assembly plus a read-only git diff: it never writes to a backend.
package gauntlet

import (
	"strings"

	"github.com/henriquesakata/cerne/internal/doclang"
)

// Input is everything the critic's bundle is made of.
type Input struct {
	Lang  doclang.Lang
	Spec  string // spec.md, or "" when absent
	Plan  string // plan.md, or "" when absent
	Notes string // the builder's declared deviations, optional
	Diff  string
}

type fences struct{ critStart, critEnd, diffStart, diffEnd, notes string }

var fenceText = map[doclang.Lang]fences{
	doclang.PT: {"=== CRITÉRIOS (início) ===", "=== CRITÉRIOS (fim) ===", "=== DIFF (início) ===", "=== DIFF (fim) ===", "Desvios declarados pelo builder"},
	doclang.EN: {"=== CRITERIA (start) ===", "=== CRITERIA (end) ===", "=== DIFF (start) ===", "=== DIFF (end) ===", "Builder's declared deviations"},
}

type section struct{ heading, text string }

// splitSections cuts md at its level-2 headings, ignoring "## " inside code fences.
func splitSections(md string) []section {
	var out []section
	inCode := false
	for _, line := range strings.Split(md, "\n") {
		if doclang.Fence(line) {
			inCode = !inCode
		}
		if h, ok := strings.CutPrefix(line, "## "); ok && !inCode {
			out = append(out, section{heading: strings.TrimSpace(h)})
		}
		if len(out) > 0 {
			out[len(out)-1].text += line + "\n"
		}
	}
	return out
}

// planContracts keeps the constraints section and every section outside the
// plan template: where the tech lead writes binding contracts and adjustments.
func planContracts(plan string) []string {
	template := map[string]bool{}
	for _, l := range doclang.Langs() {
		for _, h := range doclang.Sections(doclang.Plan, l) {
			template[strings.ToLower(h)] = true
		}
	}
	constraints := map[string]bool{}
	for _, l := range doclang.Langs() {
		s := doclang.Sections(doclang.Plan, l)
		constraints[strings.ToLower(s[4])] = true // "Restrições para o agente" / "Constraints for the agent"
	}
	var out []string
	for _, s := range splitSections(plan) {
		h := strings.ToLower(s.heading)
		if constraints[h] || !template[h] {
			out = append(out, strings.TrimSpace(s.text))
		}
	}
	return out
}

// Assemble renders the critic's single-file bundle: the criteria fence (spec
// criteria, plan contracts, builder notes) then the diff fence.
func Assemble(in Input) string {
	f, ok := fenceText[in.Lang]
	if !ok {
		f = fenceText[doclang.PT]
	}
	var parts []string
	for _, s := range splitSections(in.Spec) {
		if doclang.IsCriteriaSection(s.heading) {
			parts = append(parts, strings.TrimSpace(s.text))
		}
	}
	contracts := planContracts(in.Plan)
	if len(contracts) == 0 {
		contracts = []string{"(no plan contracts)"}
	}
	parts = append(parts, contracts...)
	if n := strings.TrimSpace(in.Notes); n != "" {
		parts = append(parts, "## "+f.notes+"\n\n"+n)
	}
	var b strings.Builder
	b.WriteString(f.critStart + "\n" + strings.Join(parts, "\n\n") + "\n" + f.critEnd + "\n\n")
	b.WriteString(f.diffStart + "\n" + strings.TrimRight(in.Diff, "\n") + "\n" + f.diffEnd + "\n")
	return b.String()
}
