package cost

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/henriquesakata/cerne/internal/domain"
)

var (
	// ErrStampNeedsImplemented: the stamp records the closed cost, so it runs
	// after `cerne feature complete` and before the human accept.
	ErrStampNeedsImplemented = errors.New("the cost stamp needs an implemented feature")
	// ErrNothingToStamp: no session bound and no stamped child — a zero stamp
	// would hide the gap in the epic rollup.
	ErrNothingToStamp = errors.New("nothing to stamp: no session bound and no stamped child")
	// ErrStampWouldLower: the durable record never goes down (the snapshot is
	// gone after the first stamp; a re-stamp after pruning would shrink it).
	ErrStampWouldLower = errors.New("the stamp would lower the recorded cost")
)

// Rollup is an epic's descendants share, at every depth: the sum of their
// stamped anchors' own counters, and the descendants stamped and missing (with
// their state).
type Rollup struct {
	Counters Counters `json:"counters"`
	Stamped  []string `json:"stamped"`
	Missing  []string `json:"missing"` // "F-0003 (implemented)"
}

var (
	custoHeadingRe = regexp.MustCompile(`(?m)^##[ \t]+Custo[ \t]*$`)
	nextSectionRe  = regexp.MustCompile(`(?m)^##[ \t]`)
	anchorRe       = regexp.MustCompile(`(?m)^<!-- cerne:cost v1 (\{.*\}) -->$`)
)

// share is an entity's own token cost — and the v1 anchor schema, a type of
// its own so a change to Report never moves the stamped format (C14).
type share struct {
	ID           string              `json:"id"`
	Counters     Counters            `json:"counters"`
	Sidechain    Counters            `json:"sidechain"`
	ByModel      map[string]Counters `json:"by_model"`
	ByRole       map[string]Counters `json:"by_role,omitempty"` // additive (F-0384): absent from older anchors
	Messages     int                 `json:"messages"`
	Sessions     int                 `json:"sessions"`
	First        string              `json:"first"`
	Last         string              `json:"last"`
	Unattributed Counters            `json:"unattributed"`
}

func shareOf(r Report) share {
	return share{ID: r.ID, Counters: r.Counters, Sidechain: r.Sidechain, ByModel: r.ByModel, ByRole: r.ByRole, Messages: r.Messages,
		Sessions: r.Sessions, First: r.First, Last: r.Last, Unattributed: r.Unattributed}
}

// anchor reads the v1 share a stamped result carries: the last anchor line of
// its real "## Custo" section.
func anchor(result string) (share, bool) {
	_, _, raw, ok := custoSection(result)
	if !ok || raw == "" {
		return share{}, false
	}
	var a share
	return a, json.Unmarshal([]byte(raw), &a) == nil
}

// AnchorCounters reads the own and the unattributed counters of result's v1
// cost anchor — the process panel's one reader (F-0321), which shows its
// coverage from the second: false with no real "## Custo" anchor.
func AnchorCounters(result string) (own, unattributed Counters, ok bool) {
	a, ok := anchor(result)
	return a.Counters, a.Unattributed, ok
}

// custoSection locates the real "## Custo" section of result, ignoring fenced
// code blocks (a quoted example must not hijack the stamp, C7): the first such
// heading outside a fence, up to the next "## " heading outside one or the end,
// as [start, end) byte offsets, plus the last anchor line inside it.
func custoSection(result string) (start, end int, anchorJSON string, ok bool) {
	start, end = -1, len(result)
	fence, off := "", 0
	for _, line := range strings.SplitAfter(result, "\n") {
		text := strings.TrimRight(line, "\r\n")
		trimmed := strings.TrimLeft(text, " ")
		switch {
		case fence != "":
			if strings.HasPrefix(trimmed, fence) {
				fence = ""
			}
		case strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~"):
			fence = trimmed[:3]
		case start < 0:
			if custoHeadingRe.MatchString(text) {
				start = off
			}
		case nextSectionRe.MatchString(text):
			return start, off, anchorJSON, true
		default:
			if m := anchorRe.FindStringSubmatch(text); m != nil {
				anchorJSON = m[1]
			}
		}
		off += len(line)
	}
	return start, end, anchorJSON, start >= 0
}

// Stamp writes r's "## Custo" block into result: replacing the real existing
// block in place (custoSection), else appended at the end. The block carries no date, so the same report stamps byte-identically.
// A report with fewer messages than the anchor already recorded is
// ErrStampWouldLower and result comes back unchanged.
func Stamp(result string, r Report) (string, error) {
	if old, ok := anchor(result); ok && r.Messages < old.Messages {
		return result, fmt.Errorf("%w: kept the stamped cost (%d messages), the transcripts now hold fewer (%d)", ErrStampWouldLower, old.Messages, r.Messages)
	}
	blk, err := block(r)
	if err != nil {
		return result, err
	}
	start, end, _, ok := custoSection(result)
	if !ok {
		return strings.TrimRight(result, "\n") + "\n\n" + blk, nil
	}
	if end < len(result) {
		return result[:start] + blk + "\n" + result[end:], nil
	}
	return result[:start] + blk, nil
}

func countersLine(c Counters) string {
	return fmt.Sprintf("input %d · cache_creation %d · cache_read %d · output %d", c.Input, c.CacheCreation, c.CacheRead, c.Output)
}

