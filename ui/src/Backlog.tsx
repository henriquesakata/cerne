import { useEffect, useRef, useState } from 'react'
import { fetchState, type Feature } from './api'
import Tag from './Tag'
import BacklogChat, { ChatToolbar, HistoryMenu } from './BacklogChat'
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
// F-0219/F-0431: a lista de conversas vive num menu da barra do chat;
// BacklogChat é montado com key={chats.active.id} — trocar de conversa
// remonta o chat e reseta seu estado efêmero de graça.
// useWide (F-0431): >= 48rem o histórico é uma coluna ao lado do chat; abaixo,
// o menu da barra. Sem matchMedia (jsdom puro) vale o estreito. A lista é
// montada num lugar só.
const WIDE = '(min-width: 48rem)'
function useWide(): boolean {
  const mq = () => (typeof window.matchMedia === 'function' ? window.matchMedia(WIDE) : null)
  const [wide, setWide] = useState(() => mq()?.matches ?? false)
  useEffect(() => {
    const m = mq()
    if (!m) return
    const on = () => setWide(m.matches)
    on()
    m.addEventListener('change', on)
    return () => m.removeEventListener('change', on)
  }, [])
  return wide
}

// B-0048: recolher a coluna de candidatas e a de conversas é preferência de
// quem olha, por navegador — o mesmo contrato da coluna Backlog do kanban
// (F-0342). Storage inacessível cai no padrão expandido e nunca quebra a página.
const LIST_OPEN_KEY = 'cerne.backlog.listOpen'
const RAIL_OPEN_KEY = 'cerne.backlog.railOpen'

function readOpen(key: string): boolean {
  try {
    return localStorage.getItem(key) !== 'false'
  } catch {
    return true
  }
}

function writeOpen(key: string, open: boolean) {
  try {
    localStorage.setItem(key, String(open))
  } catch {
    // preferência não persistida; o toggle da sessão continua valendo
  }
}

