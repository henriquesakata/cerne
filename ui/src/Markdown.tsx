import { useRef, useState, type ComponentProps } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { markerSpans, parseClarification, type MarkerSpan } from './clarify'
import { useT } from './i18n'

// stripFrontmatter remove um bloco "---\n…\n---" no início do doc (o cerne
// carimba cerne_id/imported_from ali) para que não seja renderizado como
// conteúdo. No-op quando não há frontmatter (ask/wiki já vêm sem).
function stripFrontmatter(md: string): string {
  const m = /^---\n[\s\S]*?\n---\n?/.exec(md)
  return m ? md.slice(m[0].length).replace(/^\n+/, '') : md
}

// onAnswer recebe o marcador cru, seu offset no conteúdo ORIGINAL (com
// frontmatter — pronto para resolveMarker) e a resposta do humano.
export type AnswerHandler = (raw: string, offset: number, answer: string) => void

type MdNode = {
  type: string
  value?: string
  children?: MdNode[]
  position?: { start: { offset?: number }; end?: { offset?: number } }
  url?: string
}

// Seg é um filho do container com o intervalo que ele ocupa na FONTE. Fatiar um
// span exige esses offsets: é por eles que se descobre qual pedaço de texto
// sobra fora do marcador.
type Seg = { node: MdNode; start?: number; end?: number }

const startOf = (n: MdNode) => n.position?.start.offset
const endOf = (n: MdNode) => n.position?.end?.offset

// ownedBy decide se ESTE container é quem transforma o span: ele o contém e
// nenhum filho container o contém inteiro (esse filho, mais fundo, seria o
// dono). Garante que cada marcador vire exatamente UM chip, no nível mais
// profundo que ainda o enxerga completo.
function ownedBy(node: MdNode, span: MarkerSpan): boolean {
  const ns = startOf(node)
  const ne = endOf(node)
  if (ns === undefined || ne === undefined) return false
  const end = span.offset + span.raw.length
  if (span.offset < ns || end > ne) return false
  for (const c of node.children ?? []) {
    if (!c.children || c.children.length === 0) continue
    const cs = startOf(c)
    const ce = endOf(c)
    if (cs === undefined || ce === undefined) continue
    if (span.offset >= cs && end <= ce) return false
  }
  return true
}

// spliceSpans troca, na lista de filhos do container, o trecho coberto por cada
// span pelo nó de link do chip. Um filho de texto na borda é FATIADO (o pedaço
// fora do marcador sobrevive); um filho não-texto na borda é absorvido pelo
// chip — caso patológico (o "]" que fecha o marcador caindo dentro de código
// inline ou de um link), decidido na spec da F-0181 em favor de manter chip e
// contador sempre de acordo.
function spliceSpans(children: MdNode[], spans: MarkerSpan[], base: number): MdNode[] {
  const segs: Seg[] = children.map((c) => ({ node: c, start: startOf(c), end: endOf(c) }))
  const out: MdNode[] = []
  let i = 0
  for (const span of spans) {
    const end = span.offset + span.raw.length
    // Filhos que terminam antes do marcador saem intactos.
    while (i < segs.length) {
      const s = segs[i]
      if (s.end !== undefined && s.end > span.offset) break
      out.push(s.node)
      i++
    }
    if (i >= segs.length) break
    // Pedaço de texto ANTES do marcador, dentro do filho onde ele começa.
    const first = segs[i]
    if (
      first.node.type === 'text' &&
      first.node.value !== undefined &&
      first.start !== undefined &&
      span.offset > first.start
    ) {
      out.push({ type: 'text', value: first.node.value.slice(0, span.offset - first.start) })
    }
    out.push({
      type: 'link',
      url: `#clarify:${base + span.offset}`,
      children: [{ type: 'text', value: span.raw }],
    })
    // Consome o que o marcador cobre e trata o filho da borda final.
    while (i < segs.length) {
      const s = segs[i]
      if (s.end === undefined || s.start === undefined) {
        i++
        continue
      }
      if (s.end <= end) {
        i++
        continue
      }
      if (s.start >= end) break
      if (s.node.type === 'text' && s.node.value !== undefined) {
        // Sobra do texto: vira o novo segmento corrente, para que um segundo
        // marcador no MESMO nó de texto ainda seja encontrado.
        segs[i] = {
          node: { type: 'text', value: s.node.value.slice(end - s.start) },
          start: end,
          end: s.end,
        }
      } else {
        i++
      }
      break
    }
  }
  while (i < segs.length) {
    out.push(segs[i].node)
    i++
  }
  return out
}