// block renders the "## Custo" section — document content, so pt-BR like the
// authoring prompts.
func block(r Report) (string, error) {
	anchorJSON, err := json.Marshal(shareOf(r))
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("## Custo\n\n")
	fmt.Fprintf(&b, "Tokens lidos dos transcripts do Claude Code (`cerne cost %s --stamp`), atribuídos pela ligação sessão→entidade vigente em cada mensagem. Sem valor monetário.\n\n", r.ID)
	b.WriteString("| Parte | Tokens | O que é |\n|---|---:|---|\n")
	fmt.Fprintf(&b, "| input | %d | entrada nova, cobrada a preço cheio |\n", r.Counters.Input)
	fmt.Fprintf(&b, "| cache_creation | %d | gravação no cache de prompt |\n", r.Counters.CacheCreation)
	fmt.Fprintf(&b, "| cache_read | %d | leitura do cache de prompt |\n", r.Counters.CacheRead)
	fmt.Fprintf(&b, "| output | %d | gerados pelo modelo |\n\n", r.Counters.Output)
	fmt.Fprintf(&b, "Subagentes (sidechain, já incluídos acima): %s.\n", countersLine(r.Sidechain))
	models := make([]string, 0, len(r.ByModel))
	for m := range r.ByModel {
		models = append(models, m)
	}
	sort.Strings(models)
	for i, m := range models {
		models[i] = m + " (" + countersLine(r.ByModel[m]) + ")"
	}
	if len(models) == 0 {
		models = []string{"—"}
	}
	fmt.Fprintf(&b, "Por modelo: %s.\n", strings.Join(models, "; "))
	if roles := r.Roles(); roles != nil {
		for i, role := range roles {
			roles[i] = role + " (" + countersLine(r.ByRole[role]) + ")"
		}
		fmt.Fprintf(&b, "Por papel: %s.\n", strings.Join(roles, "; "))
	}
	window := "—"
	if r.First != "" {
		window = r.First + " → " + r.Last
	}
	fmt.Fprintf(&b, "Sessões: %d (%d sem transcript, %d recuperada(s) do snapshot) · mensagens: %d · janela: %s.\n",
		r.Sessions, r.SessionsWithoutTranscript, r.SessionsRecovered, r.Messages, window)
	fmt.Fprintf(&b, "Não atribuído (mensagens dessas sessões do ledger fora de qualquer ligação): %s.\n", countersLine(r.Unattributed))
	if lost := r.SessionsWithoutTranscript - r.SessionsRecovered; lost > 0 {
		fmt.Fprintf(&b, "Atenção: %d sessão(ões) sem transcript nem snapshot; total parcial.\n", lost)
	}
	if r.Children != nil && r.Total != nil {
		fmt.Fprintf(&b, "Filhos, em qualquer profundidade (âncoras somadas): %s; carimbados: %s; sem âncora: %s.\n",
			countersLine(r.Children.Counters), listOrNone(r.Children.Stamped), listOrNone(r.Children.Missing))
		fmt.Fprintf(&b, "Total (próprio + filhos): %s.\n", countersLine(*r.Total))
	}
	fmt.Fprintf(&b, "\n<!-- cerne:cost v1 %s -->\n", anchorJSON)
	return b.String(), nil
}

func listOrNone(ids []string) string {
	if len(ids) == 0 {
		return "—"
	}
	return strings.Join(ids, ", ")
}

// DropSnapshot removes id's snapshot once its cost is stamped (the temporary
// file of F-0309's clarification); a missing one is fine.
func DropSnapshot(cacheDir, id string) error {
	err := os.Remove(snapshotPath(cacheDir, id))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// Unstamped reports whether id has a harvested cost snapshot its result never
// received: the snapshot exists and the result carries no anchor (C12 — the
// accept warns, since the stamp needs an implemented feature).
func Unstamped(cacheDir, repoDir, id string) bool {
	if _, err := os.Stat(snapshotPath(cacheDir, id)); err != nil {
		return false
	}
	_, stamped := readAnchor(repoDir, id)
	return !stamped
}

// PrecheckStamp is the precheck of `cerne cost <id> --stamp` (F-0371): the
// stamp needs an implemented feature and something to stamp — a bound session
// whose transcripts still hold messages, or a stamped child. It returns the
// report the stamp writes. `--stamp` and `cerne next <id>` both call it, so the
// next step never names a stamp the command refuses.
func PrecheckStamp(in Inputs, state domain.FeatureState, id string) (Report, error) {
	if state != domain.StateImplemented {
		return Report{}, fmt.Errorf("%w: %s is %s — run 'cerne feature complete %s' first; the stamp records the closed cost, before the human accept",
			ErrStampNeedsImplemented, id, state, id)
	}
	r, err := Compute(in, id)
	if err != nil {
		return Report{}, err
	}
	if r.Sessions == 0 && (r.Children == nil || len(r.Children.Stamped) == 0) {
		return Report{}, fmt.Errorf("%w for %s", ErrNothingToStamp, id)
	}
	return r, nil
}

// Stamped reports whether id's result already carries a cost anchor.
func Stamped(repoDir, id string) bool {
	_, ok := readAnchor(repoDir, id)
	return ok
}

// StampedReport reads result's v1 cost anchor as the Report `cerne cost <id>
// --json` shows when it reads from the stamp (Source "stamp"), the share no
// role covers named "unknown" — the costs screen's reader (F-0391): the
// durable record, never the transcripts. false with no real anchor.
func StampedReport(result string) (Report, bool) {
	a, ok := anchor(result)
	if !ok {
		return Report{}, false
	}
	r := Report{ID: a.ID, Source: "stamp", ByModel: map[string]Counters{}, Sessions: a.Sessions, Unattributed: a.Unattributed}
	r.addShare(&a)
	r.unknownRole()
	return r, true
}
