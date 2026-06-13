import { useEffect, useState } from 'react'
import Markdown from './Markdown'
import { fetchWikiIndex, fetchWikiPage, type WikiPageRef, type WikiPage } from './api'

// TOP_LABELS rotula as páginas transversais; módulos usam o próprio nome.
const TOP_LABELS: Record<string, string> = {
  project: 'Projeto',
  decisions: 'Decisões',
  references: 'Referências',
}

export default function Wiki() {
  const [pages, setPages] = useState<WikiPageRef[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    fetchWikiIndex()
      .then(setPages)
      .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)))
  }, [])

  if (error) return <p className="status error">Erro ao carregar a wiki: {error}</p>
  if (!pages) return <p className="status">Carregando…</p>

  const top = pages.filter((p) => !p.rel.startsWith('modules/'))
  const modules = pages.filter((p) => p.rel.startsWith('modules/'))

  return (
    <main className="page">
      <h1>Wiki</h1>
      <p className="muted">
        Síntese compilada por <code>cerne compile</code>, vigente-primeiro. É leitura;
        a autoria é via CLI/agente.
      </p>
      {pages.length === 0 && (
        <p className="muted">
          Nenhuma página compilada ainda. Rode <code>cerne compile</code> para gerar a síntese.
        </p>
      )}
      {top.length > 0 && (
        <section className="entity-group">
          <h2>Visão geral</h2>
          <ul className="entity-list">
            {top.map((p) => (
              <li key={p.rel}>
                <a className="entity-row" href={`#/wiki/${p.rel}`}>
                  <span className="entity-title">{TOP_LABELS[p.name] ?? p.name}</span>
                </a>
              </li>
            ))}
          </ul>
        </section>
      )}
      {modules.length > 0 && (
        <section className="entity-group">
          <h2>
            Módulos <span className="column-count">{modules.length}</span>
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

  useEffect(() => {
    fetchWikiPage(rel)
      .then(setPage)
      .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)))
  }, [rel])

  if (error) return <p className="status error">Erro ao carregar a página: {error}</p>
  if (!page) return <p className="status">Carregando…</p>

  return (
    <main className="page">
      <a className="back" href="#/wiki">← Wiki</a>
      <article className="doc">
        <Markdown>{page.body}</Markdown>
      </article>
    </main>
  )
}
