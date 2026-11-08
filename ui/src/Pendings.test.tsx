import { readFileSync } from 'node:fs'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import Pendings from './Pendings'
import type { Pending } from './api'
import { I18nProvider } from './i18n'

const api = vi.hoisted(() => ({
  fetchPendings: vi.fn<(f?: { state?: string; from?: string; component?: string }) => Promise<Pending[]>>(),
}))
vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  fetchPendings: api.fetchPendings,
}))

// Versão ao vivo controlável (F-0300): a tela re-busca quando ela muda.
const live = vi.hoisted(() => ({ version: 0 }))
vi.mock('./live', () => ({ useLiveVersion: () => live.version }))

const DAY = 86_400_000
const p2: Pending = {
  id: 'P-0002',
  title: 'Cobrir o caso de links quebrados',
  from: 'F-0331',
  state: 'open',
  components: ['lint', 'ui'],
  created_at: new Date(Date.now() - 3 * DAY - 60_000).toISOString(),
  updated_at: new Date(Date.now() - 3 * DAY - 60_000).toISOString(),
}
const p1: Pending = {
  id: 'P-0001',
  title: 'Documentar a rota nova',
  body: 'contexto do débito',
  from: 'F-0330',
  state: 'open',
  created_at: '2026-11-01T12:00:00Z',
  updated_at: '2026-11-01T12:00:00Z',
}
const p5: Pending = {
  id: 'P-0005',
  title: 'Trocar o cache',
  from: 'F-0330',
  state: 'dropped',
  reason: 'fora de escopo',
  created_at: '2026-10-20T12:00:00Z',
  updated_at: '2026-10-25T12:00:00Z',
  closed_at: '2026-10-25T12:00:00Z',
}
const p6: Pending = {
  id: 'P-0006',
  title: 'Paginar a lista',
  from: 'F-0330',
  state: 'promoted',
  promoted_to: 'F-0340',
  created_at: '2026-10-21T12:00:00Z',
  updated_at: '2026-10-26T12:00:00Z',
  closed_at: '2026-10-26T12:00:00Z',
}

const lastCall = () => api.fetchPendings.mock.calls.at(-1)?.[0]
const toggle = (id: string) => screen.getByRole('button', { name: new RegExp(`^${id}`) })
const stateGroup = () => screen.getByRole('group', { name: 'Estado' })
const stateButton = (label: string) => within(stateGroup()).getByRole('button', { name: new RegExp(`^${label}`) })

beforeEach(() => {
  cleanup()
  api.fetchPendings.mockReset()
  api.fetchPendings.mockResolvedValue([p2, p1])
  live.version = 0
  window.location.hash = ''
})

afterEach(() => localStorage.clear())

describe('Pendings — lista (F-0334 crit. 1)', () => {
  it('busca as abertas e mostra id, título, idade, origem como link e componentes, na ordem recebida', async () => {
    render(<Pendings hash="#/pendings" />)
    await screen.findByText('Cobrir o caso de links quebrados')
    expect(api.fetchPendings).toHaveBeenCalledWith({ state: 'all', from: '', component: '' })
    const ids = screen.getAllByRole('listitem').map((li) => within(li).getByText(/^P-\d{4}$/).textContent)
    expect(ids).toEqual(['P-0002', 'P-0001'])
    const row = screen.getAllByRole('listitem')[0]
    expect(within(row).getByText('3 dias')).toBeInTheDocument()
    expect(within(row).getByRole('link', { name: 'F-0331' })).toHaveAttribute('href', '#/f/F-0331')
    expect(within(row).getByRole('button', { name: 'lint' })).toBeInTheDocument()
    expect(within(row).getByRole('button', { name: 'ui' })).toBeInTheDocument()
  })
})

