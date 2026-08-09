import { useEffect, useMemo, useState } from 'react'
import type { ParsedProposal } from './BacklogChat'
import { lineDiff } from './lineDiff'
import Markdown from './Markdown'
import QuoteComment from './QuoteComment'
import { useT } from './i18n'

// ProposalDraft (F-0255) é o rascunho vivo: a proposta deixa de ser mensagem
// efêmera no scroll e vira o artefato central, ao lado do chat, com as
// revisões versionadas e o diff entre elas. O chat conduz; o painel é o que
// está sendo elaborado.
//
// As revisões chegam DERIVADAS do transcript (ver BacklogChat): toda mensagem
// do agente que parseia como bloco cerne-proposta é uma versão. Por isso não
// há estado novo a persistir — a reidratação da conversa (F-0219/F-0230) já
// devolve as versões (critério 6).
//
// O corpo é read-only por decisão de spec: ajuste só via chat (texto livre ou
// comentário de trecho, F-0250), para o agente seguir sendo o autor da spec e
// o motivo da mudança não se perder num diff mudo.

// versionText é o que o diff compara: título + corpo, porque uma revisão pode
// mudar só o título e ainda assim ser uma revisão.
function versionText(p: ParsedProposal): string {
  return `${p.title}\n\n${p.body}`
}

type Props = {
  revisions: ParsedProposal[]
  registering: boolean
  onRegister: (proposal: ParsedProposal) => void
  onQuote: (quote: string) => void
}

export default function ProposalDraft({ revisions, registering, onRegister, onQuote }: Props) {
  const [selected, setSelected] = useState(revisions.length - 1)
  const [showDiff, setShowDiff] = useState(false)
  const [collapsed, setCollapsed] = useState(false)
  const { t } = useT()

  // Revisão nova chega selecionada: é ela que o usuário acabou de pedir. Sai
  // do modo diff junto — o diff é uma leitura sobre o par anterior, não um
  // estado permanente do painel.
  useEffect(() => {
    setSelected(revisions.length - 1)
    setShowDiff(false)
  }, [revisions.length])

  const index = Math.min(Math.max(selected, 0), revisions.length - 1)
  const current = revisions[index]
  const diff = useMemo(
    () => (index > 0 ? lineDiff(versionText(revisions[index - 1]), versionText(current)) : []),
    [revisions, index, current],
  )

  if (!current) return null

  return (
    <section className="proposal-draft">
      <header className="proposal-draft-head">
        <h3>{t.proposalDraft.title}</h3>
        <button
          type="button"
          className="proposal-draft-collapse"
          aria-expanded={!collapsed}
          onClick={() => setCollapsed((c) => !c)}
        >
          {collapsed ? t.proposalDraft.expand : t.proposalDraft.collapse}
        </button>
      </header>

      {!collapsed && (
        <>
          {revisions.length > 1 && (
            <div className="proposal-draft-versions">
              <div role="tablist" aria-label={t.proposalDraft.versions}>
                {revisions.map((_, i) => (
                  <button
                    key={i}
                    type="button"
                    role="tab"
                    aria-selected={i === index}
                    className="proposal-draft-version"
                    onClick={() => {
                      setSelected(i)
                      setShowDiff(false)
                    }}
                  >
                    {`v${i + 1}`}
                  </button>
                ))}
              </div>
              {index > 0 && (
                <button
                  type="button"
                  className="proposal-draft-difftoggle"
                  aria-pressed={showDiff}
                  onClick={() => setShowDiff((d) => !d)}
                >
                  {t.proposalDraft.diff(index)}
                </button>
              )}
            </div>
          )}

          {/* F-0250 generalizado: comentar trecho funciona no painel pelo
              mesmo wrapper que já servia o card. */}
          <QuoteComment className="proposal-draft-body" onQuote={onQuote}>
            {showDiff && index > 0 ? (
              <pre className="proposal-draft-diff">
                {diff.map((l, i) => (
                  <span key={i} className={`diff-${l.kind}`}>
                    {l.text}
                  </span>
                ))}
              </pre>
            ) : (
              <>
                {(current.facet || current.components.length > 0 || current.capability) && (
                  <div className="backlog-chat-proposal-chips">
                    {current.facet && <span className="chip">{current.facet}</span>}
                    {current.components.map((c) => (
                      <span className="chip" key={c}>
                        {c}
                      </span>
                    ))}
                    {current.capability && <span className="chip chip-capability">{current.capability}</span>}
                  </div>
                )}
                <h4 className="proposal-draft-title">{current.title}</h4>
                <Markdown>{current.body}</Markdown>
              </>
            )}
          </QuoteComment>

          <button
            type="button"
            className="proposal-draft-register"
            onClick={() => onRegister(current)}
            disabled={registering || current.title.trim() === ''}
          >
            {registering ? t.backlogChat.registering : t.backlogChat.register}
          </button>
        </>
      )}
    </section>
  )
}
