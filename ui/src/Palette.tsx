// Palette (F-0196): o spotlight da UI — abre por atalho, filtra entidades por
// id ou título em memória e salta para a página de detalhe. É deliberadamente
// SEM rede a cada tecla: o /state que o board já consome tem id e título de
// tudo, e salto precisa ser instantâneo. Busca no CONTEÚDO continua sendo do
// FTS5 (#/search) — a última linha da lista entrega o usuário lá.

import { useEffect, useMemo, useRef, useState, type KeyboardEvent as ReactKeyboardEvent } from 'react'
import { fetchState, type StateResponse } from './api'
import { useLiveVersion } from './live'
import { useT } from './i18n'

export type EntryKind = 'feature' | 'decision' | 'reference' | 'runbook' | 'note'

export type Entry = {
  id: string
  title: string
  kind: EntryKind
  href: string
}

// Rota de detalhe por espécie, espelhando o roteador de App.tsx.
const HREF_PREFIX: Record<EntryKind, string> = {
  feature: 'f',
  decision: 'd',
  reference: 'r',
  runbook: 'rb',
  note: 'n',
}

// MAX_RESULTS mantém a lista navegável por teclado (e o render barato num
// backend grande): quem precisa de mais que isso quer o FTS5, não a palette.
const MAX_RESULTS = 20

// norm derruba caixa e diacrítico dos dois lados da comparação, para que
// "decisao" ache "Decisão" — o espelho no browser do que internal/slug faz no Go.
export function norm(s: string): string {
  return s
    .toLowerCase()
    .normalize('NFD')
    .replace(/\p{Diacritic}/gu, '')
}

// bareID descarta o prefixo de espécie e os zeros à esquerda ("F-0195" → "195"),
// para que o usuário alcance a entidade digitando só o número que ele lembra.
function bareID(id: string): string {
  return id.replace(/^[a-z]+-/i, '').replace(/^0+/, '')
}

// entriesFrom achata o /state nas entradas indexáveis: tudo que tem id, título
// e página de detalhe.
export function entriesFrom(state: StateResponse): Entry[] {
  const of = (kind: EntryKind, items: Array<{ id: string; title: string }>): Entry[] =>
    items.map((i) => ({
      id: i.id,
      title: i.title,
      kind,
      href: `#/${HREF_PREFIX[kind]}/${encodeURIComponent(i.id)}`,
    }))
  return [
    ...of('feature', state.features),
    ...of('decision', state.decisions),
    ...of('reference', state.references),
    ...of('runbook', state.runbooks),
    ...of('note', state.notes),
  ]
}

// rank filtra e ordena por substring normalizada, em três baldes: prefixo de id
// primeiro (quem digita "195" quer a F-0195, não uma feature que a cita no
// título), depois título, depois id no meio. Substring — e não fuzzy — porque
// num corpus de ids curtos e títulos parecidos a subsequência traz ruído que o
// usuário não consegue explicar.
export function rank(entries: Entry[], query: string): Entry[] {
  const q = norm(query.trim())
  if (q === '') return []
  const byIDPrefix: Entry[] = []
  const byTitle: Entry[] = []
  const byID: Entry[] = []
  // Termo só de zeros normaliza para vazio; sem esta guarda, startsWith('')
  // casaria com o backend inteiro e a palette viraria curinga.
  const bareQ = bareID(q)
  for (const e of entries) {
    const id = norm(e.id)
    if (id.startsWith(q) || (bareQ !== '' && bareID(id).startsWith(bareQ))) byIDPrefix.push(e)
    else if (norm(e.title).includes(q)) byTitle.push(e)
    else if (id.includes(q)) byID.push(e)
  }
  const byIDAsc = (a: Entry, b: Entry) => a.id.localeCompare(b.id)
  return [
    ...byIDPrefix.sort(byIDAsc),
    ...byTitle.sort(byIDAsc),
    ...byID.sort(byIDAsc),
  ].slice(0, MAX_RESULTS)
}

