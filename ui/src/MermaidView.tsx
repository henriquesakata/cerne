import { useEffect, useRef, useState } from 'react'
import { fetchArtifactText } from './api'

// Este módulo é carregado via React.lazy em FeaturePage: o bundle do mermaid
// (pesado) só é baixado quando o usuário abre um diagrama .mmd.

type View =
  | { kind: 'loading' }
  | { kind: 'ok'; svg: string; source: string }
  | { kind: 'error'; message: string; source: string }

// Contador de instâncias para gerar ids únicos de render do mermaid (cada
// render precisa de um id distinto, senão o mermaid reaproveita nós antigos).
let renderSeq = 0

export default function MermaidView({
  featureId,
  name,
  onClose,
  onEdit,
}: {
  featureId: string
  name: string
  onClose: () => void
  // onEdit recebe o texto Mermaid bruto para a conversão em Excalidraw.
  onEdit: (mermaidText: string) => void
}) {
  const [view, setView] = useState<View>({ kind: 'loading' })
  const renderId = useRef(`mermaid-${renderSeq++}`)

  useEffect(() => {
    let cancelled = false
    void (async () => {
      let source = ''
      try {
        source = await fetchArtifactText(featureId, name)
        // Import dinâmico: o mermaid fica num chunk lazy, fora do bundle de entrada.
        const mermaid = (await import('mermaid')).default
        mermaid.initialize({
          startOnLoad: false,
          theme:
            document.documentElement.dataset.theme === 'dark'
              ? 'dark'
              : 'default',
        })
        const { svg } = await mermaid.render(renderId.current, source)
        if (!cancelled) setView({ kind: 'ok', svg, source })
      } catch (e: unknown) {
        if (!cancelled) {
          setView({
            kind: 'error',
            message: e instanceof Error ? e.message : String(e),
            source,
          })
        }
      }
    })()
    return () => {
      cancelled = true
    }
  }, [featureId, name])

  const source =
    view.kind === 'ok' || view.kind === 'error' ? view.source : ''

  return (
    <section className="diagram-panel">
      <header className="diagram-toolbar">
        <h2>{name}</h2>
        <div className="diagram-controls">
          <button
            onClick={() => onEdit(source)}
            disabled={view.kind === 'loading' || source.trim() === ''}
          >
            Editar como Excalidraw
          </button>
          <button onClick={onClose}>Fechar</button>
        </div>
      </header>
      {view.kind === 'loading' && (
        <p className="status">Renderizando diagrama…</p>
      )}
      {view.kind === 'ok' && (
        <div
          className="mermaid-view"
          // O SVG vem do mermaid.render local (conteúdo gerado, não entrada
          // arbitrária de usuário externo).
          dangerouslySetInnerHTML={{ __html: view.svg }}
        />
      )}
      {view.kind === 'error' && (
        <div className="mermaid-error">
          <p className="form-error">
            Não foi possível renderizar o Mermaid: {view.message}
          </p>
          <pre>{view.source}</pre>
        </div>
      )}
    </section>
  )
}
