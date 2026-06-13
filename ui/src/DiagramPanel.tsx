import { useEffect, useRef, useState } from 'react'
import {
  Excalidraw,
  getSceneVersion,
  serializeAsJSON,
} from '@excalidraw/excalidraw'
import type {
  ExcalidrawImperativeAPI,
  ExcalidrawInitialDataState,
} from '@excalidraw/excalidraw/types'
import '@excalidraw/excalidraw/index.css'
import { fetchArtifactJSON, putArtifact } from './api'

// Este módulo é carregado via React.lazy em FeaturePage: o bundle do
// Excalidraw (pesado) só é baixado quando um diagrama é aberto.

type Msg = { kind: 'ok' | 'err'; text: string }

export default function DiagramPanel({
  featureId,
  name,
  isNew,
  onClose,
  onSaved,
}: {
  featureId: string
  name: string
  isNew: boolean
  onClose: () => void
  onSaved: () => void
}) {
  const [scene, setScene] = useState<ExcalidrawInitialDataState | null>(
    isNew ? {} : null,
  )
  const [msg, setMsg] = useState<Msg | null>(null)
  const [dirty, setDirty] = useState(false)
  const [busy, setBusy] = useState(false)
  const apiRef = useRef<ExcalidrawImperativeAPI | null>(null)
  // Versão da cena no último load/save: base do flag "alterado".
  const savedVersionRef = useRef<number | null>(null)

  useEffect(() => {
    if (isNew) return
    fetchArtifactJSON(featureId, name)
      .then((text) => {
        const parsed = JSON.parse(text) as ExcalidrawInitialDataState
        if (parsed.appState && 'collaborators' in parsed.appState) {
          // collaborators serializado (objeto) quebra o restore (espera Map).
          delete (parsed.appState as { collaborators?: unknown }).collaborators
        }
        setScene(parsed)
      })
      .catch((e: unknown) =>
        setMsg({
          kind: 'err',
          text: `Erro ao carregar o diagrama: ${e instanceof Error ? e.message : String(e)}`,
        }),
      )
  }, [featureId, name, isNew])

  const save = () => {
    const api = apiRef.current
    if (!api) return
    setBusy(true)
    setMsg(null)
    const json = serializeAsJSON(
      api.getSceneElements(),
      api.getAppState(),
      api.getFiles(),
      'local',
    )
    void putArtifact(featureId, name, json).then((res) => {
      setBusy(false)
      if (res.ok) {
        savedVersionRef.current = getSceneVersion(api.getSceneElements())
        setDirty(false)
        setMsg({ kind: 'ok', text: 'Diagrama salvo.' })
        onSaved()
      } else {
        setMsg({ kind: 'err', text: res.error })
      }
    })
  }

  return (
    <section className="diagram-panel">
      <header className="diagram-toolbar">
        <h2>{name}</h2>
        <div className="diagram-controls">
          {msg && (
            <span className={msg.kind === 'ok' ? 'notice' : 'form-error'}>
              {msg.text}
            </span>
          )}
          <button onClick={save} disabled={busy || !dirty}>
            Salvar diagrama
          </button>
          <button onClick={onClose}>Fechar</button>
        </div>
      </header>
      {scene === null ? (
        <p className="status">Carregando diagrama…</p>
      ) : (
        <div className="diagram-canvas">
          <Excalidraw
            initialData={scene}
            langCode="pt-BR"
            excalidrawAPI={(api) => {
              apiRef.current = api
              savedVersionRef.current = getSceneVersion(api.getSceneElements())
            }}
            onChange={(elements) => {
              if (savedVersionRef.current === null) return
              setDirty(getSceneVersion(elements) !== savedVersionRef.current)
            }}
          />
        </div>
      )}
    </section>
  )
}
