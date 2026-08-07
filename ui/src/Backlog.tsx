import { useEffect, useState } from 'react'
import { fetchState, type Feature } from './api'
import Tag from './Tag'
import BacklogChat from './BacklogChat'
import ConversationList from './ConversationList'
import { useBacklogChats } from './useBacklogChats'
import { sortByPriority } from './priority'
import { useLiveVersion } from './live'
import { useT } from './i18n'

// Backlog (F-0202): candidatas separadas do kanban — exploração, não esteira.
// candidate significa apenas PRD registrado, sem gate humano ainda; misturar
// com trabalho comprometido dava a impressão errada de urgência.
// F-0210: layout de duas colunas — lista à esquerda, chat com o agente de
// domínio à direita, pra que a candidata registrada apareça ali mesmo.
// F-0219: a coluna esquerda ganha a lista de conversas (ConversationList);
// BacklogChat é montado com key={chats.active.id} — trocar de conversa
// remonta o chat e reseta seu estado efêmero de graça.
export default function Backlog() {
  const [features, setFeatures] = useState<Feature[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [refreshKey, setRefreshKey] = useState(0)
  const version = useLiveVersion()
  const { t } = useT()
  const chats = useBacklogChats()

  useEffect(() => {
    fetchState()
      .then((s) => {
        setFeatures(s.features.filter((f) => f.state === 'candidate'))
        setError(null)
      })
      .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)))
  }, [version, refreshKey])

  return (
    <main className="kanban backlog-layout">
      <div className="backlog-list-col">
        <header className="kanban-header">
          <h1>{t.nav.backlog}</h1>
          {features && <p className="counts">{t.backlog.count(features.length)}</p>}
        </header>
        {error && <p className="status error">{t.backlog.loadError(error)}</p>}
        {!error && !features && <p className="status">{t.common.loading}</p>}
        {features && features.length === 0 && <p className="status">{t.backlog.empty}</p>}
        {features && features.length > 0 && (
          <div className="epic-lane-cards">
            {sortByPriority(features).map((f) => (
              <BacklogCard key={f.id} feature={f} />
            ))}
          </div>
        )}
        <ConversationList
          conversations={chats.chats}
          activeId={chats.active.id}
          onSelect={chats.select}
          onNew={() => chats.newConversation()}
          onDelete={(id) => chats.deleteConversation(id)}
        />
      </div>
      <BacklogChat
        key={chats.active.id}
        conversation={chats.active}
        onUpdate={chats.updateActive}
        onRegistered={(feature) => {
          chats.appendRegisteredId(feature.id)
          setRefreshKey((k) => k + 1)
        }}
      />
    </main>
  )
}

function BacklogCard({ feature }: { feature: Feature }) {
  const hasTags =
    (feature.facets?.length ?? 0) > 0 || (feature.components?.length ?? 0) > 0
  return (
    <a className="card card-candidate" href={`#/f/${feature.id}`}>
      <span className="card-meta">
        <span className="card-id">{feature.id}</span>
        {feature.module && <span className="badge">{feature.module}</span>}
        {feature.priority && <span className="badge badge-priority">{feature.priority}</span>}
      </span>
      <span className="card-title">{feature.title}</span>
      {feature.owner && <span className="card-owner">@{feature.owner}</span>}
      {hasTags && (
        <span className="note-tags">
          {feature.facets?.map((f) => (
            <Tag key={`f-${f}`} axis="facet" value={f} />
          ))}
          {feature.components?.map((c) => (
            <Tag key={`c-${c}`} axis="component" value={c} />
          ))}
        </span>
      )}
    </a>
  )
}
