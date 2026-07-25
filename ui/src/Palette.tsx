// Palette (F-0196): o spotlight da UI — abre por atalho, filtra entidades por
// id ou título em memória e salta para a página de detalhe. É deliberadamente
// SEM rede a cada tecla: o /state que o board já consome tem id e título de
// tudo, e salto precisa ser instantâneo. Busca no CONTEÚDO continua sendo do
// FTS5 (#/search) — a última linha da lista entrega o usuário lá.

import type { StateResponse } from './api'

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
function norm(s: string): string {
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