// remarkClarify transforma cada marcador [ESCLARECER]/[ESCLARECIDO] em um nó de
// link `#clarify:<offset>` cujo rótulo é o marcador cru — o override de `a`
// abaixo o renderiza como chip. Os spans vêm de markerSpans, que lê a FONTE: é
// a mesma função que alimenta o contador do badge, então o que se conta e o que
// se renderiza não podem divergir (F-0181). Casar na fonte é o que faz funcionar
// o marcador cujo corpo tem markdown inline — ele atravessa vários nós do mdast
// e nenhum nó de texto o contém inteiro. `base` recoloca o offset no conteúdo
// original (frontmatter removido antes do parse).
function remarkClarify(options: { base: number; source: string }) {
  const spans = markerSpans(options.source)
  const walk = (node: MdNode) => {
    if (!node.children || node.children.length === 0) return
    for (const child of node.children) walk(child)
    const mine = spans.filter((s) => ownedBy(node, s))
    if (mine.length > 0) node.children = spliceSpans(node.children, mine, options.base)
  }
  return (tree: MdNode) => {
    if (spans.length > 0) walk(tree)
  }
}

// ClarifyChip renderiza um marcador: aberto vira chip âmbar (clicável quando
// há onAnswer — o popover oferece as opções propostas pelo agente e um campo
// livre, no modelo AskUserQuestion); resolvido vira chip verde estático com o
// rastro pergunta → resposta.
function ClarifyChip({
  raw,
  offset,
  onAnswer,
  onCorrect,
}: {
  raw: string
  offset: number
  onAnswer?: AnswerHandler
  onCorrect?: AnswerHandler
}) {
  const { t } = useT()
  const [open, setOpen] = useState(false)
  const [text, setText] = useState('')
  const info = parseClarification(raw)
  if (!info) return <>{raw}</>
  const submit = (answer: string) => {
    // Aberto vai pelo onAnswer; resolvido, pelo onCorrect — dois caminhos
    // distintos de propósito (responder resolve, corrigir anexa à trilha).
    const handler = info.resolved ? onCorrect : onAnswer
    handler?.(raw, offset, answer)
    setOpen(false)
    setText('')
  }
  if (info.resolved) {
    const trail = info.question ? `${info.question} → ${info.answers.join(' → ')}` : info.answers.join(' → ')
    // Sem onCorrect (páginas somente-leitura) o chip segue estático.
    if (!onCorrect) {
      return (
        <span className="clarify-chip clarify-resolved" title={raw}>
          ✓ {trail}
        </span>
      )
    }
    return (
      <span className="clarify-anchor">
        <button
          type="button"
          className="clarify-chip clarify-resolved"
          title={t.clarify.correctTitle}
          onClick={() => {
            setText(info.answer)
            setOpen(!open)
          }}
        >
          ✓ {trail}
        </button>
        {open && (
          <span className="clarify-pop" role="dialog">
            <span className="clarify-pop-question">{info.question || t.clarify.noQuestion}</span>
            <span className="clarify-pop-current">{t.clarify.currentAnswer(info.answer)}</span>
            <textarea
              className="clarify-free"
              rows={3}
              placeholder={t.clarify.correctPlaceholder}
              value={text}
              onChange={(e) => setText(e.target.value)}
            />
            <span className="clarify-pop-actions">
              <button
                type="button"
                disabled={text.trim() === '' || text.trim() === info.answer}
                onClick={() => submit(text.trim())}
              >
                {t.clarify.confirm}
              </button>
            </span>
          </span>
        )}
      </span>
    )
  }
  const label = info.question || t.clarify.noQuestion
  if (!onAnswer) {
    return <span className="clarify-chip clarify-open">? {label}</span>
  }
  return (
    <span className="clarify-anchor">
      <button
        type="button"
        className="clarify-chip clarify-open"
        title={t.clarify.chipTitle}
        onClick={() => setOpen(!open)}
      >
        ? {label}
      </button>
      {open && (
        <span className="clarify-pop" role="dialog">
          <span className="clarify-pop-question">{label}</span>
          {info.options.map((o) => (
            <button type="button" key={o} className="clarify-option" onClick={() => submit(o)}>
              {o}
            </button>
          ))}
          <textarea
            className="clarify-free"
            rows={3}
            placeholder={info.options.length > 0 ? t.clarify.otherAnswer : t.clarify.answerPlaceholder}
            value={text}
            onChange={(e) => setText(e.target.value)}
          />
          <span className="clarify-pop-actions">
            <button type="button" disabled={text.trim() === ''} onClick={() => submit(text.trim())}>
              {t.clarify.confirm}
            </button>
            <button type="button" onClick={() => setOpen(false)}>
              {t.common.cancel}
            </button>
          </span>
        </span>
      )}
    </span>
  )
}

