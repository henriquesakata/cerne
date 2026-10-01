import Markdown from './Markdown'
import { fetchWikiIndex, fetchWikiPage } from './api'
import { useT } from './i18n'
import { LoadNotice, LoadPending, useLiveLoad } from './liveLoad'

export default function Wiki() {
  // live: false — a wiki não re-buscava a cada avanço do backend e segue assim
  // (F-0300, critério 6): o que ela ganha é o "Tentar de novo".
  const load = useLiveLoad(fetchWikiIndex, [], { live: false })
  const pages = load.data
  const { t } = useT()

  if (!pages) return <LoadPending load={load} message={t.wiki.loadError} />

  const top = pages.filter((p) => !p.rel.startsWith('modules/'))
  const modules = pages.filter((p) => p.rel.startsWith('modules/'))

  return (
    <main className="page">
      <LoadNotice load={load} />
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
