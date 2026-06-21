import { useProjects } from './live'

// Barra de navegação persistente: o shell que torna as páginas alcançáveis.
const LINKS: { href: string; label: string }[] = [
  { href: '#/', label: 'Kanban' },
  { href: '#/manage', label: 'Gestão' },
  { href: '#/decisions', label: 'Decisões' },
  { href: '#/references', label: 'Referências' },
  { href: '#/runbooks', label: 'Runbooks' },
  { href: '#/wiki', label: 'Wiki' },
  { href: '#/graph', label: 'Grafo' },
  { href: '#/ask', label: 'Perguntar' },
  { href: '#/search', label: 'Busca' },
  { href: '#/lint', label: 'Lint' },
  { href: '#/history', label: 'Histórico' },
]

// activeHref mapeia a rota atual para o link de seção que deve ficar ativo.
// Rotas de detalhe (#/f, #/d, #/r) destacam a seção pai.
function activeHref(hash: string): string {
  if (hash.startsWith('#/manage') || hash.startsWith('#/new')) return '#/manage'
  if (hash.startsWith('#/decisions') || hash.startsWith('#/d/')) return '#/decisions'
  if (hash.startsWith('#/references') || hash.startsWith('#/r/')) return '#/references'
  if (hash.startsWith('#/runbooks') || hash.startsWith('#/rb/')) return '#/runbooks'
  if (hash.startsWith('#/wiki')) return '#/wiki'
  if (hash.startsWith('#/graph')) return '#/graph'
  if (hash.startsWith('#/ask')) return '#/ask'
  if (hash.startsWith('#/search')) return '#/search'
  if (hash.startsWith('#/lint')) return '#/lint'
  if (hash.startsWith('#/history')) return '#/history'
  return '#/' // kanban e detalhe de feature (#/f/<id>)
}

export default function Nav({ hash }: { hash: string }) {
  const active = activeHref(hash)
  return (
    <nav className="nav">
      <span className="nav-brand">cerne</span>
      <div className="nav-links">
        {LINKS.map((l) => (
          <a
            key={l.href}
            href={l.href}
            className={l.href === active ? 'nav-link active' : 'nav-link'}
          >
            {l.label}
          </a>
        ))}
      </div>
      <ProjectSelector />
    </nav>
  )
}

// ProjectSelector mostra o projeto ativo no canto direito: com 1 projeto, só o
// nome; com vários, um seletor (com "•" nos que receberam mudanças). Trocar o
// projeto re-busca as telas e limpa o badge do escolhido.
function ProjectSelector() {
  const { projects, activePid, setActivePid, changed } = useProjects()
  if (projects.length <= 1) {
    const only = projects[0]
    return (
      <span className="nav-project-wrap">
        <span className="nav-project-label">projeto</span>
        <span className="nav-project-name">{only ? only.name : '—'}</span>
      </span>
    )
  }
  return (
    <span className="nav-project-wrap">
      <span className="nav-project-label">projeto</span>
      <select
        className="nav-project-select"
        value={activePid}
        onChange={(e) => setActivePid(e.target.value)}
      >
        {projects.map((p) => (
          <option key={p.pid} value={p.pid}>
            {changed.has(p.pid) ? '• ' : ''}
            {p.name}
          </option>
        ))}
      </select>
    </span>
  )
}