describe('Pendings — filtros no endereço (F-0334 crit. 2)', () => {
  it('trocar o estado leva o filtro ao hash e mostra só as daquele estado', async () => {
    api.fetchPendings.mockResolvedValue([p2, p1, p5, p6])
    const { rerender } = render(<Pendings hash="#/pendings" />)
    await screen.findByText('Documentar a rota nova')
    expect(screen.queryByText('Trocar o cache')).toBeNull()
    fireEvent.click(stateButton('Descartadas'))
    expect(window.location.hash).toBe('#/pendings?state=dropped')
    rerender(<Pendings hash={window.location.hash} />)
    expect(screen.getByText('Trocar o cache')).toBeInTheDocument()
    expect(screen.queryByText('Documentar a rota nova')).toBeNull()
    expect(screen.queryByText('Paginar a lista')).toBeNull()
    expect(stateButton('Descartadas')).toHaveAttribute('aria-pressed', 'true')
    expect(stateButton('Abertas')).toHaveAttribute('aria-pressed', 'false')
  })

  // Crítica C12: como a lista do GitHub, cada estado diz quantas tem, sobre os
  // mesmos filtros de origem e componente.
  it('cada botão de estado leva a contagem daquele estado', async () => {
    api.fetchPendings.mockResolvedValue([p2, p1, p5, p6])
    render(<Pendings hash="#/pendings?from=F-0330" />)
    await screen.findByText('Documentar a rota nova')
    expect(lastCall()).toEqual({ state: 'all', from: 'F-0330', component: '' })
    expect(within(stateGroup()).getAllByRole('button').map((b) => b.textContent)).toEqual([
      'Abertas 2',
      'Resolvidas 0',
      'Descartadas 1',
      'Promovidas 1',
      'Todas 4',
    ])
  })

  it('um link direto chega já filtrado', async () => {
    render(<Pendings hash="#/pendings?state=all&from=F-0331&component=lint" />)
    await screen.findByText('Documentar a rota nova')
    expect(api.fetchPendings).toHaveBeenCalledWith({ state: 'all', from: 'F-0331', component: 'lint' })
    expect(screen.getByLabelText('Origem')).toHaveValue('F-0331')
    expect(screen.getByLabelText('Componente')).toHaveValue('lint')
    expect(stateButton('Todas')).toHaveAttribute('aria-pressed', 'true')
  })

  it('o form de origem, o chip de componente e o limpar filtros mudam o hash', async () => {
    const { rerender } = render(<Pendings hash="#/pendings" />)
    await screen.findByText('Documentar a rota nova')
    expect(screen.queryByRole('link', { name: 'Limpar filtros' })).toBeNull()
    fireEvent.change(screen.getByLabelText('Origem'), { target: { value: ' F-0331 ' } })
    fireEvent.click(screen.getByRole('button', { name: 'Filtrar' }))
    expect(window.location.hash).toBe('#/pendings?state=open&from=F-0331')
    rerender(<Pendings hash={window.location.hash} />)
    await waitFor(() => expect(lastCall()).toEqual({ state: 'all', from: 'F-0331', component: '' }))
    fireEvent.click(within(screen.getAllByRole('listitem')[0]).getByRole('button', { name: 'lint' }))
    expect(window.location.hash).toBe('#/pendings?state=open&from=F-0331&component=lint')
    rerender(<Pendings hash={window.location.hash} />)
    await waitFor(() => expect(lastCall()).toEqual({ state: 'all', from: 'F-0331', component: 'lint' }))
    expect(screen.getByRole('link', { name: 'Limpar filtros' })).toHaveAttribute('href', '#/pendings')
  })

  // Crítica C7: o chip ativo desliga o filtro, e "ativo" compara pelo slug,
  // como o servidor casa o componente (LINT e "Ação UI" filtram).
  it('o chip do componente ativo fica pressionado pelo slug e desliga o filtro', async () => {
    api.fetchPendings.mockResolvedValue([{ ...p2, components: ['lint', 'acao-ui'] }])
    const { rerender } = render(<Pendings hash="#/pendings?state=open&component=LINT" />)
    const lint = await screen.findByRole('button', { name: 'lint' })
    expect(lint).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: 'acao-ui' })).toHaveAttribute('aria-pressed', 'false')
    fireEvent.click(lint)
    expect(window.location.hash).toBe('#/pendings?state=open')
    rerender(<Pendings hash="#/pendings?state=open&component=A%C3%A7%C3%A3o+UI" />)
    await waitFor(() => expect(screen.getByRole('button', { name: 'acao-ui' })).toHaveAttribute('aria-pressed', 'true'))
    expect(screen.getByRole('button', { name: 'lint' })).toHaveAttribute('aria-pressed', 'false')
  })

  // Crítica C8: os campos mostram o filtro aplicado — um texto digitado e não
  // enviado some quando outra navegação muda o filtro.
  it('trocar o estado remonta os campos a partir do hash', async () => {
    const { rerender } = render(<Pendings hash="#/pendings" />)
    await screen.findByText('Documentar a rota nova')
    fireEvent.change(screen.getByLabelText('Origem'), { target: { value: 'F-0002' } })
    fireEvent.click(stateButton('Todas'))
    expect(window.location.hash).toBe('#/pendings?state=all')
    rerender(<Pendings hash={window.location.hash} />)
    expect(screen.getByLabelText('Origem')).toHaveValue('')
  })

  // Crítica C9: um estado desconhecido no link cai em abertas, com o hash
  // reescrito no lugar (sem entrada no histórico) e sem erro nem "Tentar de novo".
  it('um estado inválido no link vira abertas', async () => {
    window.location.hash = '#/pendings?state=bogus&from=F-0331'
    render(<Pendings hash={window.location.hash} />)
    await waitFor(() => expect(window.location.hash).toBe('#/pendings?state=open&from=F-0331'))
    expect(await screen.findByText('Documentar a rota nova')).toBeInTheDocument()
    expect(stateButton('Abertas')).toHaveAttribute('aria-pressed', 'true')
    expect(screen.queryByRole('button', { name: 'Tentar de novo' })).toBeNull()
    expect(screen.queryByText(/bogus/)).toBeNull()
  })

  // Crítica C19: repetir o filtro que já vale não navega — o Voltar não precisa
  // de cliques a mais.
  it('clicar no estado já ativo, ou filtrar com os mesmos valores, não navega', async () => {
    window.location.hash = '#/pendings'
    render(<Pendings hash={window.location.hash} />)
    await screen.findByText('Documentar a rota nova')
    const before = window.history.length
    fireEvent.click(stateButton('Abertas'))
    fireEvent.click(screen.getByRole('button', { name: 'Filtrar' }))
    expect(window.location.hash).toBe('#/pendings')
    expect(window.history.length).toBe(before)
  })

  it('a seleção vazia diz qual filtro está ativo', async () => {
    api.fetchPendings.mockResolvedValue([])
    render(<Pendings hash="#/pendings?state=dropped&from=F-0331" />)
    const msg = await screen.findByText(/Nenhuma pendência/)
    expect(msg).toHaveTextContent('Descartadas')
    expect(msg).toHaveTextContent('F-0331')
    expect(screen.queryByRole('list')).toBeNull()
  })

  it('mostra a contagem', async () => {
    render(<Pendings hash="#/pendings" />)
    expect(await screen.findByText('2 pendências')).toBeInTheDocument()
  })
})

