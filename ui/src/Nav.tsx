// Barra de navegação persistente: o shell que torna as páginas alcançáveis.
const LINKS: { href: string; label: string }[] = [
  { href: '#/', label: 'Kanban' },
  { href: '#/manage', label: 'Gestão' },
  { href: '#/decisions', label: 'Decisões' },
  { href: '#/references', label: 'Referências' },
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
    </nav>
  )
}
