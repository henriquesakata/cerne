import { useEffect, useId, useState } from 'react'
import { fetchSettings, postRiskFeedback, putAcceptMode, type Feature, type RiskAnswer, type RiskDecision } from './api'
import { useT } from './i18n'

type Answer = RiskAnswer['answer']

// RiskFeedback (F-0518) is the owner's answer to the risk gate on the
// feature page: "should have asked me" on a card it accepted alone, "did not
// need me" on one it held and the merge accepted, and in calibration the
// question "did this delivery need you?" on a card accepted at the merge.
// Nobody is asked to agree: only disagreement has a button.
export function RiskFeedback({ feature, decision, onAnswered }: { feature: Feature; decision: RiskDecision; onAnswered: () => void }) {
  const { t } = useT()
  const tr = t.riskGate
  const [open, setOpen] = useState<Answer | null>(null)
  const [changing, setChanging] = useState(false)
  const done = feature.state === 'done'
  // What the decision takes: one answer, or both in calibration.
  let offers: Answer[] = []
  // The calibration question is asked only while the gate calibrates.
  if (decision.calibration) offers = done && decision.calibrating ? ['should_have_asked', 'not_needed'] : []
  else if (decision.risk === 'low' && feature.accepted_via === 'risk') offers = ['should_have_asked']
  else if (decision.risk === 'held' && done) offers = ['not_needed']
  if (offers.length === 0) return null
  const label = (a: Answer) =>
    decision.calibration ? (a === 'should_have_asked' ? tr.itDid : tr.itDidNot) : a === 'should_have_asked' ? tr.shouldHaveAsked : tr.notNeeded
  const fb = decision.feedback
  const asking = !fb || changing
  return (
    <section className="risk-feedback" aria-label={decision.calibration ? tr.question : tr.shouldHaveAsked}>
      {fb && !changing && (
        <p className="feature-note">
          {tr.yourAnswer((decision.calibration ? tr.calibrationAnswers : tr.answers)[fb.answer] ?? fb.answer)}
          {fb.reason ? ` — ${fb.reason}` : ''}
          {[...(fb.added_paths ?? []), ...(fb.added_facets ?? [])].length > 0 && (
            <> · {tr.added([...(fb.added_paths ?? []), ...(fb.added_facets ?? [])].join(', '))}</>
          )}{' '}
          <button type="button" className="risk-feedback-change" onClick={() => setChanging(true)}>
            {tr.changeAnswer}
          </button>
        </p>
      )}
      {asking && open === null && (
        <p className="feature-note risk-feedback-ask">
          {decision.calibration && <span>{tr.question} </span>}
          {offers.map((a) => (
            <button key={a} type="button" onClick={() => setOpen(a)}>
              {label(a)}
            </button>
          ))}
        </p>
      )}
      {asking && open !== null && (
        <AnswerForm
          feature={feature}
          decision={decision}
          answer={open}
          title={label(open)}
          onCancel={() => setOpen(null)}
          onSent={() => {
            setOpen(null)
            setChanging(false)
            onAnswered()
          }}
        />
      )}
    </section>
  )
}

// AnswerForm takes the reason and, with should-have-asked, the folders and
// facets the owner marks to need a person from now on.
function AnswerForm({
  feature,
  decision,
  answer,
  title,
  onCancel,
  onSent,
}: {
  feature: Feature
  decision: RiskDecision
  answer: Answer
  title: string
  onCancel: () => void
  onSent: () => void
}) {
  const { t } = useT()
  const tr = t.riskGate
  const uid = useId()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const marks = answer === 'should_have_asked'
  const facets = feature.facets ?? []
  return (
    <form
      className="risk-feedback-form"
      aria-label={title}
      onSubmit={(e) => {
        e.preventDefault()
        if (busy) return
        const data = new FormData(e.currentTarget)
        setBusy(true)
        setError(null)
        postRiskFeedback(feature.id, answer, String(data.get('reason') ?? ''), data.getAll('path').map(String), data.getAll('facet').map(String)).then(
          () => {
            setBusy(false)
            onSent()
          },
          (err: unknown) => {
            setBusy(false)
            setError(err instanceof Error ? err.message : String(err))
          },
        )
      }}
    >
      <p className="risk-feedback-title">{title}</p>
      <label htmlFor={`${uid}-reason`}>{tr.reason}</label>
      <textarea id={`${uid}-reason`} name="reason" rows={2} />
      {marks && (decision.dirs.length > 0 || facets.length > 0) && (
        <>
          <p className="settings-hint">{tr.markTitle}</p>
          {decision.dirs.length > 0 && (
            <fieldset>
              <legend>{tr.folders}</legend>
              {decision.dirs.map((d) => (
                <label key={d} className="risk-feedback-mark">
                  <input type="checkbox" name="path" value={d} /> <code>{d}</code>
                </label>
              ))}
            </fieldset>
          )}
          {facets.length > 0 && (
            <fieldset>
              <legend>{tr.facets}</legend>
              {facets.map((f) => (
                <label key={f} className="risk-feedback-mark">
                  <input type="checkbox" name="facet" value={f} /> {f}
                </label>
              ))}
            </fieldset>
          )}
        </>
      )}
      {error && (
        <p role="alert" className="form-error">
          {tr.error(error)}
        </p>
      )}
      <button type="submit" aria-disabled={busy || undefined}>
        {tr.send}
      </button>{' '}
      <button type="button" onClick={onCancel}>
        {tr.cancel}
      </button>
    </form>
  )
}

// AcceptModeControl (F-0518) is the owner's mark on a card before its
// complete: needs human validation, may pass alone, or the gate decides.
// Shown under the risk gate, or wherever a mark is already set.
export function AcceptModeControl({ feature, onChanged }: { feature: Feature; onChanged: () => void }) {
  const { t } = useT()
  const tr = t.riskGate
  const uid = useId()
  const [riskGate, setRiskGate] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  useEffect(() => {
    let live = true
    fetchSettings().then(
      (v) => live && setRiskGate(v.settings.some((s) => s.id === 'gates.accept' && s.value === 'risk')),
      () => undefined,
    )
    return () => {
      live = false
    }
  }, [])
  if (!riskGate && !feature.accept_mode) return null
  return (
    <p className="feature-note risk-accept-mode">
      <label htmlFor={`${uid}-mode`}>{tr.mode}</label>{' '}
      <select
        id={`${uid}-mode`}
        className="settings-select"
        value={feature.accept_mode ?? 'gate'}
        aria-describedby={`${uid}-hint`}
        aria-disabled={busy || undefined}
        onChange={(e) => {
          if (busy) return
          setBusy(true)
          setError(null)
          putAcceptMode(feature.id, e.target.value as 'human' | 'auto' | 'gate').then(
            () => {
              setBusy(false)
              onChanged()
            },
            (err: unknown) => {
              setBusy(false)
              setError(err instanceof Error ? err.message : String(err))
            },
          )
        }}
      >
        {(['gate', 'human', 'auto'] as const).map((m) => (
          <option key={m} value={m}>
            {tr.modes[m]}
          </option>
        ))}
      </select>{' '}
      <span id={`${uid}-hint`} className="settings-hint">
        {tr.modeHint}
      </span>
      {error && (
        <span role="alert" className="form-error">
          {' '}
          {tr.error(error)}
        </span>
      )}
    </p>
  )
}