describe('Pendings — linha expandida (F-0334 crit. 3)', () => {
  it('a aberta mostra o corpo, a criação e os três comandos copiáveis', async () => {
    const writeText = vi.fn(async () => {})
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
    render(<Pendings hash="#/pendings" />)
    await screen.findByText('Documentar a rota nova')
    const t = toggle('P-0001')
    expect(t).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByText('contexto do débito')).toBeNull()
    fireEvent.click(t)
    expect(t).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getByText('contexto do débito')).toBeInTheDocument()
    expect(
      screen.getByText(`Criada em ${new Date(p1.created_at).toLocaleDateString('pt-BR')}`),
    ).toBeInTheDocument()
    const pres = Array.from(document.querySelectorAll('pre')).map((p) => p.textContent)
    expect(pres).toEqual([
      'cerne pending resolve P-0001',
      'cerne pending drop P-0001 --reason ""',
      'cerne pending promote P-0001 --as feature',
    ])
    const names = ['Copiar comando de resolver P-0001', 'Copiar comando de descartar P-0001', 'Copiar comando de promover P-0001']
    for (const name of names) expect(screen.getByRole('button', { name })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: names[0] }))
    expect(writeText).toHaveBeenCalledWith('cerne pending resolve P-0001')
  })

  it('a descartada mostra o motivo e o fechamento, sem comandos; a promovida liga o id criado', async () => {
    api.fetchPendings.mockResolvedValue([p5, p6])
    render(<Pendings hash="#/pendings?state=all" />)
    await screen.findByText('Trocar o cache')
    fireEvent.click(toggle('P-0005'))
    const detail = document.querySelector('.pending-detail') as HTMLElement
    expect(within(detail).getByText('Motivo: fora de escopo')).toBeInTheDocument()
    expect(
      within(detail).getByText(`Fechada em ${new Date(p5.closed_at!).toLocaleDateString('pt-BR')}`),
    ).toBeInTheDocument()
    expect(document.querySelectorAll('pre')).toHaveLength(0)
    fireEvent.click(toggle('P-0006'))
    expect(screen.getByText('Promovida para')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'F-0340' })).toHaveAttribute('href', '#/f/F-0340')
    expect(document.querySelectorAll('pre')).toHaveLength(0)
  })
})

