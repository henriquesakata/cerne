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
  // picked: the choice marked in a round not sent yet (B-0023) — shown as
  // chosen, still changeable.
  picked?: string
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

export default function QuestionCard({ q, index, total, answer, picked, busy, stale, onAnswer }: Props) {
  const { t } = useT()
  const [free, setFree] = useState('')
  const answered = answer !== undefined
  const dead = stale && !answered
  const locked = answered || busy || !!dead
  const shown = answer ?? picked
  const chosen = shown !== undefined ? chosenOf(shown, q.question) : ''
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
          const isChosen = shown !== undefined && o.label === chosen
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
                aria-pressed={shown !== undefined ? isChosen : undefined}
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
      {shown !== undefined && !matched && (
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

type RoundProps = {
  questions: Question[]
  // answers[k]: the user turn line that answered card k, when one exists.
  answers: (string | undefined)[]
  busy: boolean
  stale: boolean
  onAnswer: (turn: string) => void
}

// QuestionRound (B-0023) draws the cards of one assistant turn. A round of
// several open cards (F-0443 asks up to 3 per turn) is answered as a whole: a
// click only picks, and "Send answers" sends one turn with one line per
// question once every open card has a pick. A single open card keeps sending
// on click (F-0437).
export function QuestionRound({ questions, answers, busy, stale, onAnswer }: RoundProps) {
  const { t } = useT()
  const [picks, setPicks] = useState<Record<number, string>>({})
  const open = questions.map((_, k) => k).filter((k) => answers[k] === undefined)
  const batch = !stale && open.length > 1
  const count = open.filter((k) => picks[k] !== undefined).length
  return (
    <>
      {questions.map((q, k) => (
        <QuestionCard
          key={k}
          q={q}
          index={k + 1}
          total={questions.length}
          answer={answers[k]}
          picked={batch ? picks[k] : undefined}
          busy={busy}
          stale={stale}
          onAnswer={batch ? (line) => setPicks((p) => ({ ...p, [k]: line })) : onAnswer}
        />
      ))}
      {batch && (
        <button
          type="button"
          className="question-round-send"
          disabled={busy || count < open.length}
          onClick={() => onAnswer(open.map((k) => picks[k]).join('\n'))}
        >
          {t.questionCard.sendAll(count, open.length)}
        </button>
      )}
    </>
  )
}
