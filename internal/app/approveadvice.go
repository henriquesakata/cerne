package app

import (
	"fmt"
	"os"
	"strings"

	"github.com/henriquesakata/cerne/internal/cost"
	"github.com/henriquesakata/cerne/internal/doccheck"
	"github.com/henriquesakata/cerne/internal/doclang"
	"github.com/henriquesakata/cerne/internal/stats"
	"github.com/henriquesakata/cerne/internal/store"
)

// Approve advice codes (F-0390).
const (
	// The Gauntlet cost measured in this project, on a spec naming a bar.
	AdviceGauntletCost = "gauntlet-cost"
	// A light spec naming a bar: the tests may suffice.
	AdviceLightWithBar = "light-with-bar"
	// A standard or advanced spec whose bar is "não aplicável".
	AdviceStandardWithoutBar = "standard-without-bar"
)

// ApproveAdvice is one line of advice the approve check shows (F-0390). It is
// never an impediment: no surface refuses the approve over it.
type ApproveAdvice struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// levelEN names a spec level in the English of the CLI output.
var levelEN = map[string]string{"leve": "light", "padrão": "standard", "avançado": "advanced"}

// ApproveAdvices is the advice for approving feature id (F-0390): what a
// Gauntlet round cost in this project, read off the stamped anchors, on a spec
// naming a bar; on a light spec naming one, that the tests may suffice; on a
// standard or advanced spec with "não aplicável", the justification the human
// accepts. A project in light mode (F-0364) runs no Gauntlet and gets none, as
// does a spec with no valid bar (the bar gate speaks) or a waived one.
//
// Advice never breaks a surface, so it returns no error: an unreadable spec
// gives none, and a failed measurement (a corrupt feature JSON, an unreadable
// measured spec) skips the cost advice while the level advice runs with no
// number. The check and the page then read the same as without advice.
func ApproveAdvices(repoDir, projDir, id string) []ApproveAdvice {
	if !projectGates(projDir).GauntletEnabled() {
		return nil
	}
	spec, err := os.ReadFile(FeatureDocPath(repoDir, id, "spec.md"))
	if err != nil {
		return nil
	}
	if _, err := doccheck.SpecBar(spec); err != nil {
		return nil
	}
	justification, notApplicable := doccheck.BarNotApplicable(spec)
	if !notApplicable && doccheck.BarExempt(spec) {
		return nil
	}
	level, _ := doccheck.SpecLevel(spec)
	if notApplicable && level != "padrão" && level != "avançado" {
		return nil
	}
	st, err := cost.GauntletCostStats(repoDir)
	measured := err == nil
	if !measured {
		st = cost.GauntletStats{} // no number from a failed read
	}
	l := store.DocsLanguage(repoDir)
	notApplicableForm := doclang.Display(doclang.BarLabel, l) + ": " + doclang.Display(doclang.NotApplicable, l)
	if notApplicable {
		took := "not enough measured features yet to show its cost here"
		if p := measuredShare(st, level); p != "" {
			took = "it took " + p + " cost here"
		}
		return []ApproveAdvice{{AdviceStandardWithoutBar, fmt.Sprintf(
			`level %s with "%s": a behaviour or contract change usually goes through the Gauntlet — %s; approving accepts the declared justification: "%s"`,
			levelEN[level], notApplicableForm, took, justification)}}
	}
	var out []ApproveAdvice
	if measured {
		out = append(out, ApproveAdvice{AdviceGauntletCost, gauntletCostMessage(st)})
	}
	if level == "leve" {
		slot := "<por que os testes bastam>"
		if l == doclang.EN {
			slot = "<why the tests suffice>"
		}
		took := "not enough measured features yet to show the Gauntlet's cost here"
		if p := measuredShare(st, level); p != "" {
			took = "the Gauntlet took " + p + " cost here"
		}
		out = append(out, ApproveAdvice{AdviceLightWithBar, fmt.Sprintf(
			`level light with a named bar: when the tests suffice, a light change can use "%s (%s)" — %s`, notApplicableForm, slot, took)})
	}
	return out
}

// gauntletCostMessage is the measured cost overall, then per level with
// enough features; no number below the minimum sample.
func gauntletCostMessage(st cost.GauntletStats) string {
	const head = "Gauntlet cost measured in this project (critic + judge): "
	if !st.Enough {
		return head + fmt.Sprintf("not enough measured features yet (%d of %d needed)", st.Features, cost.MinGauntletSample)
	}
	var b strings.Builder
	b.WriteString(head)
	fmt.Fprintf(&b, "median %s weighted tokens per feature, %s of its cost, over %d delivered features",
		stats.Tokens(st.MedianTokens), percent(st.MedianShare), st.Features)
	for _, level := range []string{"leve", "padrão", "avançado"} {
		if s := st.ByLevel[level]; s.Enough {
			fmt.Fprintf(&b, "; %s: %s tokens, %s (%d features)", levelEN[level], stats.Tokens(s.MedianTokens), percent(s.MedianShare), s.Features)
		}
	}
	return b.String()
}

// measuredShare is the median share at level ("20% of a light feature's"),
// else overall ("25% of a feature's"), else "" — never a number below the
// minimum sample.
func measuredShare(st cost.GauntletStats, level string) string {
	if s := st.ByLevel[level]; s.Enough {
		return percent(s.MedianShare) + " of a " + levelEN[level] + " feature's"
	}
	if st.Enough {
		return percent(st.MedianShare) + " of a feature's"
	}
	return ""
}

func percent(share float64) string { return fmt.Sprintf("%.0f%%", share*100) }
