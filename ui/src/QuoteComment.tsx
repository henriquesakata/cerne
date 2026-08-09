import { useEffect, useRef, useState, type ReactNode } from 'react'
import { useT } from './i18n'

// QuoteComment (F-0250) ancora feedback num trecho exato da proposta: o
// usuário seleciona, comenta, e o trecho vira citação no compositor — o
// feedback volta ao agente pelo turno seguinte, sem endpoint, protocolo ou
// estado persistido novo.
//
// É um wrapper genérico de propósito: a F-0255 monta o painel de rascunho
// dentro do mesmo componente, e o comportamento de comentar vem junto.

// QUOTE_MAX_CHARS é o teto da citação (critério 6). Longo o bastante para
// localizar um critério inteiro, curto o bastante para não inundar o
// compositor — o trecho é localizador, não cópia da spec.
export const QUOTE_MAX_CHARS = 280

const ELLIPSIS = ' … '

// elideSnippet normaliza o espaço (a seleção atravessa quebras de linha do
// markdown) e, quando passa do teto, corta o MEIO preservando as duas pontas:
// é o começo e o fim que permitem ao agente localizar o trecho na própria
// resposta anterior.
export function elideSnippet(text: string, max = QUOTE_MAX_CHARS): string {
  const flat = text.replace(/\s+/g, ' ').trim()
  if (flat.length <= max) return flat
  const half = Math.floor((max - ELLIPSIS.length) / 2)
  return flat.slice(0, half).trimEnd() + ELLIPSIS + flat.slice(flat.length - half).trimStart()
}

// buildQuote monta o texto que entra no compositor: blockquote com o trecho,
// linha em branco, comentário. Termina com linha em branco para o próximo
// comentário (critério 3) não colar no anterior.
export function buildQuote(snippet: string, comment: string): string {
  return `> ${elideSnippet(snippet)}\n\n${comment.trim()}\n\n`
}

// readSelection extrai o texto selecionado dentro de `root`. Dois caminhos
// porque o card tem os dois: campo de formulário (a spec editável da F-0223)
// responde por selectionStart/End, e DOM renderizado responde pela Selection
// API — cujo toString() devolve o texto VISÍVEL, que é o que resolve o
// critério 5 (seleção atravessando <strong>/<code>/<a> sem markup quebrado).
function readSelection(root: HTMLElement | null): string {
  const active = document.activeElement
  if (
    root &&
    (active instanceof HTMLTextAreaElement || active instanceof HTMLInputElement) &&
    root.contains(active)
  ) {
    return active.value.slice(active.selectionStart ?? 0, active.selectionEnd ?? 0)
  }
  const sel = window.getSelection()
  if (!sel || sel.rangeCount === 0 || sel.isCollapsed) return ''
  if (root && !root.contains(sel.anchorNode)) return ''
  return sel.toString()
}

type Anchor = { top: number; left: number }

// anchorFor posiciona o popover junto da seleção. O rect do Range é o ideal;
// campo de formulário não tem rect de seleção (e jsdom não implementa
// getBoundingClientRect), então cai para o rect do próprio elemento.
function anchorFor(root: HTMLElement | null): Anchor {
  const base = root?.getBoundingClientRect()
  const sel = window.getSelection()
  let rect: DOMRect | undefined
  if (sel && sel.rangeCount > 0) {
    rect = sel.getRangeAt(0).getBoundingClientRect?.()
  }
  if (!rect || (rect.width === 0 && rect.height === 0)) {
    rect = (document.activeElement as HTMLElement | null)?.getBoundingClientRect?.()
  }
  if (!rect || !base) return { top: 0, left: 0 }
  return { top: rect.bottom - base.top, left: rect.left - base.left }
}

type Props = {
  onQuote: (quote: string) => void
  children: ReactNode
  className?: string
}

export default function QuoteComment({ onQuote, children, className }: Props) {
  const rootRef = useRef<HTMLDivElement>(null)
  const popRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLInputElement>(null)
  const [snippet, setSnippet] = useState('')
  const [anchor, setAnchor] = useState<Anchor>({ top: 0, left: 0 })
  const [comment, setComment] = useState('')
  const { t } = useT()

  const dismiss = () => {
    setSnippet('')
    setComment('')
  }

  const onMouseUp = () => {
    const text = readSelection(rootRef.current)
    // Clique simples (ou clique dentro do próprio popover) não abre nada —
    // e, crucialmente, não fecha o popover aberto.
    if (text.trim() === '') return
    setAnchor(anchorFor(rootRef.current))
    setSnippet(text)
    setComment('')
  }

  // Critério 9: Esc ou clique fora descartam sem tocar no compositor.
  useEffect(() => {
    if (!snippet) return
    inputRef.current?.focus()
    const onDown = (e: MouseEvent) => {
      if (!popRef.current?.contains(e.target as Node)) dismiss()
    }
    const onKey = (e: globalThis.KeyboardEvent) => {
      if (e.key === 'Escape') dismiss()
    }
    document.addEventListener('mousedown', onDown)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDown)
      document.removeEventListener('keydown', onKey)
    }
  }, [snippet])

  const confirm = () => {
    if (comment.trim() === '') return
    onQuote(buildQuote(snippet, comment))
    dismiss()
  }

  return (
    <div className={className} ref={rootRef} onMouseUp={onMouseUp} style={{ position: 'relative' }}>
      {children}
      {snippet && (
        <div
          className="quote-popover"
          ref={popRef}
          role="dialog"
          aria-label={t.quoteComment.title}
          style={{ top: anchor.top, left: anchor.left }}
        >
          <p className="quote-popover-snippet">{elideSnippet(snippet, 120)}</p>
          <input
            ref={inputRef}
            value={comment}
            placeholder={t.quoteComment.placeholder}
            onChange={(e) => setComment(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault()
                confirm()
              }
            }}
          />
          <div className="quote-popover-actions">
            <button type="button" onClick={confirm} disabled={comment.trim() === ''}>
              {t.quoteComment.confirm}
            </button>
            <button type="button" className="quote-popover-cancel" onClick={dismiss}>
              {t.quoteComment.cancel}
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
