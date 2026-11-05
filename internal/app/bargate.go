package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"gitlab.com/rockt/cerne/internal/doccheck"
)

// ErrSpecMissingBar is the sentinel for an approve attempted over a spec that
// names no Gauntlet bar (F-0545). It is enforced in the shared transition core,
// so CLI and UI approve alike (F-0295), and is deliberately NOT bypassed by
// --force: a silent absence of a bar is the exact failure this gate exists to stop.
var ErrSpecMissingBar = errors.New("spec sem barra do Gauntlet")

const barForms = `a spec precisa de uma das três formas literais (seção ## Verificação):
  Barra: <referência nomeada + acesso (URL, comando ou arquivo que um crítico abre)>
  Barra: não aplicável (<justificativa de 1 linha, confirmada pelo humano>) — padrão alternativo: <teste/benchmark/invariante>
  Barra: dispensada por decisão humana (<citação curta da fala que dispensou>)
Protocolo Gauntlet: skill cerne-authoring — a barra é da mesma classe do que se entrega, e barra proposta pelo agente exige aceite humano explícito.`

// CheckSpecBar refuses when the feature's spec names no Gauntlet bar in one of
// the three literal forms (F-0545 crit. 1). A missing spec passes — doc
// presence is the document gate's concern. The predicate is doccheck.SpecBar,
// shared with the lint rule and the approve readiness (F-0308).
func CheckSpecBar(repoDir, id string) error {
	data, err := os.ReadFile(filepath.Join(repoDir, "content", "features", id, "spec.md"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	// A stub spec is the document gate's refusal ("fill in the spec"), not this
	// one's — the bar error only makes sense over a spec someone actually wrote.
	if !doccheck.HasRealContent(data) {
		return nil
	}
	if _, err := doccheck.SpecBar(data); err != nil {
		return fmt.Errorf("%w — %v; %s", ErrSpecMissingBar, err, barForms)
	}
	return nil
}
