import { useState } from 'react'
import Markdown from './Markdown'
import { fetchWikiIndex, fetchWikiPage, recompileWiki } from './api'
import { useT } from './i18n'
import { LoadNotice, LoadPending, Notice, useLiveLoad } from './liveLoad'

export default function Wiki() {
  // live: false — a wiki não re-buscava a cada avanço do backend e segue assim
  // (F-0300, critério 6): o que ela ganha é o "Tentar de novo".
  const load = useLiveLoad(fetchWikiIndex, [], { live: false })
  const { t } = useT()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  if (!load.data) return <LoadPending load={load} message={t.wiki.loadError} />
  const { pages, stale = [], needs_provider } = load.data

  // Recompilar sob demanda (F-0316): no sucesso recarrega o índice (o aviso
  // some quando nada mais está defasado); na falha a página fica como está,
  // com o erro e a nova tentativa no lugar do botão (F-0300/F-0302).
  const recompile = async () => {
    setBusy(true)
    try {
      await recompileWiki()
      setError(null)
      await load.reload()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const top = pages.filter((p) => !p.rel.startsWith('modules/'))
  const modules = pages.filter((p) => p.rel.startsWith('modules/'))

  return (
    <main className="page">
      <LoadNotice load={load} />
      {stale.length > 0 &&
        (needs_provider ? (
          <p className="ask-notice">
            <span>{t.wiki.stale(stale.length)}</span> {t.wiki.staleDelegation1}
            <code>cerne compile</code>
            {t.wiki.staleDelegation2}
          </p>
        ) : (
          <div className="load-notice" role="status">
            <p>{t.wiki.stale(stale.length)}</p>
            {error === null ? (
              <button
                type="button"
                aria-disabled={busy || undefined}
                onClick={busy ? undefined : () => void recompile()}
              >
                {busy ? t.wiki.recompiling : t.wiki.recompile}
              </button>
            ) : (
              <Notice message={t.wiki.recompileError(error)} onRetry={() => void recompile()} busy={busy} />
            )}
          </div>
        ))}
      <h1>{t.wiki.title}</h1>
      <p className="muted">
        {t.wiki.intro1}<code>cerne compile</code>{t.wiki.intro2}
      </p>
      {pages.length === 0 && (
        <p className="muted">
          {t.wiki.empty1}<code>cerne compile</code>{t.wiki.empty2}
        </p>
      )}
      {top.length > 0 && (
        <section className="entity-group">
          <h2>{t.wiki.overview}</h2>
          <ul className="entity-list">
            {top.map((p) => (
              <li key={p.rel}>
                <a className="entity-row" href={`#/wiki/${p.rel}`}>
                  <span className="entity-title">
                    {(t.wiki.topLabels as Record<string, string>)[p.name] ?? p.name}
                  </span>
                </a>
              </li>
            ))}
          </ul>
        </section>
      )}
      {modules.length > 0 && (
        <section className="entity-group">
          <h2>
            {t.wiki.modules} <span className="column-count">{modules.length}</span>
          </h2>
          <ul className="entity-list">
            {modules.map((p) => (
              <li key={p.rel}>
                <a className="entity-row" href={`#/wiki/${p.rel}`}>
                  <span className="entity-title">{p.name}</span>
                </a>
              </li>
            ))}
          </ul>
        </section>
      )}
    </main>
  )
}

export function WikiPageView({ rel }: { rel: string }) {
  const load = useLiveLoad(() => fetchWikiPage(rel), [rel], { live: false })
  const page = load.data
  const { t } = useT()

  if (!page) return <LoadPending load={load} message={t.wiki.loadPageError} />

  return (
    <main className="page">
      <LoadNotice load={load} />
      <a className="back" href="#/wiki">{t.wiki.back}</a>
      <article className="doc">
        <Markdown>{page.body}</Markdown>
      </article>
    </main>
  )
}
