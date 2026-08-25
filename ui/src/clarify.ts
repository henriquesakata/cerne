// clarify espelha internal/clarify no lado do browser: reconhece os
// marcadores de esclarecimento deixados por agentes nos docs (catálogo por
// idioma — pt [ESCLARECER]/[ESCLARECIDO], en [CLARIFY]/[CLARIFIED]; F-0182) e
// o rastro resolvido [ESCLARECIDO: pergunta → resposta]. O parse é duplicado
// de propósito (decisão da spec F-0175): um endpoint de spans acoplaria o
// render da UI ao servidor. A sincronia com internal/clarify/clarify.go é
// travada pelo corpus compartilhado (internal/clarify/testdata/corpus.json),
// lido pelos testes dos dois lados.

export type ClarifyInfo = {
  question: string
  options: string[]
  resolved: boolean
  // answers é a trilha de respostas em ordem cronológica (F-0197); answer é a
  // corrente (a última). Marcador escrito antes das correções é trilha de um
  // elemento, então nenhum documento precisa ser migrado.
  answers: string[]
  answer: string
}

// Catálogo de vocabulários por idioma (F-0182), espelho da tabela em
// internal/clarify/clarify.go. Conjunto fechado: regex aberto engoliria prosa
// entre colchetes. Idioma novo = uma linha aqui, a irmã no Go e uma fixture
// no corpus compartilhado (o teste de paridade quebra o lado que atrasar).
const VOCABS = [
  { open: 'ESCLARECER', resolved: 'ESCLARECIDO' },
  { open: 'CLARIFY', resolved: 'CLARIFIED' },
]

// Palavras em ordem de tamanho (maior primeiro) para que uma palavra que é
// prefixo de outra (CLARIFY / CLARIFIED) nunca sombreie a forma longa.
const MARKER_WORDS = VOCABS.flatMap((v) => [v.open, v.resolved]).sort(
  (a, b) => b.length - a.length,
)
const RESOLVED_WORDS = new Set(VOCABS.map((v) => v.resolved))

// Mesma forma do Go: palavra do catálogo com guilhemets legados opcionais e
// corpo opcional de linha única (sem ] nem quebra), o que também exclui
// prefixos como [ESCLARECEDOR] e [CLARIFIER].
export const CLARIFY_RE = new RegExp(
  `\\[«?(${MARKER_WORDS.join('|')})»?(?::[ \\t]*([^\\]\\n]*))?\\]`,
  'g',
)

