import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import Nav from './Nav'
import { I18nProvider } from './i18n'
import type { ProjectInfo } from './api'

// F-0187 critério 4: com mindmap.enabled = false no projeto ativo, o item some do
// menu ("não deve nem aparecer"). O flag viaja em ProjectInfo (GET /api/projects).

let mockProjects: ProjectInfo[] = []

vi.mock('./live', () => ({
  useProjects: () => ({
    projects: mockProjects,
    activePid: 'p1',
    setActivePid: () => {},
    changed: new Set<string>(),
    version: 0,
    lastChange: { pid: '', seq: 0 },
  }),
  useLiveVersion: () => 0,
}))

vi.mock('./NotificationBell', () => ({ default: () => null }))

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  fetchVersion: vi.fn(async () => ''),
}))

// F-0332 critério 8: sem stub de ResizeObserver — jsdom não tem um, então a nav
// agrupada só renderiza se não depender mais de medição.
beforeEach(() => {
  cleanup()
  mockProjects = [{ pid: 'p1', name: 'P1' }]
})

afterEach(() => localStorage.clear())

const label = (el: Element) => (el.textContent ?? '').replace('▾', '').trim()

// Os controles de topo da nav, na ordem do documento: o link solto e os botões de grupo.
function topLevel(container: HTMLElement): string[] {
  return Array.from(
    container.querySelectorAll('.nav-links > .nav-link, .nav-group-btn'),
  ).map(label)
}

function openGroup(name: string): HTMLElement {
  fireEvent.click(screen.getByRole('button', { name: new RegExp(name) }))
  return screen.getByRole('menu')
}

function itemsOf(name: string): string[] {
  return within(openGroup(name)).getAllByRole('menuitem').map(label)
}

describe('Nav agrupada (F-0332)', () => {
  it('critério 1: Dashboard e os quatro grupos, nesta ordem (pt-BR)', () => {
    const { container } = render(<Nav hash="#/" />)
    expect(topLevel(container)).toEqual([
      'Dashboard',
      'Trabalho',
      'Conhecimento',
      'Explorar',
      'Saúde',
    ])
  })

  it('critério 1: rótulos dos grupos em inglês', () => {
    localStorage.setItem('cerne-lang', 'en')
    const { container } = render(<Nav hash="#/" />, { wrapper: I18nProvider })
    expect(topLevel(container)).toEqual([
      'Dashboard',
      'Work',
      'Knowledge',
      'Explore',
      'Health',
    ])
  })

  it.each([
    ['Trabalho', ['Kanban', 'Backlog', 'Gestão', 'War Room', 'Atividade']],
    ['Conhecimento', ['Decisões', 'Referências', 'Runbooks', 'Notas', 'Convenções']],
    ['Explorar', ['Wiki', 'Perguntar', 'Busca', 'Grafo', 'Mapa mental']],
    ['Saúde', ['Lint', 'Harness', 'Histórico', 'Economia']],
  ])('critério 2: o grupo %s contém exatamente seus itens, na ordem', (group, want) => {
    render(<Nav hash="#/" />)
    expect(itemsOf(group)).toEqual(want)
  })

  it('critério 3: clique abre o menu do grupo; segundo clique fecha', () => {
    render(<Nav hash="#/" />)
    const btn = screen.getByRole('button', { name: /Trabalho/ })
    expect(btn).toHaveAttribute('aria-haspopup', 'menu')
    expect(btn).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()

    fireEvent.click(btn)
    expect(btn).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getByRole('menu')).toBeInTheDocument()

    fireEvent.click(btn)
    expect(btn).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
  })

  it('critério 4: abrir outro grupo fecha o primeiro', () => {
    render(<Nav hash="#/" />)
    openGroup('Trabalho')
    openGroup('Explorar')
    const menus = screen.getAllByRole('menu')
    expect(menus).toHaveLength(1)
    expect(within(menus[0]).getByText('Wiki')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Trabalho/ })).toHaveAttribute(
      'aria-expanded',
      'false',
    )
  })

  it('critério 5: clique fora fecha o menu', () => {
    render(<Nav hash="#/" />)
    openGroup('Trabalho')
    fireEvent.mouseDown(document.body)
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
  })

  it('critério 5: Esc fecha o menu', () => {
    render(<Nav hash="#/" />)
    openGroup('Trabalho')
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
  })

  it('critério 5: navegar para outra rota fecha o menu', () => {
    const { rerender } = render(<Nav hash="#/" />)
    openGroup('Trabalho')
    rerender(<Nav hash="#/kanban" />)
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
  })

  it.each([
    ['#/kanban', 'Trabalho', 'Kanban'],
    ['#/f/F-0001', 'Trabalho', 'Kanban'],
    ['#/new', 'Trabalho', 'Gestão'],
    ['#/activity', 'Trabalho', 'Atividade'],
    ['#/d/D-0001', 'Conhecimento', 'Decisões'],
    ['#/r/R-0001', 'Conhecimento', 'Referências'],
    ['#/rb/RB-0001', 'Conhecimento', 'Runbooks'],
    ['#/n/N-0001', 'Conhecimento', 'Notas'],
    ['#/wiki', 'Explorar', 'Wiki'],
    ['#/history', 'Saúde', 'Histórico'],
  ])('critério 6: rota %s ativa o grupo %s e o item %s', (hash, group, item) => {
    render(<Nav hash={hash} />)
    const buttons = screen
      .getAllByRole('button')
      .filter((b) => b.classList.contains('nav-group-btn'))
    expect(buttons.filter((b) => b.classList.contains('active')).map(label)).toEqual([group])
    const menu = openGroup(group)
    expect(within(menu).getByText(item)).toHaveClass('active')
    expect(within(menu).getAllByRole('menuitem').filter((a) => a.classList.contains('active'))).toHaveLength(1)
  })

  it('critério 6: o Dashboard ativo não ativa nenhum grupo', () => {
    const { container } = render(<Nav hash="#/" />)
    expect(container.querySelector('.nav-group-btn.active')).toBeNull()
    expect(screen.getByText('Dashboard')).toHaveClass('active')
  })

  it('critério 8: sem linha de medição nem botão "Mais"', () => {
    const { container } = render(<Nav hash="#/" />)
    expect(container.querySelector('.nav-measure')).toBeNull()
    expect(screen.queryByText(/^Mais/)).not.toBeInTheDocument()
    expect(screen.queryByText(/^More/)).not.toBeInTheDocument()
  })
})

