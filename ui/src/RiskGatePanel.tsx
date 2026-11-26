import { useEffect, useId, useState } from 'react'
import { decideGateProposal, fetchRiskGate, type GateProposal, type RiskGateView } from './api'
import { useT } from './i18n'

// The periods the panel counts over, in days; thirty by default (F-0518 crit. 10).
const PERIODS = [7, 30, 90]

// RiskGatePanel (F-0518) sits beside the two risk lists on the Settings
// screen: what the gate accepted alone and held in the period, and what came
// back on each side; the calibration's progress; and the open proposals to
// change the lists, which only the owner applies or dismisses here.
export default function RiskGatePanel() {
  const { t } = useT()
  const tr = t.riskGate
  const uid = useId()
  const [days, setDays] = useState(30)
  const [view, setView] = useState<RiskGateView | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [asking, setAsking] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    let live = true
    fetchRiskGate(days).then(
      (v) => {
        if (!live) return
        setView(v)
        setError(null)
      },
      (e: unknown) => {
        if (live) setError(e instanceof Error ? e.message : String(e))
      },
    )
    return () => {
      live = false
    }
  }, [days])
  const decide = (p: GateProposal, decision: 'accepted' | 'dismissed') => {
    if (busy) return
    setBusy(true)
    setAsking(null)
    decideGateProposal(p.id, decision, days).then(
      (v) => {
        setBusy(false)
        setView(v)
        setError(null)
      },
      (e: unknown) => {
        setBusy(false)
        setError(e instanceof Error ? e.message : String(e))
      },
    )
  }
  const change = (p: GateProposal) =>
    p.kind === 'relax' ? tr.relax((p.remove ?? []).join(', '), p.setting) : tr.tighten((p.add ?? []).join(', '), p.setting)
  const c = view?.calibration
  return (
    <section className="risk-gate-panel" aria-labelledby={`${uid}-title`}>
      <h3 id={`${uid}-title`}>{tr.title}</h3>
      <label className="risk-gate-period">
        {tr.period}{' '}
        <select className="settings-select" value={days} onChange={(e) => setDays(Number(e.target.value))}>
          {PERIODS.map((n) => (
            <option key={n} value={n}>
              {tr.days(n)}
            </option>
          ))}
        </select>
      </label>
      {error && (
        <p role="alert" className="form-error">
          {tr.error(error)}
        </p>
      )}
      {view && c && (
        <>
          <dl className="risk-gate-stats">
            <dt>{tr.accepted}</dt>
            <dd>{tr.group(view.stats.accepted.cards, view.stats.accepted.feedback, view.stats.accepted.fixed, view.stats.accepted.reverted)}</dd>
            <dt>{tr.held}</dt>
            <dd>{tr.group(view.stats.held.cards, view.stats.held.feedback, view.stats.held.fixed, view.stats.held.reverted)}</dd>
          </dl>
          <p className="settings-hint">
            {c.target === 0 ? tr.calibrationOff : c.remaining > 0 ? tr.calibrationLeft(c.remaining, c.target) : tr.calibrationDone(c.target)}
            {c.answered > 0 && <> {tr.calibrationMatch(c.answered, c.matched, c.missed)}</>}
          </p>
          <h4>{tr.proposals}</h4>
          {view.proposals.length === 0 ? (
            <p className="settings-hint">{tr.noProposals}</p>
          ) : (
            <ul className="risk-gate-proposals">
              {view.proposals.map((p) => (
                <li key={p.id}>
                  <p>
                    <strong>{change(p)}</strong> ({p.cards.join(', ')})
                  </p>
                  <ul className="risk-gate-signals">
                    {p.signals.map((s) => (
                      <li key={s}>{s}</li>
                    ))}
                  </ul>
                  {asking === p.id ? (
                    <div
                      className="settings-confirm"
                      role="group"
                      aria-labelledby={`${uid}-${p.id}`}
                      onKeyDown={(e) => {
                        if (e.key === 'Escape') setAsking(null)
                      }}
                    >
                      <p id={`${uid}-${p.id}`}>{tr.confirmApply(change(p))}</p>
                      <button type="button" className="settings-confirm-yes" aria-disabled={busy || undefined} onClick={() => decide(p, 'accepted')}>
                        {tr.apply}
                      </button>{' '}
                      <button type="button" className="settings-confirm-no" onClick={() => setAsking(null)}>
                        {tr.cancel}
                      </button>
                    </div>
                  ) : (
                    <p>
                      <button type="button" aria-disabled={busy || undefined} onClick={() => setAsking(p.id)}>
                        {tr.apply}
                      </button>{' '}
                      <button type="button" aria-disabled={busy || undefined} onClick={() => decide(p, 'dismissed')}>
                        {tr.dismiss}
                      </button>
                    </p>
                  )}
                </li>
              ))}
            </ul>
          )}
        </>
      )}
    </section>
  )
}
