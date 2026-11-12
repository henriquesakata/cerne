import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, act, waitFor, within } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import { LiveProvider, useProjects } from './live'
import { fetchProjects, HTTPError, renameProject, subscribeLive } from './api'
import type { ProjectInfo } from './api'
import Nav from './Nav'
import Settings from './Settings'

// F-0209: trocar de projeto pelo seletor sempre aterrissa no dashboard —
// nenhuma rota de detalhe sobrevive à troca.

let mockProjects: ProjectInfo[] = [
  { pid: 'p1', name: 'P1' },
  { pid: 'p2', name: 'P2' },
]

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  fetchProjects: vi.fn(),
  setApiProject: vi.fn(),
  subscribeLive: vi.fn(() => () => {}),
  fetchVersion: vi.fn(async () => ''),
  fetchSettings: vi.fn(async () => ({ settings: [], files: {} })),
  renameProject: vi.fn(),
}))

vi.mock('./NotificationBell', () => ({ default: () => null }))

function Probe() {
  const { activePid, setActivePid } = useProjects()
  return (
    <button onClick={() => setActivePid(activePid === 'p1' ? 'p2' : 'p1')}>
      {activePid}
    </button>
  )
}

beforeEach(() => {
  cleanup()
  localStorage.clear()
  sessionStorage.clear()
  window.location.hash = ''
  vi.mocked(fetchProjects).mockReset()
  vi.mocked(fetchProjects).mockImplementation(async () => ({ projects: mockProjects, default: 'p1' }))
})

describe('LiveProvider', () => {
  // F-0373: o link que o `cerne ui` imprime ao entrar num painel aberto.
  it('F-0373: ?project=<pid> abre esse projeto, acima da escolha guardada', async () => {
    sessionStorage.setItem('cerne.activePid', 'p1')
    window.history.replaceState({}, '', '/?project=p2')
    try {
      render(
        <LiveProvider>
          <Probe />
        </LiveProvider>,
      )
      expect(await screen.findByText('p2')).toBeInTheDocument()
    } finally {
      window.history.replaceState({}, '', '/')
    }
  })

  it('F-0373: ?project= de pid desconhecido cai na escolha de sempre', async () => {
    window.history.replaceState({}, '', '/?project=nope')
    try {
      render(
        <LiveProvider>
          <Probe />
        </LiveProvider>,
      )
      expect(await screen.findByText('p1')).toBeInTheDocument()
    } finally {
      window.history.replaceState({}, '', '/')
    }
  })

  it('critério 1: troca via setActivePid reseta o hash para o dashboard', async () => {
    window.location.hash = '#/f/F-0001'
    render(
      <LiveProvider>
        <Probe />
      </LiveProvider>,
    )
    const btn = await screen.findByText('p1')
    await act(async () => {
      btn.click()
    })
    expect(window.location.hash).toBe('')
  })

  it('critério 3: deep link no load inicial não é redirecionado', async () => {
    window.location.hash = '#/f/F-0001'
    render(
      <LiveProvider>
        <Probe />
      </LiveProvider>,
    )
    await screen.findByText('p1')
    expect(window.location.hash).toBe('#/f/F-0001')
  })
})

// F-0242: o `.catch` do bootstrap não pode fingir prontidão — senão as telas
// montam com pid vazio e disparam /api/projects//... (404 permanente).
describe('LiveProvider — falha no bootstrap (F-0242 critério 6)', () => {
  it('fetchProjects rejeitando mostra erro/retry e não libera os children', async () => {
    vi.mocked(fetchProjects).mockRejectedValueOnce(new Error('offline'))
    render(
      <LiveProvider>
        <Probe />
      </LiveProvider>,
    )
    await waitFor(() => expect(screen.getByText(/erro ao carregar projetos: offline/i)).toBeInTheDocument())
    expect(screen.queryByRole('button', { name: 'p1' })).not.toBeInTheDocument()

    vi.mocked(fetchProjects).mockResolvedValueOnce({ projects: mockProjects, default: 'p1' })
    await act(async () => {
      screen.getByRole('button', { name: /tentar de novo/i }).click()
    })
    await waitFor(() => expect(screen.getByText('p1')).toBeInTheDocument())
  })

  it('lista de projetos vazia mostra mensagem própria, não children quebrados', async () => {
    vi.mocked(fetchProjects).mockResolvedValueOnce({ projects: [], default: '' })
    render(
      <LiveProvider>
        <Probe />
      </LiveProvider>,
    )
    await waitFor(() => expect(screen.getByText(/nenhum projeto registrado/i)).toBeInTheDocument())
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
  })
})