// editable reconhece os contextos em que uma tecla solta é TEXTO, não atalho.
// É o que torna 's' seguro: sem esta guarda, o editor de docs da FeaturePage
// viraria refém do atalho.
function editable(target: EventTarget | null): boolean {
  const el = target as HTMLElement | null
  if (!el) return false
  if (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA') return true
  // closest cobre o nó interno de um editor rico (o alvo do evento é o texto,
  // não o container); o atributo cobre o jsdom, que não implementa
  // isContentEditable.
  return el.isContentEditable || !!el.closest?.('[contenteditable="true"]')
}

export default function Palette() {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const [cursor, setCursor] = useState(0)
  const [entries, setEntries] = useState<Entry[]>([])
  const inputRef = useRef<HTMLInputElement>(null)
  const panelRef = useRef<HTMLDivElement>(null)
  const restoreRef = useRef<HTMLElement | null>(null)
  const version = useLiveVersion()
  const { t } = useT()

  useEffect(() => {
    // Best-effort: sem os dados a palette ainda abre e ainda oferece o FTS5.
    fetchState()
      .then((s: StateResponse) => setEntries(entriesFrom(s)))
      .catch(() => setEntries([]))
  }, [version])

  // Atalhos globais. Cmd/Ctrl+K é o primário — nunca colide com digitação, e o
  // preventDefault é obrigatório porque o Firefox usa a combinação para a barra
  // de busca. 's' é o secundário, e só vale fora de campo editável.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const shortcut =
        ((e.key === 'k' || e.key === 'K') && (e.metaKey || e.ctrlKey)) ||
        (e.key === 's' && !e.metaKey && !e.ctrlKey && !e.altKey && !editable(e.target))
      if (!shortcut) return
      e.preventDefault()
      restoreRef.current = document.activeElement as HTMLElement | null
      setOpen(true)
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [])

  // Fecha com Esc ou clique fora — o mesmo par de listeners que o Nav usa.
  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false)
    }
    const onDown = (e: MouseEvent) => {
      if (!panelRef.current?.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('keydown', onKey)
    document.addEventListener('mousedown', onDown)
    return () => {
      document.removeEventListener('keydown', onKey)
      document.removeEventListener('mousedown', onDown)
    }
  }, [open])

  // Ao abrir: foco no campo com o termo anterior selecionado (digitar
  // substitui, continuar digitando refina). Ao fechar: foco de volta em quem o
  // tinha, para o teclado não ficar órfão no body.
  useEffect(() => {
    if (open) {
      inputRef.current?.focus()
      inputRef.current?.select()
      return
    }
    restoreRef.current?.focus?.()
  }, [open])

  const results = useMemo(() => rank(entries, query), [entries, query])
  // A linha de escape só aparece quando o filtro local não achou nada: o termo
  // é sobre id/título, e o conteúdo é do FTS5.
  const fallback = query.trim() !== '' && results.length === 0
  const count = fallback ? 1 : results.length

  useEffect(() => setCursor(0), [query])

  const go = (href: string) => {
    window.location.hash = href
    setOpen(false)
  }

  const onInputKey = (e: ReactKeyboardEvent<HTMLInputElement>) => {
    if (count === 0) return
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      setCursor((c) => (c + 1) % count)
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      setCursor((c) => (c - 1 + count) % count)
    } else if (e.key === 'Enter') {
      e.preventDefault()
      if (fallback) go(`#/search/${encodeURIComponent(query.trim())}`)
      else go(results[cursor].href)
    }
  }

  if (!open) return null

  return (
    <div className="palette-overlay">
      <div className="palette-panel" ref={panelRef} role="dialog" aria-modal="true">
        <input
          ref={inputRef}
          className="palette-input"
          role="combobox"
          aria-expanded
          aria-controls="palette-list"
          aria-activedescendant={count > 0 ? `palette-row-${cursor}` : undefined}
          placeholder={t.palette.placeholder}
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={onInputKey}
        />
        <ul className="palette-list" id="palette-list" role="listbox">
          {results.map((e, i) => (
            <li
              key={e.id}
              id={`palette-row-${i}`}
              className="palette-row"
              role="option"
              aria-selected={i === cursor}
              onMouseEnter={() => setCursor(i)}
              onClick={() => go(e.href)}
            >
              <span className="palette-row-id">{e.id}</span>
              <span className="badge">{t.palette.kindLabels[e.kind]}</span>
              <span className="palette-row-title">{e.title}</span>
            </li>
          ))}
          {fallback && (
            <li
              id="palette-row-0"
              className="palette-row palette-fallback"
              role="option"
              aria-selected
              onClick={() => go(`#/search/${encodeURIComponent(query.trim())}`)}
            >
              {t.palette.fullText(query.trim())}
            </li>
          )}
        </ul>
        <p className="palette-hint">{t.palette.hint}</p>
      </div>
    </div>
  )
}
