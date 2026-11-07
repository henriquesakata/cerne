import { useRef, useState } from 'react'
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
  onRename: (id: string, title: string) => void
}

// ConversationList (F-0219) é a seção "Conversas" da coluna esquerda do
// Backlog: <details> nativo cobre o colapsável de graça (critério 1), sem
// estado nem dependência nova.
export default function ConversationList({ conversations, activeId, onSelect, onNew, onDelete, onRename }: Props) {
  const { t } = useT()
  const sorted = [...conversations].sort((a, b) => b.updatedAt - a.updatedAt)
  // Rename em linha (F-0310): só o primeiro desfecho vale (settled) — o blur
  // que segue Enter/Escape não confirma de novo; Enter e Escape devolvem o
  // foco ao ✎ da linha, o blur não (não rouba o foco do que foi clicado).
  const [editingId, setEditingId] = useState<string | null>(null)
  const settled = useRef(false)
  const renameBtn = useRef<HTMLButtonElement | null>(null)
  const field = useRef<HTMLInputElement | null>(null)

  const startRename = (id: string, btn: HTMLButtonElement) => {
    settled.current = false
    renameBtn.current = btn
    setEditingId(id)
  }

  // finish: value null = cancelar (crit. 19); vazio ou inalterado não chama
  // onRename (crit. 18).
  const finish = (c: Conversation, value: string | null, viaKey: boolean) => {
    if (settled.current) return
    settled.current = true
    setEditingId(null)
    const next = value?.trim() ?? ''
    if (next !== '' && next !== c.title) onRename(c.id, next)
    if (viaKey) renameBtn.current?.focus()
  }

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
            {editingId === c.id ? (
              <input
                ref={field}
                className="conversation-item-rename-input"
                aria-label={t.backlogChat.renameInput}
                defaultValue={c.title}
                autoFocus
                onFocus={(e) => e.currentTarget.select()}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') {
                    // C1: cancelar o keydown suprime o keypress do mesmo Enter,
                    // que senão ativaria o ✎ recém-focado e reabriria o editor.
                    e.preventDefault()
                    finish(c, e.currentTarget.value, true)
                  } else if (e.key === 'Escape') finish(c, null, true)
                }}
                onBlur={(e) => finish(c, e.currentTarget.value, false)}
              />
            ) : (
              <button type="button" className="conversation-item-select" onClick={() => onSelect(c.id)}>
                <span className="conversation-item-title">{c.title || t.backlogChat.untitledConversation}</span>
                {/* ponytail: sem tick — aba ociosa mostra a idade do último render; um intervalo de 60 s resolve se incomodar. */}
                <span className="conversation-item-age">{t.notif.age(ageMinutes(c.updatedAt))}</span>
              </button>
            )}
            <button
              type="button"
              className="conversation-item-rename"
              aria-label={t.backlogChat.renameConversation(c.title || t.backlogChat.untitledConversation)}
              title={t.backlogChat.renameConversation(c.title || t.backlogChat.untitledConversation)}
              // C15: durante a edição o ✎ alterna — o mousedown mantém o foco
              // no campo (sem o blur que confirmaria) e o click fecha o editor,
              // confirmando como o Enter.
              onMouseDown={(e) => {
                if (editingId === c.id) e.preventDefault()
              }}
              onClick={(e) =>
                editingId === c.id ? finish(c, field.current?.value ?? null, true) : startRename(c.id, e.currentTarget)
              }
            >
              ✎
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
