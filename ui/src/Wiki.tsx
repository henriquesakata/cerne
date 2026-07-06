import { useEffect, useState } from 'react'
import Markdown from './Markdown'
import { fetchWikiIndex, fetchWikiPage, type WikiPageRef, type WikiPage } from './api'
import { useT } from './i18n'

export default function Wiki() {
  const [pages, setPages] = useState<WikiPageRef[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const { t } = useT()

  useEffect(() => {
    fetchWikiIndex()
      .then(setPages)
      .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)))
  }, [])

  if (error) return <p className="status error">{t.wiki.loadError(error)}</p>
  if (!pages) return <p className="status">{t.common.loading}</p>

  const top = pages.filter((p) => !p.rel.startsWith('modules/'))
  const modules = pages.filter((p) => p.rel.startsWith('modules/'))

  return (
    <main className="page">
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
  const [page, setPage] = useState<WikiPage | null>(null)
  const [error, setError] = useState<string | null>(null)
  const { t } = useT()

  useEffect(() => {
    fetchWikiPage(rel)
      .then(setPage)
      .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)))
  }, [rel])

  if (error) return <p className="status error">{t.wiki.loadPageError(error)}</p>
  if (!page) return <p className="status">{t.common.loading}</p>

  return (
    <main className="page">
      <a className="back" href="#/wiki">{t.wiki.back}</a>
      <article className="doc">
        <Markdown>{page.body}</Markdown>
      </article>
    </main>
  )
}