// CodeBlock (F-0247) envolve todo bloco cercado com um botão de copiar. O
// texto sai do textContent do <pre> renderizado — é o conteúdo do <code>, já
// sem a cerca e sem o rótulo de linguagem (critério 2), sem precisar recuperar
// offsets da fonte. Botão sempre visível: afordância só no hover não existe
// para teclado nem toque, e este bloco existe para ser copiado.
function CodeBlock({ children, ...rest }: ComponentProps<'pre'>) {
  const [copied, setCopied] = useState(false)
  const ref = useRef<HTMLPreElement>(null)
  const { t } = useT()

  // O `?.` curto-circuita a cadeia inteira, então clipboard ausente (contexto
  // inseguro) não lança; o catch cobre permissão negada. Em nenhum dos dois
  // casos o estado de confirmação liga (critério 6).
  const copy = () => {
    navigator.clipboard
      ?.writeText(ref.current?.textContent ?? '')
      .then(() => {
        setCopied(true)
        setTimeout(() => setCopied(false), 2000)
      })
      .catch(() => {})
  }

  return (
    <div className="code-block">
      <pre ref={ref} {...rest}>
        {children}
      </pre>
      <button type="button" className="code-copy" aria-label={t.common.copyBlock} onClick={copy}>
        {copied ? t.common.copied : t.common.copy}
      </button>
    </div>
  )
}

// Markdown renderiza o conteúdo dos docs (spec/plan/result, decisões e
// referências). O plugin remark-gfm habilita o GitHub Flavored Markdown —
// tabelas, listas de tarefas, ~~tachado~~ e autolinks — que os docs do cerne
// usam bastante; sem ele, tabelas apareceriam como texto cru com pipes.
// Marcadores [ESCLARECER] viram chips (F-0175); onAnswer, quando fornecido
// (página da feature), torna o chip respondível inline.
export default function Markdown({
  children,
  onAnswer,
  onCorrect,
}: {
  children: string
  onAnswer?: AnswerHandler
  onCorrect?: AnswerHandler
}) {
  const stripped = stripFrontmatter(children)
  const base = children.length - stripped.length
  return (
    <ReactMarkdown
      remarkPlugins={[remarkGfm, [remarkClarify, { base, source: stripped }]]}
      components={{
        pre: CodeBlock,
        a: ({ href, children: label, ...rest }) => {
          if (href?.startsWith('#clarify:')) {
            const raw = Array.isArray(label) ? label.join('') : String(label ?? '')
            return (
              <ClarifyChip
                raw={raw}
                offset={Number(href.slice('#clarify:'.length))}
                onAnswer={onAnswer}
                onCorrect={onCorrect}
              />
            )
          }
          return (
            <a href={href} {...rest}>
              {label}
            </a>
          )
        },
      }}
    >
      {stripped}
    </ReactMarkdown>
  )
}
