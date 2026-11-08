import { useState, type FormEvent } from 'react'
import { fetchPendings, type Pending, type PendingState } from './api'
import { FixBlock } from './Harness'
import { LoadNotice, LoadPending, useLiveLoad } from './liveLoad'
import Markdown from './Markdown'
import Tag from './Tag'
import { useT } from './i18n'

// Tela de pendências (F-0334): a mesma seleção do `cerne pending list`, só
// leitura. O filtro vive no hash (#/pendings?state=…&from=…&component=…), sem
// estado local: um link abre já filtrado e o Voltar do browser desfaz o filtro.
// A busca é uma só, com state=all e os filtros de origem e componente: o estado
// escolhido recorta essa lista aqui, com a igualdade de estado do
// app.ListPendings, e cada botão de estado conta a sua parte (crítica C12).

type Filter = { state: string; from: string; component: string }

const STATES: (PendingState | 'all')[] = ['open', 'resolved', 'dropped', 'promoted', 'all']
const CHIP: Record<Exclude<PendingState, 'open'>, string> = {
  resolved: 'chip-done',
  dropped: 'chip-obsolete',
  promoted: 'chip-implemented',
}

function parseFilter(hash: string): Filter {
  const i = hash.indexOf('?')
  const q = new URLSearchParams(i < 0 ? '' : hash.slice(i + 1))
  return { state: q.get('state') || 'open', from: q.get('from') ?? '', component: q.get('component') ?? '' }
}

function filterHash(f: Filter): string {
  const q = new URLSearchParams({ state: f.state })
  if (f.from) q.set('from', f.from)
  if (f.component) q.set('component', f.component)
  return `#/pendings?${q}`
}

const fHref = (id: string) => `#/f/${encodeURIComponent(id)}`

// slug mirrors Go's slug.Make (internal/slug), by which the server stores and
// matches components: lowercase, its diacritics table, spaces to "-", and
// nothing outside [a-z0-9-]. The active chip compares the same way (C7).
const FOLD_FROM = 'áàâãäéèêëíìîïóòôõöúùûüçñ'
const FOLD_TO = 'aaaaaeeeeiiiiooooouuuucn'
function slug(s: string): string {
  return Array.from(s.toLowerCase().replace(/ /g, '-'), (c) => FOLD_TO[FOLD_FROM.indexOf(c)] ?? c)
    .join('')
    .replace(/[^a-z0-9-]/g, '')
}

export default function Pendings({ hash }: { hash: string }) {
  const { t } = useT()
  const f = parseFilter(hash)
  const load = useLiveLoad(() => fetchPendings({ state: 'all', from: f.from, component: f.component }), [f.from, f.component])
  const ofState = (s: string) => (load.data ?? []).filter((p) => s === 'all' || p.state === s)
  const shown = ofState(f.state)
  const go = (part: Partial<Filter>) => {
    window.location.hash = filterHash({ ...f, ...part })
  }
  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    const d = new FormData(e.currentTarget)
    go({ from: String(d.get('from') ?? '').trim(), component: String(d.get('component') ?? '').trim() })
  }
  const filtered = f.state !== 'open' || f.from !== '' || f.component !== ''
  const stateLabel = (t.pendings.states as Record<string, string>)[f.state] ?? f.state

  return (
    <main className="page">
      <h1>{t.pendings.title}</h1>
      <p className="muted">{t.pendings.intro}</p>
      {load.data !== null && <LoadNotice load={load} />}
      <div className="filters" role="group" aria-label={t.pendings.stateFilter}>
        {STATES.map((s) => (
          <button
            key={s}
            type="button"
            className={f.state === s ? 'filter active' : 'filter'}
            aria-pressed={f.state === s}
            onClick={() => go({ state: s })}
          >
            {t.pendings.states[s]}
            {load.data !== null && <> <span className="column-count">{ofState(s).length}</span></>}
          </button>
        ))}
      </div>
      {/* Keyed by the whole filter: every navigation remounts the inputs from
          the hash, so they never show a value that is not applied (C8). */}
      <form className="search-form" key={filterHash(f)} onSubmit={submit}>
        <label>
          {t.pendings.from} <input name="from" defaultValue={f.from} />
        </label>
        <label>
          {t.pendings.component} <input name="component" defaultValue={f.component} />
        </label>
        <button type="submit">{t.pendings.apply}</button>
        {filtered && <a href="#/pendings">{t.pendings.clear}</a>}
      </form>
      {load.data === null ? (
        <LoadPending load={load} message={t.pendings.loadError} />
      ) : shown.length === 0 ? (
        <p className="muted">{t.pendings.empty(stateLabel, f.from || undefined, f.component || undefined)}</p>
      ) : (
        <>
          <p className="counts">{t.pendings.count(shown.length)}</p>
          <ul className="entity-list">
            {shown.map((p) => (
              <PendingRow
                key={p.id}
                p={p}
                component={slug(f.component)}
                onComponent={(c) => go({ component: c === slug(f.component) ? '' : c })}
              />
            ))}
          </ul>
        </>
      )}
    </main>
  )
}

