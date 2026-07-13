import { useState } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { CLARIFY_RE, parseClarification } from './clarify'
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
  position?: { start: { offset?: number } }
  url?: string
}

// remarkClarify transforma cada marcador [ESCLARECER]/[ESCLARECIDO] achado em
// nós de TEXTO do mdast em um nó de link `#clarify:<offset>` cujo rótulo é o
// marcador cru — o override de `a` abaixo o renderiza como chip. Operar no
// mdast (e não no string) pula de graça código inline/cercado e comentários
// HTML, que não são nós de texto. `base` recoloca o offset no conteúdo
// original (frontmatter removido antes do parse).
function remarkClarify(options: { base: number }) {
  const walk = (node: MdNode) => {
    if (!node.children) return
    const next: MdNode[] = []
    for (const child of node.children) {
      if (child.type !== 'text' || !child.value || child.position?.start.offset === undefined) {
        walk(child)
        next.push(child)
        continue
      }
      const start = child.position.start.offset
      let last = 0
      const parts: MdNode[] = []
      for (const m of child.value.matchAll(CLARIFY_RE)) {
        if (m.index > last) parts.push({ type: 'text', value: child.value.slice(last, m.index) })
        parts.push({
          type: 'link',
          url: `#clarify:${options.base + start + m.index}`,
          children: [{ type: 'text', value: m[0] }],
        })
        last = m.index + m[0].length
      }
      if (parts.length === 0) {
        next.push(child)
        continue
      }
      if (last < child.value.length) parts.push({ type: 'text', value: child.value.slice(last) })
      next.push(...parts)
    }
    node.children = next
  }
  return (tree: MdNode) => walk(tree)
}

// ClarifyChip renderiza um marcador: aberto vira chip âmbar (clicável quando
// há onAnswer — o popover oferece as opções propostas pelo agente e um campo
// livre, no modelo AskUserQuestion); resolvido vira chip verde estático com o
// rastro pergunta → resposta.
function ClarifyChip({
  raw,
  offset,
  onAnswer,
}: {
  raw: string
  offset: number
  onAnswer?: AnswerHandler
}) {
  const { t } = useT()
  const [open, setOpen] = useState(false)
  const [text, setText] = useState('')
  const info = parseClarification(raw)
  if (!info) return <>{raw}</>
  if (info.resolved) {
    return (
      <span className="clarify-chip clarify-resolved" title={raw}>
        ✓ {info.question ? `${info.question} → ${info.answer}` : info.answer}
      </span>
    )
  }
  const label = info.question || t.clarify.noQuestion
  if (!onAnswer) {
    return <span className="clarify-chip clarify-open">? {label}</span>
  }
  const submit = (answer: string) => {
    onAnswer(raw, offset, answer)
    setOpen(false)
    setText('')
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

// Markdown renderiza o conteúdo dos docs (spec/plan/result, decisões e
// referências). O plugin remark-gfm habilita o GitHub Flavored Markdown —
// tabelas, listas de tarefas, ~~tachado~~ e autolinks — que os docs do cerne
// usam bastante; sem ele, tabelas apareceriam como texto cru com pipes.
// Marcadores [ESCLARECER] viram chips (F-0175); onAnswer, quando fornecido
// (página da feature), torna o chip respondível inline.
export default function Markdown({
  children,
  onAnswer,
}: {
  children: string
  onAnswer?: AnswerHandler
}) {
  const stripped = stripFrontmatter(children)
  const base = children.length - stripped.length
  return (
    <ReactMarkdown
      remarkPlugins={[remarkGfm, [remarkClarify, { base }]]}
      components={{
        a: ({ href, children: label, ...rest }) => {
          if (href?.startsWith('#clarify:')) {
            const raw = Array.isArray(label) ? label.join('') : String(label ?? '')
            return <ClarifyChip raw={raw} offset={Number(href.slice('#clarify:'.length))} onAnswer={onAnswer} />
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
