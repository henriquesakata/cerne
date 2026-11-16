import { useState } from 'react'
import type { Question } from './BacklogChat'
import { useT } from './i18n'

// QuestionCard (F-0437) draws one ```cerne-pergunta block: a product question,
// an option per button, the analyst's recommendation highlighted, and a free
// text field. It stores nothing: the choice goes out as a normal user turn
// (onAnswer) and the conversation, already kept locally, is the only record —
// `answer` is derived from it by the caller, so a reload shows the card spent.
//
// The turn text is read by the analyst, so it is built in the UI's locale:
// "**<question>** → <choice> (<origin>)".

type Props = {
  q: Question
  // answer: the user turn that answered this question, when one exists.
  answer?: string
  busy: boolean
  onAnswer: (turn: string) => void
}

export default function QuestionCard({ q, answer, busy, onAnswer }: Props) {
  const { t } = useT()
  const [free, setFree] = useState('')
  const locked = answer !== undefined || busy
  const reply = (choice: string, origin: string) => onAnswer(`**${q.question}** → ${choice} (${origin})`)

  return (
    <section className="question-card" role="group" aria-label={q.question}>
      <header className="question-card-head">
        <h4>{t.questionCard.title}</h4>
      </header>
      <p className="question-card-question">{q.question}</p>
      <ul className="question-card-options">
        {q.options.map((o) => (
          <li key={o.label}>
            <button
              type="button"
              className={'question-card-option' + (o.recommended ? ' question-card-option-recommended' : '')}
              disabled={locked}
              onClick={() => reply(o.label, o.recommended ? t.questionCard.accepted : t.questionCard.otherOption)}
            >
              <span className="question-card-label">
                {o.label}
                {o.recommended && <span className="chip question-card-badge">{t.questionCard.recommended}</span>}
              </span>
              {o.consequence && <span className="question-card-consequence">{o.consequence}</span>}
            </button>
          </li>
        ))}
      </ul>
      <form
        className="question-card-free"
        onSubmit={(e) => {
          e.preventDefault()
          const text = free.trim()
          if (text && !locked) reply(text, t.questionCard.free)
        }}
      >
        <input
          type="text"
          aria-label={t.questionCard.other}
          placeholder={t.questionCard.other}
          value={free}
          disabled={locked}
          onChange={(e) => setFree(e.target.value)}
        />
        <button type="submit" disabled={locked || free.trim() === ''}>
          {t.questionCard.send}
        </button>
      </form>
    </section>
  )
}
