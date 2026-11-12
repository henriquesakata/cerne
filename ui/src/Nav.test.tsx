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
    ['Trabalho', ['Kanban', 'Backlog', 'Pendências', 'Ideias', 'Gestão', 'War Room', 'Atividade']],
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

  // Crítica F-0332 #4: "fora" é fora do grupo aberto, não fora da barra — o
  // container .nav-links estica pela barra e o Dashboard em #/ não muda o hash.
  it('critério 5: clique no Dashboard ou no vazio da barra fecha o menu', () => {
    const { container } = render(<Nav hash="#/" />)
    openGroup('Trabalho')
    fireEvent.mouseDown(screen.getByText('Dashboard'))
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
    openGroup('Trabalho')
    fireEvent.mouseDown(container.querySelector('.nav-links')!)
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
  })

  it('critério 5: clique dentro do menu aberto não o fecha', () => {
    render(<Nav hash="#/" />)
    const menu = openGroup('Trabalho')
    fireEvent.mouseDown(within(menu).getByText('Backlog'))
    expect(screen.getByRole('menu')).toBeInTheDocument()
  })

  // Crítica F-0332 #3: sair do menu com Tab é o "clique fora" do teclado.
  it('critério 5: o foco saindo do grupo fecha o menu; foco entre itens não', () => {
    render(<Nav hash="#/" />)
    const menu = openGroup('Trabalho')
    const [first, second] = within(menu).getAllByRole('menuitem')
    fireEvent.blur(first, { relatedTarget: second })
    expect(screen.getByRole('menu')).toBeInTheDocument()
    fireEvent.blur(second, { relatedTarget: screen.getByText('Dashboard') })
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
  })

  // Crítica F-0332 #1: Esc não pode jogar o foco no body.
  it('critério 5: Esc devolve o foco ao botão do grupo', () => {
    render(<Nav hash="#/" />)
    const menu = openGroup('Trabalho')
    within(menu).getAllByRole('menuitem')[0].focus()
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(screen.getByRole('button', { name: /Trabalho/ })).toHaveFocus()
  })

  // Crítica F-0332 #6: todas as 21 telas, não uma amostra — uma rota sem prefixo
  // em activeHref cairia no Dashboard em silêncio.
  it.each([
    ['#/kanban', 'Trabalho', 'Kanban'],
    ['#/f/F-0001', 'Trabalho', 'Kanban'],
    ['#/backlog', 'Trabalho', 'Backlog'],
    ['#/pendings', 'Trabalho', 'Pendências'],
    ['#/pendings?from=F-0331', 'Trabalho', 'Pendências'],
    ['#/ideas', 'Trabalho', 'Ideias'],
    ['#/manage', 'Trabalho', 'Gestão'],
    ['#/new', 'Trabalho', 'Gestão'],
    ['#/rooms', 'Trabalho', 'War Room'],
    ['#/rooms/R1', 'Trabalho', 'War Room'],
    ['#/activity', 'Trabalho', 'Atividade'],
    ['#/decisions', 'Conhecimento', 'Decisões'],
    ['#/d/D-0001', 'Conhecimento', 'Decisões'],
    ['#/references', 'Conhecimento', 'Referências'],
    ['#/r/R-0001', 'Conhecimento', 'Referências'],
    ['#/runbooks', 'Conhecimento', 'Runbooks'],
    ['#/rb/RB-0001', 'Conhecimento', 'Runbooks'],
    ['#/notes', 'Conhecimento', 'Notas'],
    ['#/n/N-0001', 'Conhecimento', 'Notas'],
    ['#/conventions', 'Conhecimento', 'Convenções'],
    ['#/wiki', 'Explorar', 'Wiki'],
    ['#/ask', 'Explorar', 'Perguntar'],
    ['#/search', 'Explorar', 'Busca'],
    ['#/graph', 'Explorar', 'Grafo'],
    ['#/mindmap', 'Explorar', 'Mapa mental'],
    ['#/lint', 'Saúde', 'Lint'],
    ['#/harness', 'Saúde', 'Harness'],
    ['#/history', 'Saúde', 'Histórico'],
    ['#/stats', 'Saúde', 'Economia'],
  ])('critério 6: rota %s ativa o grupo %s e o item %s', (hash, group, item) => {
    render(<Nav hash={hash} />)
    const buttons = screen
      .getAllByRole('button')
      .filter((b) => b.classList.contains('nav-group-btn'))
    expect(buttons.filter((b) => b.classList.contains('active')).map(label)).toEqual([group])
    expect(screen.getByText('Dashboard')).not.toHaveClass('active')
    const menu = openGroup(group)
    expect(within(menu).getByText(item)).toHaveClass('active')
    expect(within(menu).getByText(item)).toHaveAttribute('aria-current', 'page')
    expect(within(menu).getAllByRole('menuitem').filter((a) => a.classList.contains('active'))).toHaveLength(1)
  })

  // F-0334 crit. 1 e 5: as duas filas de trabalho têm entrada própria.
  it('F-0334: Pendências e Ideias no grupo Trabalho levam às suas telas', () => {
    render(<Nav hash="#/" />)
    const menu = openGroup('Trabalho')
    expect(within(menu).getByRole('menuitem', { name: 'Pendências' })).toHaveAttribute('href', '#/pendings')
    expect(within(menu).getByRole('menuitem', { name: 'Ideias' })).toHaveAttribute('href', '#/ideas')
  })

  // F-0334 (crítica C6): a nav marca Pendências e Ideias pela mesma regra
  // que o App usa para montá-las; o que o App não monta cai no Dashboard.
  it.each(['#/ideas?x=1', '#/pendingsfoo', '#/ideasfoo'])('F-0334: %s não marca grupo nenhum', (hash) => {
    const { container } = render(<Nav hash={hash} />)
    expect(container.querySelector('.nav-group-btn.active')).toBeNull()
    expect(screen.getByText('Dashboard')).toHaveClass('active')
  })

  it('critério 6: o Dashboard ativo não ativa nenhum grupo', () => {
    const { container } = render(<Nav hash="#/" />)
    expect(container.querySelector('.nav-group-btn.active')).toBeNull()
    expect(screen.getByText('Dashboard')).toHaveClass('active')
    expect(screen.getByText('Dashboard')).toHaveAttribute('aria-current', 'page')
  })

  // Crítica F-0332 #7: link antigo para um item desligado ainda diz onde se está.
  it('critério 6: rota de item desligado por flag ainda ativa o grupo', () => {
    mockProjects = [{ pid: 'p1', name: 'P1', graph: false }]
    render(<Nav hash="#/graph" />)
    expect(screen.getByRole('button', { name: /Explorar/ })).toHaveClass('active')
  })

  it('critério 3: o menu aberto leva o nome do grupo', () => {
    render(<Nav hash="#/" />)
    openGroup('Conhecimento')
    expect(screen.getByRole('menu', { name: 'Conhecimento' })).toBeInTheDocument()
  })

  // Crítica F-0332 #12: prova o não-uso com um espião, não com a ausência do jsdom.
  it('critério 8: sem ResizeObserver, sem linha de medição nem botão "Mais"', () => {
    const constructed = vi.fn()
    globalThis.ResizeObserver = class {
      constructor() {
        constructed()
      }
      observe() {}
      unobserve() {}
      disconnect() {}
    } as unknown as typeof ResizeObserver
    try {
      const { container } = render(<Nav hash="#/" />)
      openGroup('Explorar')
      expect(constructed).not.toHaveBeenCalled()
      expect(container.querySelector('.nav-measure')).toBeNull()
      expect(screen.queryByText(/^Mais/)).not.toBeInTheDocument()
      expect(screen.queryByText(/^More/)).not.toBeInTheDocument()
    } finally {
      delete (globalThis as { ResizeObserver?: unknown }).ResizeObserver
    }
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

// F-0373 critério 6: o nome do projeto em exibição está no cabeçalho — a Nav é
// montada por App em toda rota.
describe('Nav: nome do projeto em exibição', () => {
  it('com um projeto, mostra o nome', () => {
    mockProjects = [{ pid: 'p1', name: 'Meu Projeto' }]
    render(
      <I18nProvider>
        <Nav hash="#/kanban" />
      </I18nProvider>,
    )
    expect(document.querySelector('.nav .nav-project-name')).toHaveTextContent('Meu Projeto')
  })

  it('com vários, o seletor tem o projeto ativo selecionado', () => {
    mockProjects = [
      { pid: 'p1', name: 'Meu Projeto' },
      { pid: 'p2', name: 'Outro' },
    ]
    render(
      <I18nProvider>
        <Nav hash="#/kanban" />
      </I18nProvider>,
    )
    expect(document.querySelector('.nav select.nav-project-select')).toHaveDisplayValue('Meu Projeto')
  })
})