// F-0187/F-0315, adaptados no F-0332 critério 7: os itens desligáveis moram no
// grupo Explorar, então o teste abre o grupo antes de procurar o rótulo.
describe('Nav: flags graph/mindmap (critério 7)', () => {
  it('mostra o item Mapa mental quando o projeto ativo permite (default)', () => {
    mockProjects = [{ pid: 'p1', name: 'P1', mindmap: true }]
    render(<Nav hash="#/" />)
    expect(itemsOf('Explorar')).toContain('Mapa mental')
  })

  it('esconde o item Mapa mental quando o projeto ativo desabilita', () => {
    mockProjects = [{ pid: 'p1', name: 'P1', mindmap: false }]
    render(<Nav hash="#/" />)
    expect(itemsOf('Explorar')).not.toContain('Mapa mental')
  })

  it('trata flag ausente (servidor antigo) como ligado', () => {
    mockProjects = [{ pid: 'p1', name: 'P1' }]
    render(<Nav hash="#/" />)
    expect(itemsOf('Explorar')).toEqual(expect.arrayContaining(['Grafo', 'Mapa mental']))
  })

  it('esconde o item Grafo quando o projeto ativo desliga graph', () => {
    mockProjects = [{ pid: 'p1', name: 'P1', graph: false }]
    render(<Nav hash="#/" />)
    const items = itemsOf('Explorar')
    expect(items).not.toContain('Grafo')
    expect(items).toContain('Mapa mental')
  })

  it('mostra o item Grafo com graph ligado', () => {
    mockProjects = [{ pid: 'p1', name: 'P1', graph: true }]
    render(<Nav hash="#/" />)
    expect(itemsOf('Explorar')).toContain('Grafo')
  })

  it('graph e mindmap desligados deixam Explorar só com Wiki, Perguntar e Busca', () => {
    mockProjects = [{ pid: 'p1', name: 'P1', graph: false, mindmap: false }]
    render(<Nav hash="#/" />)
    expect(itemsOf('Explorar')).toEqual(['Wiki', 'Perguntar', 'Busca'])
  })
})