// F-0288: o projeto ativo é por ABA. sessionStorage guarda a escolha desta aba
// (sobrevive a reload); localStorage guarda só o default de uma aba nova. Uma
// aba trocando de projeto não pode arrastar as outras.
describe('LiveProvider — projeto ativo por aba (F-0288)', () => {
  it('critério 1: evento storage de outra aba não troca o projeto nem mexe no hash', async () => {
    render(
      <LiveProvider>
        <Probe />
      </LiveProvider>,
    )
    await screen.findByText('p1')
    window.location.hash = '#/f/F-0001'
    await act(async () => {
      window.dispatchEvent(
        new StorageEvent('storage', { key: 'cerne.activePid', newValue: 'p2' }),
      )
    })
    expect(screen.getByText('p1')).toBeInTheDocument()
    expect(window.location.hash).toBe('#/f/F-0001')
  })

  it('critério 2: sessionStorage vence localStorage no boot (reload mantém a aba)', async () => {
    sessionStorage.setItem('cerne.activePid', 'p2')
    localStorage.setItem('cerne.activePid', 'p1')
    render(
      <LiveProvider>
        <Probe />
      </LiveProvider>,
    )
    expect(await screen.findByText('p2')).toBeInTheDocument()
  })

  it('critério 3: sem sessionStorage, a aba nova herda a última escolha (localStorage)', async () => {
    localStorage.setItem('cerne.activePid', 'p2')
    render(
      <LiveProvider>
        <Probe />
      </LiveProvider>,
    )
    expect(await screen.findByText('p2')).toBeInTheDocument()
  })

  it('setActivePid grava nas duas storages', async () => {
    render(
      <LiveProvider>
        <Probe />
      </LiveProvider>,
    )
    const btn = await screen.findByText('p1')
    await act(async () => {
      btn.click()
    })
    expect(sessionStorage.getItem('cerne.activePid')).toBe('p2')
    expect(localStorage.getItem('cerne.activePid')).toBe('p2')
  })

  it('critério 5: pid salvo que sumiu da lista cai no default do servidor', async () => {
    sessionStorage.setItem('cerne.activePid', 'p9')
    render(
      <LiveProvider>
        <Probe />
      </LiveProvider>,
    )
    expect(await screen.findByText('p1')).toBeInTheDocument()
  })
})

// F-0315 critérios 6/7, elo UI: uma escrita de settings faz o servidor emitir
// o evento SSE "projects" (elo servidor: TestSettingsWriteAnnouncesProjects);
// a aba relê /api/projects e a nav esconde/reexibe o Grafo sem reload.
// Desde o F-0332 o Grafo mora no grupo Explorar: o teste abre o grupo, que fica
// aberto enquanto a lista de projetos é relida (só fecha ao navegar).
describe('LiveProvider — gate do Grafo ao vivo (F-0315)', () => {
  it('evento projects com graph desligado tira a aba Grafo; religado, ela volta', async () => {
    let onProjects = () => {}
    vi.mocked(subscribeLive).mockImplementation((h) => {
      onProjects = h.onProjects
      return () => {}
    })
    const gate = (graph: boolean) => async () => ({ projects: [{ pid: 'p1', name: 'P1', graph }], default: 'p1' })
    vi.mocked(fetchProjects).mockImplementation(gate(true))
    render(
      <LiveProvider>
        <Nav hash="#/" />
      </LiveProvider>,
    )
    fireEvent.click(await screen.findByRole('button', { name: /Explorar/ }))
    expect((await screen.findAllByText('Grafo')).length).toBeGreaterThan(0)
    const brand = screen.getByText('cerne')

    vi.mocked(fetchProjects).mockImplementation(gate(false))
    await act(async () => onProjects())
    await waitFor(() => expect(screen.queryByText('Grafo')).not.toBeInTheDocument())
    // O menu segue aberto: o Grafo sumiu do menu, não porque o menu fechou.
    expect(screen.getByRole('menu')).toHaveTextContent('Wiki')

    vi.mocked(fetchProjects).mockImplementation(gate(true))
    await act(async () => onProjects())
    expect((await screen.findAllByText('Grafo')).length).toBeGreaterThan(0)
    // Mesma árvore: a nav não foi remontada (sem reload).
    expect(screen.getByText('cerne')).toBe(brand)
  })
})

