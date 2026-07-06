import { useEffect, useState } from 'react'
import { fetchWork, type Work } from './api'
import { useLiveVersion } from './live'
import { useT } from './i18n'

// Aba Atividade (F-0130): o work-in-progress compartilhado dos agentes,
// somente leitura — a escrita é ato do agente via CLI (cerne work start/done).

// Quantas atividades encerradas mostrar (as mais recentes).
const RECENT_FINISHED = 20

function relTime(iso: string, lang: string): string {
  const then = new Date(iso).getTime()
  const mins = Math.round((Date.now() - then) / 60000)
  const rtf = new Intl.RelativeTimeFormat(lang, { numeric: 'auto' })
  if (mins < 60) return rtf.format(-mins, 'minute')
  if (mins < 60 * 24) return rtf.format(-Math.round(mins / 60), 'hour')
  return rtf.format(-Math.round(mins / (60 * 24)), 'day')
}

// refHref aponta um id referenciado para a sua rota de detalhe.
function refHref(id: string): string {
  if (id.startsWith('F-')) return `#/f/${encodeURIComponent(id)}`
  if (id.startsWith('D-')) return `#/d/${encodeURIComponent(id)}`
  return '#/'
}

export default function Activity() {
  const [works, setWorks] = useState<Work[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const version = useLiveVersion()
  const { t, lang } = useT()

  useEffect(() => {
    fetchWork()
      .then((w) => {
        setWorks(w)
        setError(null)
      })
      .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)))
  }, [version])

  if (error) return <p className="status error">{t.activity.loadError(error)}</p>
  if (!works) return <p className="status">{t.common.loading}</p>

  const active = works
    .filter((w) => w.state === 'active')
    .sort((a, b) => (a.created_at < b.created_at ? 1 : -1))
  const finished = works
    .filter((w) => w.state !== 'active')
    .sort((a, b) => (a.updated_at < b.updated_at ? 1 : -1))
    .slice(0, RECENT_FINISHED)

  return (
    <main className="page">
      <h1>{t.activity.title}</h1>
      <p className="counts">{t.activity.subtitle}</p>

      <section className="entity-group">
        <h2>
          {t.activity.activeGroup}{' '}
          <span className="column-count">{active.length}</span>
        </h2>
        {active.length === 0 && <p className="muted">{t.activity.emptyActive}</p>}
        <ul className="entity-list">
          {active.map((w) => (
            <WorkRow key={w.id} w={w} lang={lang} />
          ))}
        </ul>
      </section>

      {finished.length > 0 && (
        <section className="entity-group">
          <h2>{t.activity.finishedGroup}</h2>
          <ul className="entity-list">
            {finished.map((w) => (
              <WorkRow key={w.id} w={w} lang={lang} />
            ))}
          </ul>
        </section>
      )}
    </main>
  )
}

function WorkRow({ w, lang }: { w: Work; lang: string }) {
  const { t } = useT()
  const chipClass =
    w.state === 'active'
      ? 'chip chip-active'
      : w.state === 'done'
        ? 'chip chip-done'
        : 'chip chip-obsolete'
  return (
    <li>
      <div className="entity-row work-row">
        <span className="card-id">{w.id}</span>
        <span className={chipClass}>{t.activity.stateLabels[w.state]}</span>
        <span className="entity-title">
          {w.title}
          {w.note && <span className="muted"> — {w.note}</span>}
        </span>
        <span className="work-refs">
          {(w.refs ?? []).map((r) => (
            <a key={r} className="work-ref" href={refHref(r)}>
              {r}
            </a>
          ))}
        </span>
        <span className="work-actor" title={w.created_at}>
          {w.actor} · {relTime(w.state === 'active' ? w.created_at : w.updated_at, lang)}
        </span>
      </div>
    </li>
  )
}