// Crítica C11: a idade de uma pendência fechada pararia de fazer sentido (uma
// resolvida no dia seguinte leria "40 dias" um mês depois): a linha fechada
// mostra quando fechou; a aberta, a idade.
describe('Pendings — idade só na aberta (crítica C11)', () => {
  it('a linha fechada mostra a data de fechamento, não a idade', async () => {
    api.fetchPendings.mockResolvedValue([p2, p5])
    render(<Pendings hash="#/pendings?state=all" />)
    await screen.findByText('Trocar o cache')
    const [open, closed] = screen.getAllByRole('listitem')
    expect(within(open).getByText('3 dias')).toBeInTheDocument()
    expect(within(closed).getByText(`Fechada em ${new Date(p5.closed_at!).toLocaleDateString('pt-BR')}`)).toBeInTheDocument()
    expect(within(closed).queryByText(/\d+ dias?$/)).toBeNull()
  })
})

describe('Pendings — ao vivo (F-0334 crit. 6)', () => {
  it('um avanço do backend refaz a busca e a pendência nova aparece', async () => {
    api.fetchPendings.mockResolvedValue([p1])
    const { rerender } = render(<Pendings hash="#/pendings" />)
    await screen.findByText('Documentar a rota nova')
    api.fetchPendings.mockResolvedValue([p1, p2])
    live.version = 1
    rerender(<Pendings hash="#/pendings" />)
    expect(await screen.findByText('Cobrir o caso de links quebrados')).toBeInTheDocument()
    expect(api.fetchPendings).toHaveBeenCalledTimes(2)
  })
})

describe('Pendings — inglês (F-0334 crit. 8)', () => {
  it('título, estados e contagem em inglês', async () => {
    localStorage.setItem('cerne-lang', 'en')
    render(
      <I18nProvider>
        <Pendings hash="#/pendings" />
      </I18nProvider>,
    )
    expect(await screen.findByText('2 pendings')).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('Pendings')
    const group = screen.getByRole('group', { name: 'State' })
    expect(within(group).getAllByRole('button').map((b) => b.textContent)).toEqual([
      'Open 2',
      'Resolved 0',
      'Dropped 0',
      'Promoted 0',
      'All 2',
    ])
  })
})

// Crítica C10: em 375 px o botão de copiar cobria o fim do comando (as aspas
// do --reason), os ids quebravam no meio e o campo de componente vazava do
// form. O layout é CSS: fixa as regras e que a tela usa as classes delas.
describe('Pendings — largura de celular (crítica C10)', () => {
  const css = readFileSync('src/app.css', 'utf8')
  const rule = (sel: string) =>
    new RegExp(`(^|\\n)${sel.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')} \\{([^}]*)\\}`).exec(css)?.[2] ?? ''

  it('o comando rola sob o botão, os ids não quebram e o form quebra a linha', () => {
    expect(rule('.pending-detail .harness-fix pre')).toMatch(/padding-right: 5rem/)
    expect(rule('.pending-detail .harness-fix pre')).toMatch(/overflow-x: auto/)
    expect(rule('.pending-row .card-id, .pending-row a')).toMatch(/white-space: nowrap/)
    expect(rule('.pending-row')).toMatch(/flex-wrap: wrap/)
    expect(rule('.pendings-form')).toMatch(/flex-wrap: wrap/)
    expect(rule('.pendings-form label')).toMatch(/min-width: 0/)
  })

  it('a linha e o form usam essas classes', async () => {
    render(<Pendings hash="#/pendings" />)
    await screen.findByText('Documentar a rota nova')
    expect(screen.getAllByRole('listitem')[0].querySelector('.pending-row')).not.toBeNull()
    expect(screen.getByRole('button', { name: 'Filtrar' }).closest('form')).toHaveClass('pendings-form')
  })
})