// F-0315 (crítica C2): toda escrita de settings dispara o evento "projects";
// se a releitura da lista falhar depois do boot, o app inteiro não pode virar a
// página de erro de boot — a lista e as telas atuais ficam, com um aviso.
describe('LiveProvider — releitura da lista que falha depois do boot (F-0315)', () => {
  it('mantém projetos e telas, com um aviso não bloqueante e nova tentativa', async () => {
    let onProjects = () => {}
    vi.mocked(subscribeLive).mockImplementation((h) => {
      onProjects = h.onProjects
      return () => {}
    })
    render(
      <LiveProvider>
        <Probe />
      </LiveProvider>,
    )
    await screen.findByText('p1')

    vi.mocked(fetchProjects).mockRejectedValueOnce(new Error('Failed to fetch'))
    await act(async () => onProjects())
    expect(screen.getByText('p1')).toBeInTheDocument()
    expect(screen.queryByText(/erro ao carregar projetos/i)).not.toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent('Failed to fetch')

    await act(async () => screen.getByRole('button', { name: /tentar de novo/i }).click())
    await waitFor(() => expect(screen.queryByRole('status')).not.toBeInTheDocument())
    expect(screen.getByText('p1')).toBeInTheDocument()
  })
})

// F-0331 crit. 2 e 7, elo UI: o rename faz o servidor emitir o evento
// "projects" (elo servidor: TestProjectNamePutAnnouncesProjects); a aba relê
// /api/projects e o seletor, o cabeçalho e o campo mostram o nome novo sem
// reload. Na falha, nada muda fora do campo.
describe('LiveProvider — nome do projeto ao vivo (F-0331)', () => {
  const two = (p1: string) => async () => ({
    projects: [
      { pid: 'p1', name: p1 },
      { pid: 'p2', name: 'P2' },
    ],
    default: 'p1',
  })
  let onProjects = () => {}
  let onChange = (_pid: string) => {}
  const mount = async () => {
    globalThis.ResizeObserver ??= class {
      observe() {}
      unobserve() {}
      disconnect() {}
    } as unknown as typeof ResizeObserver
    vi.mocked(subscribeLive).mockImplementation((h) => {
      onProjects = h.onProjects
      onChange = h.onChange
      return () => {}
    })
    vi.mocked(fetchProjects).mockImplementation(two('P1'))
    vi.mocked(renameProject).mockReset()
    render(
      <LiveProvider>
        <Nav hash="#/settings" />
        <Settings />
      </LiveProvider>,
    )
    const region = await screen.findByRole('region', { name: 'Nome do projeto' })
    return { region, input: within(region).getByRole('textbox', { name: 'Nome do projeto' }) }
  }

  it('crit. 2: salvar e o evento projects trocam o nome no seletor, no cabeçalho e no campo', async () => {
    const { region, input } = await mount()
    const brand = screen.getByText('cerne')
    vi.mocked(renameProject).mockResolvedValue('Núcleo')
    fireEvent.change(input, { target: { value: '  Núcleo ' } })
    fireEvent.click(within(region).getByRole('button', { name: 'Salvar' }))
    await waitFor(() => expect(renameProject).toHaveBeenCalledWith('Núcleo'))

    vi.mocked(fetchProjects).mockImplementation(two('Núcleo'))
    await act(async () => onProjects())
    expect(await screen.findByRole('option', { name: 'Núcleo' })).toBeInTheDocument()
    expect(document.querySelector('.settings-project strong')).toHaveTextContent('Núcleo')
    expect(within(screen.getByRole('region', { name: 'Nome do projeto' })).getByRole('textbox')).toHaveValue('Núcleo')
    // Mesma árvore: a nav não foi remontada (sem reload).
    expect(screen.getByText('cerne')).toBe(brand)
  })

  // Crítica C1: o tick do pull só emite "change". Sem reler a lista nele, o
  // campo ficava com o nome velho e um Salvar desfazia o rename do outro clone.
  it('crítica C1: só um frame change (rename trazido por pull) troca o nome no seletor, no cabeçalho e no campo', async () => {
    await mount()
    vi.mocked(fetchProjects).mockImplementation(two('Renomeado Fora'))
    await act(async () => onChange('p1'))
    expect(await screen.findByRole('option', { name: 'Renomeado Fora' })).toBeInTheDocument()
    expect(document.querySelector('.settings-project strong')).toHaveTextContent('Renomeado Fora')
    expect(within(screen.getByRole('region', { name: 'Nome do projeto' })).getByRole('textbox')).toHaveValue(
      'Renomeado Fora',
    )
  })

  it('crit. 7: a recusa fica no campo, que mantém o texto, e o seletor mantém o nome do backend', async () => {
    const { region, input } = await mount()
    vi.mocked(renameProject).mockRejectedValue(new HTTPError('could not acquire backend lock', 409))
    fireEvent.change(input, { target: { value: 'Núcleo' } })
    fireEvent.click(within(region).getByRole('button', { name: 'Salvar' }))
    expect(await within(region).findByRole('alert')).toHaveTextContent('O backend está ocupado')
    await act(async () => onProjects())
    expect(input).toHaveValue('Núcleo')
    expect(screen.getByRole('option', { name: 'P1' })).toBeInTheDocument()
    expect(within(region).getByRole('alert')).toBeInTheDocument()
  })
})
