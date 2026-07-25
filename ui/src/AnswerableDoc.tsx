import { useState } from 'react'

import { answerClarification, correctClarification } from './api'
import Markdown from './Markdown'

// AnswerableDoc renderiza o corpo de uma entidade não-feature (decisão, nota,
// runbook, referência) com os chips [ESCLARECER] RESPONDÍVEIS — o resgate da
// UX da F-0175 nas páginas onde o chip era só leitura (F-0193). O popover é o
// mesmo do Markdown; a persistência vai pelo endpoint genérico ancorado em
// raw+offset, e onSaved refaz o fetch do doc para exibir o rastro resolvido.
export default function AnswerableDoc({
  id,
  docPath,
  content,
  onSaved,
}: {
  id: string
  docPath: string
  content: string
  onSaved: () => void
}) {
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const send = (
    call: typeof answerClarification,
  ): ((raw: string, offset: number, ans: string) => void) => {
    return (raw, offset, ans) => {
      setSaving(true)
      setError(null)
      void call(id, docPath, content, raw, offset, ans).then((res) => {
        setSaving(false)
        if (res.ok) onSaved()
        else setError(res.error)
      })
    }
  }
  const answer = send(answerClarification)
  // F-0197: corrigir é o mesmo fluxo pela rota irmã — o chip resolvido deixa
  // de ser beco sem saída.
  const correct = send(correctClarification)

  return (
    <>
      {error && <p className="form-error">{error}</p>}
      <Markdown
        onAnswer={saving ? undefined : answer}
        onCorrect={saving ? undefined : correct}
      >
        {content}
      </Markdown>
    </>
  )
}
