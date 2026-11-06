import type { ChatRun } from './api'
import { useT } from './i18n'
import Markdown from './Markdown'

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

// CopyCommands (F-0323 critério 7): num modo sem canal de execução não existe
// card que prometa rodar — os comandos viram um bloco para o usuário copiar e
// rodar no terminal dele (o CodeBlock do Markdown dá o botão de copiar).
export function CopyCommands({ commands }: { commands: string[] }) {
  const { t } = useT()
  return (
    <section className="command-proposal">
      <p className="muted">{t.commandProposal.noExecHint}</p>
      <Markdown>{'```bash\n' + commands.join('\n') + '\n```'}</Markdown>
    </section>
  )
}

// ToolRuns (F-0323 critérios 1/3) lista o que o turno confirmado executou: o
// comando e o resultado estruturado — o id que o cerne devolveu ou o erro
// nomeado. É o que o humano confere contra a prosa do agente.
export function ToolRuns({ runs }: { runs: ChatRun[] }) {
  const { t } = useT()
  return (
    <section className="tool-runs">
      <h4>{t.toolRuns.title}</h4>
      <ul>
        {runs.map((r, i) => (
          <li key={i} className={r.result.ok ? 'tool-run-ok' : 'tool-run-failed'}>
            <code>{['cerne', r.verb, ...(r.args ?? [])].join(' ')}</code>
            <span>
              {r.result.ok
                ? r.result.id
                  ? t.toolRuns.ok(r.result.id)
                  : t.toolRuns.noEntity
                : t.toolRuns.failed(r.result.error?.code ?? '', r.result.error?.message ?? '')}
            </span>
          </li>
        ))}
      </ul>
    </section>
  )
}
