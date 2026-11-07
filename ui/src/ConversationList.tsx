import { useEffect, useRef, useState } from 'react'
import type { Conversation } from './useBacklogChats'
import { useT } from './i18n'

// AGE_TICK_MS (C8): a idade é recalculada a cada render; o tick garante um
// render por minuto com a aba parada.
const AGE_TICK_MS = 60_000

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
  const [, setTick] = useState(0)
  useEffect(() => {
    const id = setInterval(() => setTick((n) => n + 1), AGE_TICK_MS)
    return () => clearInterval(id)
  }, [])
  // Rename em linha (F-0310): só o primeiro desfecho vale (settled) — o blur
  // que segue Enter/Escape não confirma de novo; Enter e Escape devolvem o
  // foco ao ✎ da linha, o blur não (não rouba o foco do que foi clicado).
  const [editingId, setEditingId] = useState<string | null>(null)
  const settled = useRef(false)
  const renameBtn = useRef<HTMLButtonElement | null>(null)
  const field = useRef<HTMLInputElement | null>(null)
  // openedWith (C4): o título quando o editor abriu — "inalterado" é contra
  // ele, não contra c.title, que o resumo pode trocar com o editor aberto.
  const openedWith = useRef('')

  const startRename = (c: Conversation, btn: HTMLButtonElement) => {
    settled.current = false
    renameBtn.current = btn
    openedWith.current = c.title
    setEditingId(c.id)
  }

  // finish: value null = cancelar (crit. 19); vazio ou inalterado não chama
  // onRename (crit. 18, C4).
  const finish = (c: Conversation, value: string | null, viaKey: boolean) => {
    if (settled.current) return
    settled.current = true
    setEditingId(null)
    const next = value?.trim() ?? ''
    if (next !== '' && next !== openedWith.current.trim()) onRename(c.id, next)
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
              <button
                type="button"
                className="conversation-item-select"
                // C9: a coluna corta o título (reticência de CSS); o tooltip mostra
                // o nome inteiro, que o nome acessível (o conteúdo) já carrega.
                title={c.title || t.backlogChat.untitledConversation}
                onClick={() => onSelect(c.id)}
              >
                <span className="conversation-item-title">{c.title || t.backlogChat.untitledConversation}</span>
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
                editingId === c.id ? finish(c, field.current?.value ?? null, true) : startRename(c, e.currentTarget)
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
