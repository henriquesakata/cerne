import { useEffect, useRef, useState } from 'react'
import { fetchArtifactText } from './api'
import { useT } from './i18n'

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
}: {
  featureId: string
  name: string
  onClose: () => void
}) {
  const [view, setView] = useState<View>({ kind: 'loading' })
  const renderId = useRef(`mermaid-${renderSeq++}`)
  const { t } = useT()

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

  return (
    <section className="diagram-panel">
      <header className="diagram-toolbar">
        <h2>{name}</h2>
        <div className="diagram-controls">
          <button onClick={onClose}>{t.diagram.close}</button>
        </div>
      </header>
      {view.kind === 'loading' && (
        <p className="status">{t.diagram.rendering}</p>
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
            {t.diagram.mermaidError(view.message)}
          </p>
          <pre>{view.source}</pre>
        </div>
      )}
    </section>
  )
}
