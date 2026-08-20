package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ErrSpecMissingBar is the sentinel for an approve attempted over a spec that
// names no Gauntlet bar (F-0545). Like the clarification gate (F-0175), it is
// enforced at the CLI approve path and is deliberately NOT bypassed by --force:
// a silent absence of a bar is the exact failure this gate exists to stop.
var ErrSpecMissingBar = errors.New("spec sem barra do Gauntlet")

// The three literal forms a spec may carry (cerne-authoring skill, F-0270).
// Accent alternations tolerate NFC and NFD (darwin paste), mirroring the
// lint-side mention matching.
var (
	htmlCommentRe = regexp.MustCompile(`(?s)<!--.*?-->`)
	barLineRe     = regexp.MustCompile(`(?m)^\s*Barra:\s*(.*)$`)
	naoAplicavelRe = regexp.MustCompile(`(?i)^n(?:ã|a\x{0303})o\s+aplic(?:á|a\x{0301})vel\s*\(\s*[^)]+\)`)
	dispensadaRe   = regexp.MustCompile(`(?i)^dispensada\s+por\s+decis(?:ã|a\x{0303})o\s+humana\s*\(\s*[^)]+\)`)
	naoAplicavelPrefixRe = regexp.MustCompile(`(?i)^n(?:ã|a\x{0303})o\s+aplic(?:á|a\x{0301})vel`)
	dispensadaPrefixRe   = regexp.MustCompile(`(?i)^dispensada`)
)

const barForms = `a spec precisa de uma das três formas literais (seção ## Verificação):
  Barra: <referência nomeada + acesso (URL, comando ou arquivo que um crítico abre)>
  Barra: não aplicável (<justificativa de 1 linha, confirmada pelo humano>) — padrão alternativo: <teste/benchmark/invariante>
  Barra: dispensada por decisão humana (<citação curta da fala que dispensou>)
Protocolo Gauntlet: skill cerne-authoring — a barra é da mesma classe do que se entrega, e barra proposta pelo agente exige aceite humano explícito.`

// CheckSpecBar refuses when the feature's spec names no Gauntlet bar in one of
// the three literal forms (F-0545 crit. 1). A missing spec passes — doc
// presence is the document gate's concern. Guide comments in the template
// contain the forms as examples, so HTML comments are stripped before scanning.
func CheckSpecBar(repoDir, id string) error {
	data, err := os.ReadFile(filepath.Join(repoDir, "content", "features", id, "spec.md"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	body := htmlCommentRe.ReplaceAllString(string(data), "")
	m := barLineRe.FindStringSubmatch(body)
	if m == nil {
		return fmt.Errorf("%w — nenhuma linha \"Barra:\" encontrada; %s", ErrSpecMissingBar, barForms)
	}
	value := strings.TrimSpace(m[1])
	switch {
	case value == "":
		return fmt.Errorf("%w — \"Barra:\" sem valor; %s", ErrSpecMissingBar, barForms)
	case naoAplicavelPrefixRe.MatchString(value) && !naoAplicavelRe.MatchString(value):
		return fmt.Errorf("%w — \"Barra: não aplicável\" exige justificativa entre parênteses, confirmada pelo humano; %s", ErrSpecMissingBar, barForms)
	case dispensadaPrefixRe.MatchString(value) && !dispensadaRe.MatchString(value):
		return fmt.Errorf("%w — \"Barra: dispensada por decisão humana\" exige a citação da fala entre parênteses; %s", ErrSpecMissingBar, barForms)
	}
	return nil
}