// Contextos de citação: marcador dentro deles é o doc FALANDO da convenção,
// não um esclarecimento autorado. Comentários HTML carregam o guia dos
// templates (aberto engole até o fim); fence e código inline são como a prosa
// cita a sintaxe — os mesmos contextos que o render markdown não transforma.
const COMMENT_RE = /<!--[\s\S]*?(-->|$)/g
const FENCED_RE = /^```[^\n]*\n[\s\S]*?^```/gm
const INLINE_CODE_RE = /`[^`\n]+`/g

// Dentro de uma tabela GFM um pipe cru é separador de CÉLULA: o parser fatia
// um marcador com opções em várias células antes de qualquer lógica de chip,
// e sobra texto cru na célula, sem como responder (F-0282). A saída é o escape
// do próprio markdown — numa tabela os pipes de opção do marcador são escritos
// `\|`, o que mantém a célula inteira. Logo a forma escapada separa opções
// exatamente como a crua, e a barra invertida cai na leitura.
//
// Na forma RESOLVIDA não há opções — o corpo é partido só pela seta — então ali
// `\|` é pipe literal: o parse desescapa e resolveMarker/correctMarker escapam
// na escrita, que é o que impede uma resposta digitada com pipe de corromper a
// tabela em que vive. Espelho de unescapePipe/escapePipe no Go.
// A barra invertida escapa a si mesma além do pipe, numa passada só: sem isso,
// uma resposta digitada como barra invertida seguida de pipe seria gravada com
// a barra escapando a barra, e o GFM leria o pipe seguinte como separador de
// célula — exatamente a corrupção que o escape existe para impedir.
const unescapePipe = (s: string) => s.replace(/\\([\\|])/g, '$1')
const escapePipe = (s: string) => s.replace(/[\\|]/g, (c) => '\\' + c)

// parseClarification decompõe um marcador cru; null quando não é marcador.
export function parseClarification(raw: string): ClarifyInfo | null {
  const m = new RegExp(CLARIFY_RE.source).exec(raw)
  if (!m || m[0] !== raw) return null
  const body = (m[2] ?? '').trim()
  if (RESOLVED_WORDS.has(m[1])) {
    // A pergunta é o que precede a PRIMEIRA seta; o resto é a trilha, cada
    // correção anexada com a mesma seta (F-0197). Sem seta alguma, o corpo
    // inteiro é a única resposta — a forma legada.
    const arrow = body.indexOf('→')
    const question = arrow >= 0 ? unescapePipe(body.slice(0, arrow).trim()) : ''
    const rest = arrow >= 0 ? body.slice(arrow + 1) : body
    const answers = rest.split('→').map((s) => unescapePipe(s.trim()))
    return {
      question,
      options: [],
      resolved: true,
      answers,
      answer: answers[answers.length - 1],
    }
  }
  const parts = unescapePipe(body).split('|')
  return {
    question: parts[0].trim(),
    options: parts
      .slice(1)
      .map((p) => p.trim())
      .filter((p) => p !== ''),
    resolved: false,
    answers: [],
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
    out.push({ raw: m[0], offset: m.index, resolved: RESOLVED_WORDS.has(m[1]) })
  }
  return out
}

// countOpen conta os marcadores abertos fora de contextos de citação —
// alimenta o badge das abas de doc (critério 9 da F-0175).
export function countOpen(content: string): number {
  return markerSpans(content).filter((s) => !s.resolved).length
}

// resolveMarker substitui UMA ocorrência do marcador pela forma resolvida —
// no MESMO vocabulário do marcador aberto ([CLARIFY] vira [CLARIFIED], nunca
// [ESCLARECIDO]; F-0182) — ancorada no offset exato para não tocar
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
  const word = new RegExp(CLARIFY_RE.source).exec(raw)?.[1]
  const vocab = VOCABS.find((v) => v.open === word)
  if (!vocab) return null
  const resolved = info.question
    ? `[${vocab.resolved}: ${escapePipe(info.question)} → ${escapePipe(answer)}]`
    : `[${vocab.resolved}: ${escapePipe(answer)}]`
  return content.slice(0, offset) + resolved + content.slice(offset + raw.length)
}

// correctMarker substitui UMA ocorrência de marcador RESOLVIDO pela forma com
// a nova resposta anexada à trilha — o espelho de clarify.Correct no Go
// (F-0197). Corrigir não apaga o que foi corrigido: o doc carrega a resposta
// antiga junto da nova, corrente por último. Devolve null quando o offset não
// bate com raw (conteúdo mudou sob os pés) ou quando o marcador ainda está
// aberto — responder é trabalho do resolveMarker.
export function correctMarker(
  content: string,
  raw: string,
  offset: number,
  answer: string,
): string | null {
  if (content.slice(offset, offset + raw.length) !== raw) return null
  const info = parseClarification(raw)
  if (!info || !info.resolved) return null
  const word = new RegExp(CLARIFY_RE.source).exec(raw)?.[1]
  if (!word || !RESOLVED_WORDS.has(word)) return null
  const trail = [...info.answers, answer].map(escapePipe).join(' → ')
  // Sem pergunta, a trilha ainda precisa da seta inicial: sem ela, a primeira
  // resposta seria reparseada como pergunta.
  const corrected = info.question
    ? `[${word}: ${escapePipe(info.question)} → ${trail}]`
    : `[${word}: → ${trail}]`
  return content.slice(0, offset) + corrected + content.slice(offset + raw.length)
}