function PendingRow({ p, component, onComponent }: { p: Pending; component: string; onComponent: (c: string) => void }) {
  const { t, lang } = useT()
  const [open, setOpen] = useState(false)
  // O mesmo truncamento do pendingAge do CLI (dias inteiros).
  const age = Math.floor((Date.now() - Date.parse(p.created_at)) / 86_400_000)
  const date = (iso: string) => new Date(iso).toLocaleDateString(lang)
  return (
    <li>
      <div className="entity-row">
        <button type="button" className="pending-toggle" aria-expanded={open} onClick={() => setOpen(!open)}>
          <span className="card-id">{p.id}</span>
          <span className="entity-title">{p.title}</span>
        </button>
        {p.state !== 'open' && <span className={`chip ${CHIP[p.state]}`}>{t.pendings.stateChip[p.state]}</span>}
        <span className="muted">{t.pendings.age(age)}</span>
        <a href={fHref(p.from)}>{p.from}</a>
        {(p.components ?? []).map((c) => (
          <Tag key={c} axis="component" value={c} active={c === component} onToggle={onComponent} />
        ))}
      </div>
      {open && (
        <div className="pending-detail">
          {p.body && <Markdown>{p.body}</Markdown>}
          <p className="muted">
            <span>
              {t.pendings.created} {date(p.created_at)}
            </span>
            {p.closed_at && (
              <>
                {' · '}
                <span>
                  {t.pendings.closed} {date(p.closed_at)}
                </span>
              </>
            )}
          </p>
          {p.reason && (
            <p>
              {t.common.reasonLabel} {p.reason}
            </p>
          )}
          {p.promoted_to && (
            <p>
              {t.pendings.promotedTo} <a href={fHref(p.promoted_to)}>{p.promoted_to}</a>
            </p>
          )}
          {p.state === 'open' && (
            <>
              <h3>{t.pendings.commands}</h3>
              <p className="muted">{t.pendings.commandsHint}</p>
              <FixBlock command={`cerne pending resolve ${p.id}`} label={t.pendings.copyResolve(p.id)} />
              <FixBlock command={`cerne pending drop ${p.id} --reason ""`} label={t.pendings.copyDrop(p.id)} />
              <FixBlock command={`cerne pending promote ${p.id} --as feature`} label={t.pendings.copyPromote(p.id)} />
            </>
          )}
        </div>
      )}
    </li>
  )
}

// DeliveryPendings (F-0334 crit. 4): as pendências abertas que a entrega
// deixou, na página da feature. Painel acessório como o Backlinks: lista vazia
// ou falha de carga não ocupam a página.
export function DeliveryPendings({ id }: { id: string }) {
  const { t } = useT()
  const load = useLiveLoad(() => fetchPendings({ from: id }), [id])
  if (!load.data?.length) return null
  return (
    <section className="backlinks-section">
      <h2 className="backlinks-title">{t.pendings.deliveryTitle}</h2>
      <ul className="backlinks-list">
        {load.data.map((p) => (
          <li key={p.id}>
            <span className="card-id">{p.id}</span> <span className="backlinks-entity">{p.title}</span>
          </li>
        ))}
      </ul>
      <a href={`#/pendings?from=${encodeURIComponent(id)}`}>{t.pendings.seeAll}</a>
    </section>
  )
}
