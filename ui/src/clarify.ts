// clarify espelha internal/clarify no lado do browser: reconhece os
// marcadores [ESCLARECER] deixados por agentes nos docs e o rastro resolvido
// [ESCLARECIDO: pergunta → resposta]. O parse é duplicado de propósito
// (decisão da spec F-0175): um endpoint de spans acoplaria o render da UI ao
// servidor. Mantenha o regex em sincronia com internal/clarify/clarify.go.

export type ClarifyInfo = {
  question: string
  options: string[]
  resolved: boolean
  answer: string
}

// Mesma forma do Go: palavra com guilhemets legados opcionais e corpo
// opcional de linha única (sem ] nem quebra), o que também exclui prefixos
// como [ESCLARECEDOR].
export const CLARIFY_RE = /\[«?(ESCLARECER|ESCLARECIDO)»?(?::[ \t]*([^\]\n]*))?\]/g

// Contextos de citação: marcador dentro deles é o doc FALANDO da convenção,
// não um esclarecimento autorado. Comentários HTML carregam o guia dos
// templates (aberto engole até o fim); fence e código inline são como a prosa
// cita a sintaxe — os mesmos contextos que o render markdown não transforma.
const COMMENT_RE = /<!--[\s\S]*?(-->|$)/g
const FENCED_RE = /^```[^\n]*\n[\s\S]*?^```/gm
const INLINE_CODE_RE = /`[^`\n]+`/g

// parseClarification decompõe um marcador cru; null quando não é marcador.
export function parseClarification(raw: string): ClarifyInfo | null {
  const m = new RegExp(CLARIFY_RE.source).exec(raw)
  if (!m || m[0] !== raw) return null
  const body = (m[2] ?? '').trim()
  if (m[1] === 'ESCLARECIDO') {
    const arrow = body.indexOf('→')
    if (arrow >= 0) {
      return {
        question: body.slice(0, arrow).trim(),
        options: [],
        resolved: true,
        answer: body.slice(arrow + 1).trim(),
      }
    }
    return { question: '', options: [], resolved: true, answer: body }
  }
  const parts = body.split('|')
  return {
    question: parts[0].trim(),
    options: parts
      .slice(1)
      .map((p) => p.trim())
      .filter((p) => p !== ''),
    resolved: false,
    answer: '',
  }
}

// citationRanges devolve os intervalos [início, fim) de todos os contextos de
// citação (comentários, fences, código inline).
function citationRanges(content: string): Array<[number, number]> {
  const ranges: Array<[number, number]> = []
  for (const re of [COMMENT_RE, FENCED_RE, INLINE_CODE_RE]) {
    for (const m of content.matchAll(re)) {
      ranges.push([m.index, m.index + m[0].length])
    }
  }
  return ranges
}

// MarkerSpan é uma ocorrência de marcador no conteúdo: o texto cru e o offset
// que o ancora (o que o resolveMarker exige para reescrever só ela).
export type MarkerSpan = { raw: string; offset: number; resolved: boolean }

// markerSpans devolve todos os marcadores fora de contexto de citação, em
// ordem de documento. É a FONTE ÚNICA dos spans no browser: o contador do
// badge e o render do Markdown consomem esta mesma função, para que o que se
// conta e o que se renderiza não possam divergir (F-0181 — antes o render
// casava o regex nó a nó do mdast e perdia marcador que atravessa nós inline).
// Espelha internal/clarify.Parse: cita quem COMEÇA dentro da citação.
export function markerSpans(content: string): MarkerSpan[] {
  const citations = citationRanges(content)
  const out: MarkerSpan[] = []
  for (const m of content.matchAll(CLARIFY_RE)) {
    if (citations.some(([a, b]) => m.index >= a && m.index < b)) continue
    out.push({ raw: m[0], offset: m.index, resolved: m[1] === 'ESCLARECIDO' })
  }
  return out
}

// countOpen conta os marcadores abertos fora de contextos de citação —
// alimenta o badge das abas de doc (critério 9 da F-0175).
export function countOpen(content: string): number {
  return markerSpans(content).filter((s) => !s.resolved).length
}

// resolveMarker substitui UMA ocorrência do marcador pela forma resolvida
// [ESCLARECIDO: pergunta → resposta], ancorada no offset exato para não tocar
// duplicatas. Devolve null se o offset não bate com raw (conteúdo mudou sob
// os pés — recarregue em vez de salvar errado).
export function resolveMarker(
  content: string,
  raw: string,
  offset: number,
  answer: string,
): string | null {
  if (content.slice(offset, offset + raw.length) !== raw) return null
  const info = parseClarification(raw)
  if (!info || info.resolved) return null
  const resolved = info.question
    ? `[ESCLARECIDO: ${info.question} → ${answer}]`
    : `[ESCLARECIDO: ${answer}]`
  return content.slice(0, offset) + resolved + content.slice(offset + raw.length)
}
