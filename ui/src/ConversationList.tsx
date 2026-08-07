import type { Conversation } from './useBacklogChats'
import { useT } from './i18n'

function ageMinutes(at: number): number {
  return Math.max(0, Math.floor((Date.now() - at) / 60_000))
}

type Props = {
  conversations: Conversation[]
  activeId: string
  onSelect: (id: string) => void
  onNew: () => void
  onDelete: (id: string) => void
}

// ConversationList (F-0219) é a seção "Conversas" da coluna esquerda do
// Backlog: <details> nativo cobre o colapsável de graça (critério 1), sem
// estado nem dependência nova.
export default function ConversationList({ conversations, activeId, onSelect, onNew, onDelete }: Props) {
  const { t } = useT()
  const sorted = [...conversations].sort((a, b) => b.updatedAt - a.updatedAt)

  return (
    <details className="conversation-list" open>
      <summary>
        {t.backlogChat.conversationsTitle} ({sorted.length})
      </summary>
      <button type="button" className="conversation-list-new" onClick={onNew}>
        {t.backlogChat.newConversation}
      </button>
      <ul>
        {sorted.map((c) => (
          <li
            key={c.id}
            className={c.id === activeId ? 'conversation-item active' : 'conversation-item'}
          >
            <button type="button" className="conversation-item-select" onClick={() => onSelect(c.id)}>
              <span className="conversation-item-title">{c.title || t.backlogChat.untitledConversation}</span>
              <span className="conversation-item-age">{t.notif.age(ageMinutes(c.updatedAt))}</span>
            </button>
            <button
              type="button"
              className="conversation-item-delete"
              aria-label={t.backlogChat.deleteConversation}
              onClick={() => onDelete(c.id)}
            >
              ×
            </button>
          </li>
        ))}
      </ul>
    </details>
  )
}
