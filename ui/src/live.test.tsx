import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, act, waitFor, within } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import { LiveProvider, pageReload, useProjects } from './live'
import { fetchProjects, fetchUpdate, HTTPError, renameProject, restartPanel, startUpdate, subscribeLive } from './api'
import type { UpdateState } from './api'
import type { ProjectInfo } from './api'
import Nav from './Nav'
import { navAllowed, setNavGuard } from './navGuard'
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
  restartPanel: vi.fn(),
  fetchUpdate: vi.fn(async () => null),
  startUpdate: vi.fn(),
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

  it('F-0373: ?project= é consumido uma vez e sai da URL', async () => {
    window.history.replaceState({}, '', '/?project=p2&x=1#/kanban')
    try {
      render(
        <LiveProvider>
          <Probe />
        </LiveProvider>,
      )
      expect(await screen.findByText('p2')).toBeInTheDocument()
      expect(window.location.search).toBe('?x=1')
      expect(window.location.hash).toBe('#/kanban')
      // uma troca depois do link segue o usuário, e o reload não volta ao link
      await act(async () => {
        screen.getByText('p2').click()
      })
      expect(await screen.findByText('p1')).toBeInTheDocument()
    } finally {
      window.history.replaceState({}, '', '/')
    }
  })

  it('F-0373: ?project= com pid que pede escape (espaço, barra) acha o projeto', async () => {
    mockProjects = [
      { pid: 'p1', name: 'P1' },
      { pid: 'a b/c', name: 'Esp' },
    ]
    window.history.replaceState({}, '', '/?project=' + encodeURIComponent('a b/c'))
    try {
      render(
        <LiveProvider>
          <Probe />
        </LiveProvider>,
      )
      expect(await screen.findByText('a b/c')).toBeInTheDocument()
    } finally {
      mockProjects = [
        { pid: 'p1', name: 'P1' },
        { pid: 'p2', name: 'P2' },
      ]
      window.history.replaceState({}, '', '/')
    }
  })

  it('F-0373: ?project= de pid desconhecido cai na escolha de sempre e avisa', async () => {
    window.history.replaceState({}, '', '/?project=nope')
    try {
      render(
        <LiveProvider>
          <Probe />
        </LiveProvider>,
      )
      expect(await screen.findByText('p1')).toBeInTheDocument()
      expect(screen.getByRole('status')).toHaveTextContent(/nope/)
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

  it('F-0464 A2: an unsaved vault draft that refuses keeps the project and the route', async () => {
    window.location.hash = '#/vault/N-0001'
    render(
      <LiveProvider>
        <Probe />
      </LiveProvider>,
    )
    const btn = await screen.findByText('p1')
    setNavGuard(() => false)
    try {
      await act(async () => {
        btn.click()
      })
      expect(screen.getByText('p1')).toBeInTheDocument()
      expect(window.location.hash).toBe('#/vault/N-0001')
      // a confirmed discard switches, and the router is not asked a second time
      const guard = vi.fn(() => true)
      setNavGuard(guard)
      await act(async () => {
        screen.getByText('p1').click()
      })
      expect(await screen.findByText('p2')).toBeInTheDocument()
      expect(guard).toHaveBeenCalledTimes(1)
      expect(navAllowed()).toBe(true)
      expect(guard).toHaveBeenCalledTimes(1)
    } finally {
      setNavGuard(null)
    }
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

// F-0478: o painel avisa do binário novo no disco, reinicia nele pelo botão e
// cada aba recarrega quando o painel volta com outra identidade.
describe('LiveProvider — reinício no binário novo (F-0478)', () => {
  const running = { version: 'v0.14.3', ui: 'old' }
  // changed segue o servidor: o arquivo no disco é outro quando a identidade
  // difere da que roda (o teste do build local passa changed à parte).
  const listWith =
    (installed: { version: string; ui: string; error?: string; changed?: boolean }, id = running) =>
    async () => ({
      projects: mockProjects,
      default: 'p1',
      ...id,
      installed: { changed: installed.version !== id.version || installed.ui !== id.ui, ...installed },
    })
  let onProjects = () => {}
  beforeEach(() => {
    vi.mocked(subscribeLive).mockImplementation((h) => {
      onProjects = h.onProjects
      return () => {}
    })
    vi.mocked(restartPanel).mockReset()
    pageReload.run = vi.fn()
  })

  it('critérios 1 e 2: avisa só quando o disco tem outro binário', async () => {
    vi.mocked(fetchProjects).mockImplementation(listWith(running))
    render(
      <LiveProvider>
        <Probe />
      </LiveProvider>,
    )
    await screen.findByText('p1')
    expect(screen.queryByRole('button', { name: 'Reiniciar o painel' })).not.toBeInTheDocument()

    vi.mocked(fetchProjects).mockImplementation(listWith({ version: 'v0.15.0', ui: 'new' }))
    await act(async () => onProjects())
    expect(screen.getByRole('status')).toHaveTextContent('O cerne 0.15.0 está instalado; este painel ainda roda o 0.14.3.')
    expect(screen.getByRole('button', { name: 'Reiniciar o painel' })).toHaveAttribute('aria-disabled', 'false')
    expect(screen.getByRole('status')).toHaveClass('advisory')
  })

  it('critério 1: build local novo com a mesma versão e UI também avisa', async () => {
    vi.mocked(fetchProjects).mockImplementation(listWith({ ...running, changed: true }))
    render(
      <LiveProvider>
        <Probe />
      </LiveProvider>,
    )
    expect(await screen.findByRole('button', { name: 'Reiniciar o painel' })).toBeInTheDocument()
  })

  it('reinício aceito que não traz o painel de volta noutra versão sai do "Reiniciando…" em 30 s', async () => {
    vi.mocked(fetchProjects).mockImplementation(listWith({ version: 'v0.15.0', ui: 'new' }))
    vi.mocked(restartPanel).mockResolvedValue(undefined)
    render(
      <LiveProvider>
        <Probe />
      </LiveProvider>,
    )
    const btn = await screen.findByRole('button', { name: 'Reiniciar o painel' })
    vi.useFakeTimers()
    try {
      await act(async () => fireEvent.click(btn))
      expect(screen.getByRole('status')).toHaveTextContent('Reiniciando o painel…')
      await act(async () => vi.advanceTimersByTimeAsync(30_500))
      expect(screen.getByRole('status')).toHaveTextContent('O painel não voltou numa versão nova.')
      expect(btn).toHaveAttribute('aria-disabled', 'false')
    } finally {
      vi.useRealTimers()
    }
  })

  it('o binário do disco que não responde não vira aviso', async () => {
    vi.mocked(fetchProjects).mockImplementation(listWith({ version: '', ui: '', error: 'exit status 3', changed: true }))
    render(
      <LiveProvider>
        <Probe />
      </LiveProvider>,
    )
    await screen.findByText('p1')
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })

  it('o clique pede o reinício e mostra que está reiniciando', async () => {
    vi.mocked(fetchProjects).mockImplementation(listWith({ version: 'v0.15.0', ui: 'new' }))
    vi.mocked(restartPanel).mockImplementation(() => new Promise(() => {}))
    render(
      <LiveProvider>
        <Probe />
      </LiveProvider>,
    )
    fireEvent.click(await screen.findByRole('button', { name: 'Reiniciar o painel' }))
    expect(restartPanel).toHaveBeenCalledTimes(1)
    expect(screen.getByRole('status')).toHaveTextContent('Reiniciando o painel…')
    const btn = screen.getByRole('button', { name: 'Reiniciar o painel' })
    expect(btn).toHaveAttribute('aria-disabled', 'true')
    fireEvent.click(btn)
    expect(restartPanel).toHaveBeenCalledTimes(1) // o segundo clique não pede de novo
  })

  it('critério 6: a recusa do servidor aparece no aviso', async () => {
    vi.mocked(fetchProjects).mockImplementation(listWith({ version: 'v0.15.0', ui: 'new' }))
    vi.mocked(restartPanel).mockRejectedValue(new Error('the cerne on disk does not run'))
    render(
      <LiveProvider>
        <Probe />
      </LiveProvider>,
    )
    fireEvent.click(await screen.findByRole('button', { name: 'Reiniciar o painel' }))
    expect(await screen.findByText('O painel não reiniciou: the cerne on disk does not run')).toBeInTheDocument()
  })

  it('critério 5: o painel volta com outra identidade e a aba recarrega', async () => {
    vi.mocked(fetchProjects).mockImplementation(listWith({ version: 'v0.15.0', ui: 'new' }))
    render(
      <LiveProvider>
        <Probe />
      </LiveProvider>,
    )
    await screen.findByText('p1')
    expect(pageReload.run).not.toHaveBeenCalled()

    const upgraded = { version: 'v0.15.0', ui: 'new' }
    vi.mocked(fetchProjects).mockImplementation(listWith(upgraded, upgraded))
    await act(async () => onProjects())
    expect(pageReload.run).toHaveBeenCalledTimes(1)
  })

  it('critério 5: espera o projeto da aba voltar ao painel antes de recarregar', async () => {
    vi.mocked(fetchProjects).mockImplementation(listWith(running))
    render(
      <LiveProvider>
        <Probe />
      </LiveProvider>,
    )
    await screen.findByText('p1')
    const upgraded = { version: 'v0.15.0', ui: 'new' }
    const only = (list: ProjectInfo[]) => async () => ({ projects: list, default: 'p2', ...upgraded, installed: upgraded })

    // O painel voltou, mas o processo do p1 ainda não se registrou de novo.
    vi.mocked(fetchProjects).mockImplementation(only([{ pid: 'p2', name: 'P2' }]))
    await act(async () => onProjects())
    expect(pageReload.run).not.toHaveBeenCalled()
    expect(screen.getByText('p1')).toBeInTheDocument() // a aba não troca de projeto

    vi.mocked(fetchProjects).mockImplementation(only(mockProjects))
    sessionStorage.clear() // a aba que chegou pelo link do `cerne ui` não guardou escolha
    await act(async () => onProjects())
    expect(pageReload.run).toHaveBeenCalledTimes(1)
    // A recarga volta no projeto da aba, não no padrão do painel (p2).
    expect(sessionStorage.getItem('cerne.activePid')).toBe('p1')
  })

  it('critério 5: projeto que não volta recarrega mesmo assim depois de 15 s', async () => {
    vi.mocked(fetchProjects).mockImplementation(listWith(running))
    render(
      <LiveProvider>
        <Probe />
      </LiveProvider>,
    )
    await screen.findByText('p1')
    vi.useFakeTimers()
    try {
      const upgraded = { version: 'v0.15.0', ui: 'new' }
      vi.mocked(fetchProjects).mockImplementation(async () => ({
        projects: [{ pid: 'p2', name: 'P2' }],
        default: 'p2',
        ...upgraded,
        installed: upgraded,
      }))
      await act(async () => onProjects())
      expect(pageReload.run).not.toHaveBeenCalled()
      await act(async () => vi.advanceTimersByTimeAsync(15_500))
      expect(pageReload.run).toHaveBeenCalledTimes(1)
    } finally {
      vi.useRealTimers()
    }
  })

  it('critério 5 com rascunho aberto: a trava recusa e o aviso oferece recarregar', async () => {
    vi.mocked(fetchProjects).mockImplementation(listWith(running))
    render(
      <LiveProvider>
        <Probe />
      </LiveProvider>,
    )
    await screen.findByText('p1')
    setNavGuard(() => false)
    try {
      const upgraded = { version: 'v0.15.0', ui: 'new' }
      vi.mocked(fetchProjects).mockImplementation(listWith(upgraded, upgraded))
      await act(async () => onProjects())
      expect(pageReload.run).not.toHaveBeenCalled()
      expect(screen.getByRole('status')).toHaveTextContent('O painel agora roda o cerne 0.15.0.')
      fireEvent.click(screen.getByRole('button', { name: 'Recarregar' }))
      expect(pageReload.run).toHaveBeenCalledTimes(1)
    } finally {
      setNavGuard(null)
    }
  })
})

// F-0480: o botão Atualizar só aparece com release mais nova que o binário
// instalado; o clique troca o binário e o painel reinicia nele (F-0478).
describe('Nav — botão Atualizar (F-0480)', () => {
  const base: UpdateState = { current: 'v0.14.3', latest: 'v0.15.0', newer: true, swappable: true, running: false }
  let onProjects = () => {}
  let onUpdate: (line: string) => void = () => {}
  beforeEach(() => {
    vi.mocked(subscribeLive).mockImplementation((h) => {
      onProjects = h.onProjects
      onUpdate = h.onUpdate ?? (() => {})
      return () => {}
    })
    vi.mocked(startUpdate).mockReset()
  })
  afterEach(() => vi.mocked(fetchUpdate).mockImplementation(async () => null))

  const renderNav = () =>
    render(
      <LiveProvider>
        <Nav hash="#/" />
      </LiveProvider>,
    )

  it('critério 2: aparece com release nova e some em dia ou sem consulta', async () => {
    vi.mocked(fetchUpdate).mockImplementation(async () => base)
    renderNav()
    expect(await screen.findByRole('button', { name: 'Atualizar para 0.15.0' })).toBeInTheDocument()

    vi.mocked(fetchUpdate).mockImplementation(async () => ({ ...base, latest: 'v0.14.3', newer: false }))
    await act(async () => onProjects())
    expect(screen.queryByRole('button', { name: /Atualizar para/ })).not.toBeInTheDocument()

    vi.mocked(fetchUpdate).mockImplementation(async () => ({ ...base, latest: undefined, newer: false, error: 'offline' }))
    await act(async () => onProjects())
    expect(screen.queryByRole('button', { name: /Atualizar para|disponível/ })).not.toBeInTheDocument()
  })

  it('critério 3: o clique pede a troca e mostra o andamento', async () => {
    vi.mocked(fetchUpdate).mockImplementation(async () => base)
    vi.mocked(startUpdate).mockImplementation(() => new Promise(() => {}))
    renderNav()
    fireEvent.click(await screen.findByRole('button', { name: 'Atualizar para 0.15.0' }))
    expect(startUpdate).toHaveBeenCalledTimes(1)
    await act(async () => onUpdate('==> Upgrading henriquesakata/tap/cerne'))
    expect(screen.getByRole('status')).toHaveTextContent('Atualizando o cerne… ==> Upgrading henriquesakata/tap/cerne')
    expect(screen.queryByRole('button', { name: /Atualizar para/ })).not.toBeInTheDocument()
  })

  it('critério 4: a troca que termina bem cai no reinício da F-0478', async () => {
    vi.mocked(fetchUpdate).mockImplementation(async () => base)
    pageReload.run = vi.fn()
    vi.mocked(fetchProjects).mockImplementation(async () => ({ projects: mockProjects, default: 'p1', version: 'v0.14.3', ui: 'old' }))
    renderNav()
    await screen.findByRole('button', { name: 'Atualizar para 0.15.0' })
    // O painel voltou no binário novo (o servidor reinicia sozinho depois da troca).
    vi.mocked(fetchUpdate).mockImplementation(async () => ({ ...base, current: 'v0.15.0', newer: false }))
    vi.mocked(fetchProjects).mockImplementation(async () => ({ projects: mockProjects, default: 'p1', version: 'v0.15.0', ui: 'new' }))
    await act(async () => onProjects())
    expect(pageReload.run).toHaveBeenCalledTimes(1)
  })

  it('critério 5: a troca que falha mostra o motivo e o comando', async () => {
    vi.mocked(fetchUpdate).mockImplementation(async () => ({
      ...base,
      last: { state: 'not done', detail: 'checksum mismatch; the old executable is untouched', fix: 'brew upgrade henriquesakata/tap/cerne' },
    }))
    renderNav()
    expect(await screen.findByText('A atualização não terminou: checksum mismatch; the old executable is untouched')).toBeInTheDocument()
    expect(screen.getByText('brew upgrade henriquesakata/tap/cerne')).toBeInTheDocument()
    // O botão segue lá para tentar de novo.
    expect(screen.getByRole('button', { name: 'Atualizar para 0.15.0' })).toBeInTheDocument()
  })

  it('critério 6: sem troca por dentro, mostra a versão nova e o comando, sem botão de troca', async () => {
    vi.mocked(fetchUpdate).mockImplementation(async () => ({
      ...base,
      swappable: false,
      why: 'this is a local build (version dev), not a release: it is not replaced',
      fix: 'go install github.com/henriquesakata/cerne/cmd/cerne@latest',
    }))
    renderNav()
    const chip = await screen.findByRole('button', { name: '0.15.0 disponível' })
    expect(screen.queryByRole('button', { name: /Atualizar para/ })).not.toBeInTheDocument()
    fireEvent.click(chip)
    expect(screen.getByText('go install github.com/henriquesakata/cerne/cmd/cerne@latest')).toBeInTheDocument()
    expect(startUpdate).not.toHaveBeenCalled()
  })
})
