import { useEffect, useRef, useState } from 'react'
import { useProjects } from './live'
import { useTheme } from './theme'
import { useT, type Lang, type Messages } from './i18n'
import { fetchVersion } from './api'
import NotificationBell from './NotificationBell'

type NavItem = { href: string; key: keyof Messages['nav'] }

// Barra de navegação persistente (F-0332): o Dashboard solto, por ser a tela
// inicial, e o resto em quatro grupos por assunto, cada um um menu por clique.
// Cinco controles cabem em qualquer largura, então cada tela tem lugar fixo.
const DASHBOARD: NavItem = { href: '#/', key: 'dashboard' }
const GROUPS: { key: keyof Messages['nav']; items: NavItem[] }[] = [
  {
    key: 'groupWork',
    items: [
      { href: '#/kanban', key: 'kanban' },
      { href: '#/backlog', key: 'backlog' },
      { href: '#/pendings', key: 'pendings' },
      { href: '#/ideas', key: 'ideas' },
      { href: '#/manage', key: 'manage' },
      { href: '#/rooms', key: 'warRoom' },
      { href: '#/activity', key: 'activity' },
      { href: '#/handoffs', key: 'handoffs' },
    ],
  },
  {
    key: 'groupKnowledge',
    items: [
      { href: '#/decisions', key: 'decisions' },
      { href: '#/references', key: 'references' },
      { href: '#/runbooks', key: 'runbooks' },
      { href: '#/notes', key: 'notes' },
      { href: '#/conventions', key: 'conventions' },
    ],
  },
  {
    key: 'groupExplore',
    items: [
      { href: '#/wiki', key: 'wiki' },
      { href: '#/ask', key: 'ask' },
      { href: '#/search', key: 'search' },
      { href: '#/graph', key: 'graph' },
      { href: '#/mindmap', key: 'mindmap' },
    ],
  },
  {
    key: 'groupHealth',
    items: [
      { href: '#/lint', key: 'lint' },
      { href: '#/harness', key: 'harness' },
      { href: '#/history', key: 'history' },
      { href: '#/stats', key: 'stats' },
      { href: '#/costs', key: 'costs' },
    ],
  },
]

