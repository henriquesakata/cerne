import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { useProjects } from './live'
import { useTheme } from './theme'
import { useT, type Lang, type Messages } from './i18n'
import { fetchVersion } from './api'
import NotificationBell from './NotificationBell'

// Barra de navegação persistente: o shell que torna as páginas alcançáveis.
const LINKS: { href: string; key: keyof Messages['nav'] }[] = [
  { href: '#/', key: 'dashboard' },
  { href: '#/kanban', key: 'kanban' },
  { href: '#/backlog', key: 'backlog' },
  { href: '#/rooms', key: 'warRoom' },
  { href: '#/manage', key: 'manage' },
  { href: '#/decisions', key: 'decisions' },
  { href: '#/references', key: 'references' },
  { href: '#/runbooks', key: 'runbooks' },
  { href: '#/notes', key: 'notes' },
  { href: '#/conventions', key: 'conventions' },
  { href: '#/wiki', key: 'wiki' },
  { href: '#/graph', key: 'graph' },
  { href: '#/mindmap', key: 'mindmap' },
  { href: '#/ask', key: 'ask' },
  { href: '#/search', key: 'search' },
  { href: '#/lint', key: 'lint' },
  { href: '#/harness', key: 'harness' },
  { href: '#/history', key: 'history' },
  { href: '#/activity', key: 'activity' },
  { href: '#/stats', key: 'stats' },
]

// activeHref mapeia a rota atual para o link de seção que deve ficar ativo.
// Rotas de detalhe (#/f, #/d, #/r) destacam a seção pai.
function activeHref(hash: string): string {
  // Kanban e detalhe de feature destacam Kanban; '#/' (dashboard) é o fallback.
  if (hash.startsWith('#/kanban') || hash.startsWith('#/f/')) return '#/kanban'
  if (hash.startsWith('#/backlog')) return '#/backlog'
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
  if (hash.startsWith('#/stats')) return '#/stats'
  if (hash.startsWith('#/settings')) return '#/settings' // engrenagem, fora de LINKS
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

// NavLinks é uma "priority+ navigation": mantém os links em uma única linha e
// empurra os que não couberem para um dropdown "Mais". Quantos cabem é medido em
// tempo real (ResizeObserver), então funciona em qualquer largura de monitor.
function NavLinks({ active, hash }: { active: string; hash: string }) {
  const { t, lang } = useT()
  const { projects, activePid } = useProjects()
  // graph.enabled (F-0315) / mindmap.enabled (F-0187/F-0314) = false no projeto
  // ativo esconde o item; flag ausente (servidor antigo) fica ligado. O valor
  // chega pelo /api/projects, relido no evento SSE "projects" de uma escrita.
  const info = projects.find((p) => p.pid === activePid)
  const off = { graph: info?.graph === false, mindmap: info?.mindmap === false }
  const links = useMemo(
    () => LINKS.filter((l) => !off[l.key as keyof typeof off]),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [off.graph, off.mindmap],
  )
  const containerRef = useRef<HTMLDivElement>(null)
  const measureRef = useRef<HTMLDivElement>(null)
  const [visibleCount, setVisibleCount] = useState(links.length)
  const [open, setOpen] = useState(false)

  // Mede a linha oculta (todos os links + botão "Mais") e decide quantos cabem
  // na largura disponível do container, reservando espaço para o botão "Mais".
  const recompute = useCallback(() => {
    const container = containerRef.current
    const measure = measureRef.current
    if (!container || !measure) return
    const avail = container.clientWidth
    const items = Array.from(
      measure.querySelectorAll<HTMLElement>('[data-nav-item]'),
    )
    const moreEl = measure.querySelector<HTMLElement>('[data-nav-more]')
    const gap = parseFloat(getComputedStyle(container).columnGap) || 0
    const widths = items.map((el) => el.getBoundingClientRect().width)
    const moreW = moreEl ? moreEl.getBoundingClientRect().width : 0
    const totalAll =
      widths.reduce((a, w) => a + w, 0) + gap * Math.max(0, widths.length - 1)
    if (totalAll <= avail) {
      setVisibleCount(items.length) // tudo cabe: sem dropdown
      return
    }
    const budget = avail - moreW - gap // precisa do "Mais": reserva a largura dele
    let acc = 0
    let count = 0
    for (const w of widths) {
      const add = w + (count > 0 ? gap : 0)
      if (acc + add <= budget) {
        acc += add
        count++
      } else break
    }
    setVisibleCount(Math.max(1, count)) // sempre pelo menos 1 link visível
  }, [])

  // Recalcula ao montar, ao mudar idioma (larguras dos rótulos mudam) e sempre
  // que o container é redimensionado.
  useLayoutEffect(() => {
    recompute()
    const container = containerRef.current
    if (!container) return
    const ro = new ResizeObserver(recompute)
    ro.observe(container)
    return () => ro.disconnect()
  }, [recompute, lang, links])

  // Navegou → fecha o dropdown.
  useEffect(() => setOpen(false), [hash])

  // Fecha ao clicar fora ou apertar Esc.
  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      if (!containerRef.current?.contains(e.target as Node)) setOpen(false)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false)
    }
    document.addEventListener('mousedown', onDown)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDown)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  const visible = links.slice(0, visibleCount)
  const overflow = links.slice(visibleCount)
  const activeInOverflow = overflow.some((l) => l.href === active)

  return (
    <div className="nav-links" ref={containerRef}>
      {visible.map((l) => (
        <a
          key={l.href}
          href={l.href}
          className={l.href === active ? 'nav-link active' : 'nav-link'}
        >
          {t.nav[l.key]}
        </a>
      ))}
      {overflow.length > 0 && (
        <div className="nav-more">
          <button
            type="button"
            className={
              activeInOverflow
                ? 'nav-link nav-more-btn active'
                : 'nav-link nav-more-btn'
            }
            aria-haspopup="menu"
            aria-expanded={open}
            onClick={() => setOpen((o) => !o)}
          >
            {t.nav.more} ▾
          </button>
          {open && (
            <div className="nav-more-menu" role="menu">
              {overflow.map((l) => (
                <a
                  key={l.href}
                  href={l.href}
                  role="menuitem"
                  className={
                    l.href === active ? 'nav-more-item active' : 'nav-more-item'
                  }
                  onClick={() => setOpen(false)}
                >
                  {t.nav[l.key]}
                </a>
              ))}
            </div>
          )}
        </div>
      )}
      {/* Linha de medição oculta: replica todos os itens para calcular larguras. */}
      <div className="nav-measure" ref={measureRef} aria-hidden="true">
        {links.map((l) => (
          <span key={l.href} data-nav-item className="nav-link">
            {t.nav[l.key]}
          </span>
        ))}
        <span data-nav-more className="nav-link nav-more-btn">
          {t.nav.more} ▾
        </span>
      </div>
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
// como na barra (VS Code), e não em LINKS, onde cairia quase sempre no "Mais".
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
