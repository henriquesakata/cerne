import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'

// stripFrontmatter remove um bloco "---\n…\n---" no início do doc (o cerne
// carimba cerne_id/imported_from ali) para que não seja renderizado como
// conteúdo. No-op quando não há frontmatter (ask/wiki já vêm sem).
function stripFrontmatter(md: string): string {
  const m = /^---\n[\s\S]*?\n---\n?/.exec(md)
  return m ? md.slice(m[0].length).replace(/^\n+/, '') : md
}

// Markdown renderiza o conteúdo dos docs (spec/plan/result, decisões e
// referências). O plugin remark-gfm habilita o GitHub Flavored Markdown —
// tabelas, listas de tarefas, ~~tachado~~ e autolinks — que os docs do cerne
// usam bastante; sem ele, tabelas apareceriam como texto cru com pipes.
export default function Markdown({ children }: { children: string }) {
  return <ReactMarkdown remarkPlugins={[remarkGfm]}>{stripFrontmatter(children)}</ReactMarkdown>
}