// activeHref mapeia a rota atual para o link de seção que deve ficar ativo.
// Rotas de detalhe (#/f, #/d, #/r) destacam a seção pai.
function activeHref(hash: string): string {
  // Kanban e detalhe de feature destacam Kanban; '#/' (dashboard) é o fallback.
  if (hash.startsWith('#/kanban') || hash.startsWith('#/f/')) return '#/kanban'
  if (hash.startsWith('#/backlog')) return '#/backlog'
  // The same rule App routes them by (F-0334): anything else is the dashboard.
  if (hash === '#/pendings' || hash.startsWith('#/pendings?')) return '#/pendings'
  if (hash === '#/ideas' || hash.startsWith('#/ideas?')) return '#/ideas'
  if (hash.startsWith('#/rooms')) return '#/rooms'
  if (hash.startsWith('#/manage') || hash.startsWith('#/new')) return '#/manage'
  if (hash.startsWith('#/decisions') || hash.startsWith('#/d/')) return '#/decisions'
  if (hash.startsWith('#/references') || hash.startsWith('#/r/')) return '#/references'
  if (hash.startsWith('#/runbooks') || hash.startsWith('#/rb/')) return '#/runbooks'
  if (hash.startsWith('#/notes') || hash.startsWith('#/n/')) return '#/notes'
  if (hash.startsWith('#/conventions')) return '#/conventions'
  if (hash.startsWith('#/wiki')) return '#/wiki'
  if (hash.startsWith('#/graph')) return '#/graph'
  if (hash.startsWith('#/mindmap')) return '#/mindmap'
  if (hash.startsWith('#/ask')) return '#/ask'
  if (hash.startsWith('#/search')) return '#/search'
  if (hash.startsWith('#/lint')) return '#/lint'
  if (hash.startsWith('#/harness')) return '#/harness'
  if (hash.startsWith('#/history')) return '#/history'
  if (hash.startsWith('#/activity')) return '#/activity'
  // The same rule App routes them by (F-0386): list, filtered list, detail.
  if (/^#\/handoffs($|[?/])/.test(hash)) return '#/handoffs'
  if (hash.startsWith('#/stats')) return '#/stats'
  // The same rule App routes them by (F-0391): area, filtered area, detail.
  if (/^#\/costs($|[?/])/.test(hash)) return '#/costs'
  if (hash.startsWith('#/settings')) return '#/settings' // engrenagem, fora dos grupos
  return '#/' // dashboard (tela inicial, F-0132)
}

export default function Nav({ hash }: { hash: string }) {
  const active = activeHref(hash)
  return (
    <nav className="nav">
      <span className="nav-brand">
        cerne
        <NavVersion />
      </span>
      <NavLinks active={active} hash={hash} />
      <ProjectSelector />
      <SettingsLink active={active === '#/settings'} />
      <NotificationBell />
      <LangToggle />
      <ThemeToggle />
    </nav>
  )
}

// NavLinks renderiza o Dashboard e um botão por grupo; o clique abre o menu do
// grupo, no máximo um por vez. O grupo da rota atual fica destacado.
function NavLinks({ active, hash }: { active: string; hash: string }) {
  const { t } = useT()
  const { projects, activePid } = useProjects()
  // graph.enabled (F-0315) / mindmap.enabled (F-0187/F-0314) = false no projeto
  // ativo esconde o item; flag ausente (servidor antigo) fica ligado. O valor
  // chega pelo /api/projects, relido no evento SSE "projects" de uma escrita.
  const info = projects.find((p) => p.pid === activePid)
  const off = { graph: info?.graph === false, mindmap: info?.mindmap === false }
  // Grupo da rota atual sobre a lista completa: um link antigo para um item
  // desligado por flag ainda diz em que grupo se está.
  const activeGroup = GROUPS.find((g) => g.items.some((l) => l.href === active))?.key
  const [open, setOpen] = useState<string | null>(null)
  // Apontam para o grupo aberto: "fora" é fora dele, não fora da barra inteira.
  const openRef = useRef<HTMLDivElement>(null)
  const openBtnRef = useRef<HTMLButtonElement>(null)

  // Navegou → fecha o menu.
  useEffect(() => setOpen(null), [hash])

  // Fecha ao clicar fora do grupo aberto ou apertar Esc (que devolve o foco ao
  // botão, para o teclado não cair no body).
  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      if (!openRef.current?.contains(e.target as Node)) setOpen(null)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return
      setOpen(null)
      openBtnRef.current?.focus()
    }
    document.addEventListener('mousedown', onDown)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDown)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  const dashActive = DASHBOARD.href === active
  return (
    <div className="nav-links">
      <a
        href={DASHBOARD.href}
        className={dashActive ? 'nav-link active' : 'nav-link'}
        aria-current={dashActive ? 'page' : undefined}
      >
        {t.nav[DASHBOARD.key]}
      </a>
      {GROUPS.map((g) => {
        const isOpen = open === g.key
        const items = g.items.filter((l) => !off[l.key as keyof typeof off])
        return (
          <div
            key={g.key}
            className="nav-group"
            ref={isOpen ? openRef : undefined}
            // Tab para fora do grupo é o "clique fora" do teclado.
            onBlur={(e) => {
              if (isOpen && !e.currentTarget.contains(e.relatedTarget as Node | null)) setOpen(null)
            }}
          >
            <button
              type="button"
              ref={isOpen ? openBtnRef : undefined}
              className={g.key === activeGroup ? 'nav-link nav-group-btn active' : 'nav-link nav-group-btn'}
              aria-haspopup="menu"
              aria-expanded={isOpen}
              onClick={() => setOpen(isOpen ? null : g.key)}
            >
              {t.nav[g.key]} <span aria-hidden="true">▾</span>
            </button>
            {isOpen && (
              <div className="nav-group-menu" role="menu" aria-label={t.nav[g.key]}>
                {items.map((l) => (
                  <a
                    key={l.href}
                    href={l.href}
                    role="menuitem"
                    className={l.href === active ? 'nav-group-item active' : 'nav-group-item'}
                    aria-current={l.href === active ? 'page' : undefined}
                    onClick={() => setOpen(null)}
                  >
                    {t.nav[l.key]}
                  </a>
                ))}
              </div>
            )}
          </div>
        )
      })}
    </div>
  )
}

// Versão do binário que está servindo a UI (F-0129), discreta ao lado da marca.
function NavVersion() {
  const { t } = useT()
  const { activePid } = useProjects()
  const [v, setV] = useState('')
  useEffect(() => {
    if (!activePid) return // a rota é escopada por projeto; espera o pid
    fetchVersion()
      .then(setV)
      .catch(() => setV('')) // sem versão ≠ erro de UI: só omite
  }, [activePid])
  if (!v) return null
  return (
    <span className="nav-version" title={t.nav.versionTitle}>
      {v}
    </span>
  )
}

// Engrenagem das configurações do projeto ativo (F-0315): no cluster da direita,
// como na barra (VS Code), e não num dos grupos de navegação.
function SettingsLink({ active }: { active: boolean }) {
  const { t } = useT()
  return (
    <a
      href="#/settings"
      className={active ? 'theme-toggle settings-link active' : 'theme-toggle settings-link'}
      title={t.nav.settings}
      aria-label={t.nav.settings}
      aria-current={active ? 'page' : undefined}
    >
      ⚙
    </a>
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
