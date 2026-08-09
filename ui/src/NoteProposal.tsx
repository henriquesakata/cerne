import type { ParsedProposal } from './BacklogChat'
import Markdown from './Markdown'
import QuoteComment from './QuoteComment'
import { useT } from './i18n'

// NoteProposal (F-0249) é o card de confirmação do bloco ```cerne-nota: o
// agente propõe conhecimento durável, o usuário confirma, o servidor grava.
// Mesmo gate do registro de candidata (F-0223/F-0224) — o agente é read-only,
// quem escreve é o clique.
//
// Fica inline no scroll, e não no painel do rascunho: a nota é episódica
// (nasce de um trecho da conversa) e não tem revisões versionadas como a spec.
//
// O corpo é read-only pelo mesmo motivo da proposta: ajuste volta pelo chat —
// comentar um trecho (QuoteComment, F-0250) devolve o pedido ao agente, que
// reemite o bloco corrigido. Editar aqui tiraria do agente a autoria e do
// histórico o motivo da mudança.

type Props = {
  note: ParsedProposal
  registering: boolean
  // createdId: a nota já foi gravada nesta conversa — o card vira recibo.
  createdId?: string
  onRegister: (note: ParsedProposal) => void
  onQuote: (quote: string) => void
}

export default function NoteProposal({ note, registering, createdId, onRegister, onQuote }: Props) {
  const { t } = useT()

  if (createdId) {
    return (
      <section className="note-proposal note-proposal-created">
        <p className="status">{t.noteProposal.created(createdId)}</p>
      </section>
    )
  }

  return (
    <section className="note-proposal">
      <header className="note-proposal-head">
        <h4>{t.noteProposal.title}</h4>
      </header>
      <QuoteComment className="note-proposal-body" onQuote={onQuote}>
        {(note.facet || note.components.length > 0 || note.informs.length > 0) && (
          <div className="backlog-chat-proposal-chips">
            {note.facet && <span className="chip">{note.facet}</span>}
            {note.components.map((c) => (
              <span className="chip" key={c}>
                {c}
              </span>
            ))}
            {note.informs.map((id) => (
              <span className="chip chip-revision" key={id}>
                {id}
              </span>
            ))}
          </div>
        )}
        <h5 className="note-proposal-title">{note.title}</h5>
        <Markdown>{note.body}</Markdown>
      </QuoteComment>
      <button
        type="button"
        className="note-proposal-register"
        onClick={() => onRegister(note)}
        disabled={registering || note.title.trim() === '' || note.body.trim() === ''}
      >
        {registering ? t.noteProposal.registering : t.noteProposal.register}
      </button>
    </section>
  )
}
