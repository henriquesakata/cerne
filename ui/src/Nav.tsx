import { useProjects } from './live'
import { useTheme } from './theme'
import { useT, type Lang, type Messages } from './i18n'

// Barra de navegação persistente: o shell que torna as páginas alcançáveis.
const LINKS: { href: string; key: keyof Messages['nav'] }[] = [
  { href: '#/', key: 'kanban' },
  { href: '#/manage', key: 'manage' },
  { href: '#/decisions', key: 'decisions' },
  { href: '#/references', key: 'references' },
  { href: '#/runbooks', key: 'runbooks' },
  { href: '#/notes', key: 'notes' },
  { href: '#/wiki', key: 'wiki' },
  { href: '#/graph', key: 'graph' },
  { href: '#/ask', key: 'ask' },
  { href: '#/search', key: 'search' },
  { href: '#/lint', key: 'lint' },
  { href: '#/history', key: 'history' },
]

// activeHref mapeia a rota atual para o link de seção que deve ficar ativo.
// Rotas de detalhe (#/f, #/d, #/r) destacam a seção pai.
function activeHref(hash: string): string {
  if (hash.startsWith('#/manage') || hash.startsWith('#/new')) return '#/manage'
  if (hash.startsWith('#/decisions') || hash.startsWith('#/d/')) return '#/decisions'
  if (hash.startsWith('#/references') || hash.startsWith('#/r/')) return '#/references'
  if (hash.startsWith('#/runbooks') || hash.startsWith('#/rb/')) return '#/runbooks'
  if (hash.startsWith('#/notes') || hash.startsWith('#/n/')) return '#/notes'
  if (hash.startsWith('#/wiki')) return '#/wiki'
  if (hash.startsWith('#/graph')) return '#/graph'
  if (hash.startsWith('#/ask')) return '#/ask'
  if (hash.startsWith('#/search')) return '#/search'
  if (hash.startsWith('#/lint')) return '#/lint'
  if (hash.startsWith('#/history')) return '#/history'
  return '#/' // kanban e detalhe de feature (#/f/<id>)
}

export default function Nav({ hash }: { hash: string }) {
  const { t } = useT()
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
            {t.nav[l.key]}
          </a>
        ))}
      </div>
      <ProjectSelector />
      <LangToggle />
      <ThemeToggle />
    </nav>
  )
}

// Alterna claro/escuro; a escolha persiste em localStorage (F-0126).
function ThemeToggle() {
  const { t } = useT()
  const [theme, setTheme] = useTheme()
  const dark = theme === 'dark'
  return (
    <button
      type="button"
      className="theme-toggle"
      title={dark ? t.nav.themeLight : t.nav.themeDark}
      aria-label={dark ? t.nav.themeLight : t.nav.themeDark}
      onClick={() => setTheme(dark ? 'light' : 'dark')}
    >
      {dark ? '☀' : '🌙'}
    </button>
  )
}

// Seletor compacto de idioma (PT | EN); a escolha persiste em localStorage (F-0125).
function LangToggle() {
  const { t, lang, setLang } = useT()
  const opts: { value: Lang; label: string }[] = [
    { value: 'pt-BR', label: 'PT' },
    { value: 'en', label: 'EN' },
  ]
  return (
    <span className="lang-toggle" role="group" aria-label={t.nav.language}>
      {opts.map((o) => (
        <button
          key={o.value}
          type="button"
          className={o.value === lang ? 'lang-option active' : 'lang-option'}
          aria-pressed={o.value === lang}
          onClick={() => setLang(o.value)}
        >
          {o.label}
        </button>
      ))}
    </span>
  )
}

// ProjectSelector mostra o projeto ativo no canto direito: com 1 projeto, só o
// nome; com vários, um seletor (com "•" nos que receberam mudanças). Trocar o
// projeto re-busca as telas e limpa o badge do escolhido.
function ProjectSelector() {
  const { t } = useT()
  const { projects, activePid, setActivePid, changed } = useProjects()
  if (projects.length <= 1) {
    const only = projects[0]
    return (
      <span className="nav-project-wrap">
        <span className="nav-project-label">{t.nav.project}</span>
        <span className="nav-project-name">{only ? only.name : '—'}</span>
      </span>
    )
  }
  return (
    <span className="nav-project-wrap">
      <span className="nav-project-label">{t.nav.project}</span>
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
