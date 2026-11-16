import { useState } from 'react'
import type { Question } from './BacklogChat'
import { useT } from './i18n'

// QuestionCard (F-0437) draws one ```cerne-pergunta block: a product question,
// an option per button, the analyst's recommendation highlighted, and a free
// text field. It stores nothing: the choice goes out as a normal user turn
// (onAnswer) and the conversation, already kept locally, is the only record —
// `answer` is derived from it by the caller, so a reload shows the card spent
// with the chosen option pressed (or the free text shown).
//
// The turn text is read by the analyst, so it is built in the UI's locale:
// "**<question>** → <choice> (<origin>)".

type Props = {
  q: Question
  index: number
  total: number
  // answer: the user turn that answered this question, when one exists.
  answer?: string
  busy: boolean
  // stale: the question sits in an earlier assistant turn and was never
  // answered — the analyst moved on, so the card is locked.
  stale?: boolean
  onAnswer: (turn: string) => void
}

// chosenOf reads "<choice>" out of "**<question>** → <choice> (<origin>)".
function chosenOf(answer: string, question: string): string {
  const rest = answer.slice(`**${question}** →`.length).trim()
  return rest.replace(/\s*\([^()]*\)$/, '')
}

export default function QuestionCard({ q, index, total, answer, busy, stale, onAnswer }: Props) {
  const { t } = useT()
  const [free, setFree] = useState('')
  const answered = answer !== undefined
  const dead = stale && !answered
  const locked = answered || busy || !!dead
  const chosen = answered ? chosenOf(answer, q.question) : ''
  const matched = q.options.some((o) => o.label === chosen)
  const reply = (choice: string, origin: string) => onAnswer(`**${q.question}** → ${choice} (${origin})`)

  return (
    <section className="question-card" role="group" aria-label={q.question}>
      <header className="question-card-head">
        <h4>
          {t.questionCard.title(index, total)}
          {answered && t.questionCard.answered}
          {dead && t.questionCard.unanswered}
        </h4>
      </header>
      <p className="question-card-question">{q.question}</p>
      <ul className="question-card-options">
        {q.options.map((o) => {
          const isChosen = answered && o.label === chosen
          return (
            <li key={o.label}>
              <button
                type="button"
                className={
                  'question-card-option' +
                  (o.recommended ? ' question-card-option-recommended' : '') +
                  (isChosen ? ' question-card-option-chosen' : '')
                }
                disabled={locked}
                aria-pressed={answered ? isChosen : undefined}
                onClick={() => reply(o.label, o.recommended ? t.questionCard.accepted : t.questionCard.otherOption)}
              >
                <span className="question-card-label">
                  {o.label}
                  {o.recommended && <span className="question-card-badge">{t.questionCard.recommended}</span>}
                </span>
                {o.consequence && <span className="question-card-consequence">{o.consequence}</span>}
              </button>
            </li>
          )
        })}
      </ul>
      {answered && !matched && (
        <p className="question-card-answer">
          <span className="question-card-answer-label">{t.questionCard.yourAnswer}</span>
          {chosen}
        </p>
      )}
      {!answered && !dead && (
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
      )}
    </section>
  )
}
