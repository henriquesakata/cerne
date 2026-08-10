import { useT } from './i18n'

// CommandProposal (F-0260) é o card do bloco ```cerne-comando: o agente ecoa os
// comandos de escrita que pretende rodar e para; o clique devolve o turno a ele
// com a permissão de executar.
//
// Ao contrário dos cards de candidata e de nota, este NÃO grava nada — quem
// escreve é o próprio agente, no turno seguinte. Por isso os comandos aparecem
// literais, um por linha: é exatamente o que vai rodar, e conferir isso é a
// única defesa do usuário contra autorizar algo que não leu.
//
// Não há edição inline pelo mesmo motivo dos outros cards: ajuste volta pelo
// chat, para o agente reemitir o bloco corrigido. Um campo editável aqui viraria
// um terminal — e a fronteira é o allowlist do servidor, não este componente.

type Props = {
  commands: string[]
  // busy: já há um turno em voo; autorizar duas vezes duplicaria a escrita.
  busy: boolean
  onConfirm: () => void
}

export default function CommandProposal({ commands, busy, onConfirm }: Props) {
  const { t } = useT()

  return (
    <section className="command-proposal">
      <header className="command-proposal-head">
        <h4>{t.commandProposal.title}</h4>
      </header>
      <p className="muted">{t.commandProposal.hint}</p>
      <ol className="command-proposal-list">
        {commands.map((c, i) => (
          <li key={i}>
            <code>{c}</code>
          </li>
        ))}
      </ol>
      <button type="button" className="command-proposal-run" onClick={onConfirm} disabled={busy}>
        {t.commandProposal.run}
      </button>
    </section>
  )
}