export default function Backlog() {
  const [features, setFeatures] = useState<Feature[] | null>(null)
  // F-0195: contagem de [ESCLARECER] abertos, mesma fonte do kanban. Servidor
  // antigo não manda o campo — ausência é "sem pendência", nunca erro.
  const [openClarifications, setOpenClarifications] = useState<Record<string, number>>({})
  const [error, setError] = useState<string | null>(null)
  const [refreshKey, setRefreshKey] = useState(0)
  const version = useLiveVersion()
  const { t } = useT()
  const chats = useBacklogChats()
  const wide = useWide()
  const [listOpen, setListOpen] = useState(() => readOpen(LIST_OPEN_KEY))
  const toggleList = () => {
    setListOpen(!listOpen)
    writeOpen(LIST_OPEN_KEY, !listOpen)
  }
  // Rail recolhido = o layout largo usa o menu da barra do layout estreito:
  // "Nova conversa" e o histórico continuam a um clique, só a coluna sai.
  const [railOpen, setRailOpen] = useState(() => readOpen(RAIL_OPEN_KEY))
  const toggleRail = () => {
    setRailOpen(!railOpen)
    writeOpen(RAIL_OPEN_KEY, !railOpen)
  }
  const railed = wide && railOpen
  // Trocar de conversa remonta o chat (key): depois de escolher ou criar, o
  // foco vai para o compositor da nova conversa.
  const focusComposer = useRef(false)
  useEffect(() => {
    if (!focusComposer.current) return
    focusComposer.current = false
    document.querySelector<HTMLElement>('.backlog-chat textarea')?.focus()
  }, [chats.active.id])
  // O flag vale só para esta ação: se a conversa não mudar (confirmação do cap
  // recusada), é solto logo depois do flush.
  const armFocus = () => {
    focusComposer.current = true
    setTimeout(() => (focusComposer.current = false), 0)
  }
  const history = {
    conversations: chats.chats,
    activeId: chats.active.id,
    onSelect: (id: string) => {
      if (id !== chats.active.id) armFocus()
      chats.select(id)
    },
    onNew: () => {
      armFocus()
      chats.newConversation()
    },
    onDelete: (id: string) => chats.deleteConversation(id),
    onRename: chats.renameConversation,
  }

  useEffect(() => {
    fetchState()
      .then((s) => {
        setFeatures(s.features.filter((f) => f.state === 'candidate'))
        setOpenClarifications(s.open_clarifications ?? {})
        setError(null)
      })
      .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)))
  }, [version, refreshKey])

  return (
    <main className={listOpen ? 'kanban backlog-layout' : 'kanban backlog-layout backlog-layout-collapsed'}>
      <header className="backlog-head">
        {/* B-0048: o título é o controle, como o <summary> da coluna Backlog
            do kanban; botão dentro do h1 (o inverso seria inválido). */}
        <h1>
          <button
            type="button"
            className="backlog-toggle"
            aria-expanded={listOpen}
            aria-controls="backlog-list"
            title={listOpen ? t.backlog.collapseList : t.backlog.expandList}
            onClick={toggleList}
          >
            {t.nav.backlog}
          </button>
        </h1>
        {features && (
          <p className="backlog-count" title={t.backlog.count(features.length)}>
            {listOpen ? t.backlog.count(features.length) : features.length}
          </p>
        )}
      </header>
      {/* DOM: o painel do chat vem antes das candidatas (ordem de Tab e de
          leitura iguais à visual abaixo de 60rem); acima, o grid posiciona. A
          coluna de histórico fica fora do key, para manter rolagem e foco ao
          trocar de conversa. */}
      <div className={railed ? 'backlog-chat-shell' : 'backlog-chat-shell backlog-chat-shell-stack'}>
        {!railed && (
          <ChatToolbar refineId={chats.active.featureId ?? ''}>
            <HistoryMenu history={history} />
            {wide && (
              <button
                type="button"
                className="backlog-chat-new"
                aria-expanded={false}
                aria-controls="backlog-chat-rail"
                onClick={toggleRail}
              >
                {t.backlogChat.showConversations}
              </button>
            )}
          </ChatToolbar>
        )}
        {railed && (
          <div
            className="backlog-chat-rail"
            id="backlog-chat-rail"
            role="region"
            aria-label={t.backlogChat.conversationsTitle}
          >
            <div className="backlog-chat-rail-head">
              <button type="button" className="backlog-chat-new" onClick={history.onNew}>
                {t.backlogChat.newConversation}
              </button>
              <button
                type="button"
                className="backlog-chat-new backlog-chat-rail-hide"
                aria-expanded={true}
                aria-controls="backlog-chat-rail"
                aria-label={t.backlogChat.hideConversations}
                title={t.backlogChat.hideConversations}
                onClick={toggleRail}
              >
                <span aria-hidden="true">‹</span>
              </button>
            </div>
            <ConversationList
              conversations={history.conversations}
              activeId={history.activeId}
              onSelect={history.onSelect}
              onDelete={history.onDelete}
              onRename={history.onRename}
            />
          </div>
        )}
        <BacklogChat
          key={chats.active.id}
          conversation={chats.active}
          onUpdate={chats.updateActive}
          toolbarOutside={!railed}
          onRegistered={(feature) => {
            chats.appendRegisteredId(feature.id)
            setRefreshKey((k) => k + 1)
          }}
        />
      </div>
      {listOpen && (
        <div className="backlog-list-col" id="backlog-list">
          {error && <p className="backlog-note backlog-note-error">{t.backlog.loadError(error)}</p>}
          {!error && !features && <p className="backlog-note">{t.common.loading}</p>}
          {features && features.length === 0 && <p className="backlog-note">{t.backlog.empty}</p>}
          {features && features.length > 0 && (
            <div className="backlog-roster">
              {sortByPriority(features).map((f) => (
                <BacklogCard
                  key={f.id}
                  feature={f}
                  pending={openClarifications[f.id] ?? 0}
                  onRefine={() => chats.refineFeature(f.id)}
                />
              ))}
            </div>
          )}
        </div>
      )}
    </main>
  )
}

// BacklogCard (F-0256): a ação de refino fica FORA do <a> — botão aninhado em
// link é inválido e o clique disputaria com a navegação. O Backlog só lista
// candidatas, então a ação nunca aparece para uma feature aprovada; o gate de
// verdade é o do servidor (422 pós-candidatura).
function BacklogCard({
  feature,
  pending,
  onRefine,
}: {
  feature: Feature
  pending: number
  onRefine: () => void
}) {
  const { t } = useT()
  const hasTags =
    (feature.facets?.length ?? 0) > 0 || (feature.components?.length ?? 0) > 0
  return (
    <div className="backlog-card">
      <a className="card card-candidate" href={`#/f/${feature.id}`}>
        <span className="card-meta">
          <span className="card-id">{feature.id}</span>
          {pending > 0 && (
            <span className="badge badge-clarify" title={t.kanban.pendingClarifyTitle(pending)}>
              {t.kanban.pendingClarify(pending)}
            </span>
          )}
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
      <button type="button" className="backlog-card-refine" onClick={onRefine}>
        {t.backlog.refineInChat}
      </button>
    </div>
  )
}
