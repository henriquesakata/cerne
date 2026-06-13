import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'

// Markdown renderiza o conteúdo dos docs (spec/plan/result, decisões e
// referências). O plugin remark-gfm habilita o GitHub Flavored Markdown —
// tabelas, listas de tarefas, ~~tachado~~ e autolinks — que os docs do cerne
// usam bastante; sem ele, tabelas apareceriam como texto cru com pipes.
export default function Markdown({ children }: { children: string }) {
  return <ReactMarkdown remarkPlugins={[remarkGfm]}>{children}</ReactMarkdown>
}
