import { readFileSync } from 'node:fs'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import Handoffs, { HandoffPage } from './Handoffs'
import type { Handoff } from './api'
import { I18nProvider } from './i18n'

const api = vi.hoisted(() => ({
  fetchHandoffs: vi.fn<() => Promise<Handoff[]>>(),
  fetchHandoff: vi.fn<(id: string) => Promise<Handoff>>(),
}))
vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  fetchHandoffs: api.fetchHandoffs,
  fetchHandoff: api.fetchHandoff,
}))

// Versão ao vivo controlável (F-0300): a tela re-busca quando ela muda.
const live = vi.hoisted(() => ({ version: 0 }))
vi.mock('./live', () => ({ useLiveVersion: () => live.version }))

// A ordem é a do servidor (pendentes da mais antiga, depois fechadas da mais
// recente): a tela não reordena.
const ho7: Handoff = {
  id: 'HO-0007',
  state: 'pending',
  from_work: 'W-0010',
  from_work_title: 'Investigar erro X',
  actor: 'a@b',
  next: 'rodar X',
  brief: 'Parei no X.',
  created_at: '2026-11-12T20:30:00Z',
  updated_at: '2026-11-12T20:30:00Z',
}
const ho6: Handoff = {
  id: 'HO-0006',
  state: 'taken',
  from_work: 'W-0011',
  from_work_title: 'Refatorar Y',
  refs: ['F-0386', 'W-0003'],
  actor: 'a@b',
  next: 'rodar os testes de Y',
  contingencies: ['teste vermelho → reverter o último commit'],
  taken_by: 'c@d',
  taken_work: 'W-0012',
  taken_at: '2026-11-12T22:15:00Z',
  taken_note: 'entendi Y; começo pelos testes',
  brief: 'Parei no parser.\n\nEvidência:\n- internal/x/parser.go\n',
  created_at: '2026-11-12T18:00:00Z',
  updated_at: '2026-11-12T22:15:00Z',
}
const ho5: Handoff = {
  id: 'HO-0005',
  state: 'superseded',
  from_work: 'W-0010',
  from_work_title: 'Investigar erro X',
  actor: 'a@b',
  next: 'n',
  superseded_by: 'HO-0007',
  brief: 'b5',
  created_at: '2026-11-12T10:00:00Z',
  updated_at: '2026-11-12T20:30:00Z',
}
// Its W- is gone: the row falls back to the next step.
const ho4: Handoff = {
  id: 'HO-0004',
  state: 'cancelled',
  from_work: 'W-0009',
  from_work_title: '',
  actor: 'a@b',
  next: 'reproduzir o bug do cache',
  cancel_reason: 'o bug sumiu',
  brief: 'b4',
  created_at: '2026-11-11T10:00:00Z',
  updated_at: '2026-11-12T09:00:00Z',
}
const all = [ho7, ho6, ho5, ho4]

const filterGroup = () => screen.getByRole('group', { name: 'Estado' })
const stateButton = (label: string) => within(filterGroup()).getByRole('button', { name: new RegExp(`^${label}`) })
const toggle = (id: string) => screen.getByRole('button', { name: new RegExp(`^${id}`) })
const rowIds = () => screen.getAllByRole('listitem').map((li) => within(li).getByText(/^HO-\d{4}$/).textContent)
const rowOf = (id: string) => toggle(id).closest('li') as HTMLElement

beforeEach(() => {
  cleanup()
  // Só o relógio: os timers reais seguem para o waitFor.
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date('2026-11-12T23:30:00Z'))
  api.fetchHandoffs.mockReset()
  api.fetchHandoffs.mockResolvedValue(all)
  api.fetchHandoff.mockReset()
  live.version = 0
  window.location.hash = ''
})

afterEach(() => {
  vi.useRealTimers()
  localStorage.clear()
})

describe('Passagens — filtro por estado (F-0421 crit. 1)', () => {
  it('abre em Pendentes, com as pílulas dos estados da CLI e a contagem de cada uma', async () => {
    render(<Handoffs hash="#/handoffs" />)
    await screen.findByText('Investigar erro X')
    expect(rowIds()).toEqual(['HO-0007'])
    expect(within(filterGroup()).getAllByRole('button').map((b) => b.textContent)).toEqual([
      'Pendentes 1',
      'Assumidas 1',
      'Substituídas 1',
      'Canceladas 1',
      'Todas 4',
    ])
    expect(stateButton('Pendentes')).toHaveAttribute('aria-pressed', 'true')
  })

  it('a pílula leva o estado ao endereço e recorta a lista na ordem do servidor', async () => {
    const { rerender } = render(<Handoffs hash="#/handoffs" />)
    await screen.findByText('Investigar erro X')
    fireEvent.click(stateButton('Canceladas'))
    expect(window.location.hash).toBe('#/handoffs?state=cancelled')
    rerender(<Handoffs hash={window.location.hash} />)
    expect(rowIds()).toEqual(['HO-0004'])
    expect(stateButton('Canceladas')).toHaveAttribute('aria-pressed', 'true')
    fireEvent.click(stateButton('Todas'))
    expect(window.location.hash).toBe('#/handoffs?state=all')
    rerender(<Handoffs hash={window.location.hash} />)
    expect(rowIds()).toEqual(['HO-0007', 'HO-0006', 'HO-0005', 'HO-0004'])
  })

  it('um link direto chega já filtrado', async () => {
    render(<Handoffs hash="#/handoffs?state=taken" />)
    await screen.findByText('Refatorar Y')
    expect(rowIds()).toEqual(['HO-0006'])
  })

  it('clicar no estado já ativo não navega', async () => {
    render(<Handoffs hash="#/handoffs" />)
    await screen.findByText('Investigar erro X')
    fireEvent.click(stateButton('Pendentes'))
    expect(window.location.hash).toBe('')
  })

  it('um estado desconhecido vira Pendentes, com o endereço reescrito no lugar', async () => {
    window.location.hash = '#/handoffs?state=bogus'
    render(<Handoffs hash={window.location.hash} />)
    await waitFor(() => expect(window.location.hash).toBe('#/handoffs?state=pending'))
    expect(await screen.findByText('Investigar erro X')).toBeInTheDocument()
    expect(stateButton('Pendentes')).toHaveAttribute('aria-pressed', 'true')
    expect(screen.queryByText(/bogus/)).toBeNull()
  })

  it('o filtro vazio diz qual estado está ativo', async () => {
    api.fetchHandoffs.mockResolvedValue([ho6])
    render(<Handoffs hash="#/handoffs" />)
    expect(await screen.findByText('Nenhuma passagem no filtro Pendentes.')).toBeInTheDocument()
    expect(screen.queryByText(/cerne work handoff/)).toBeNull()
  })

  it('sem passagem nenhuma, ensina como uma nasce', async () => {
    api.fetchHandoffs.mockResolvedValue([])
    render(<Handoffs hash="#/handoffs" />)
    expect(await screen.findByText('cerne work handoff W-… --next "…"')).toBeInTheDocument()
    expect(screen.queryAllByRole('listitem')).toHaveLength(0)
    expect(screen.queryByRole('button', { name: 'Expandir tudo' })).toBeNull()
  })
})

describe('Passagens — contagem e expandir tudo (F-0421 crit. 2)', () => {
  it('mostra a contagem das passagens mostradas', async () => {
    render(<Handoffs hash="#/handoffs?state=all" />)
    await screen.findByText('Refatorar Y')
    expect(screen.getByText('4 passagens')).toBeInTheDocument()
  })

  it('Expandir tudo abre todas as linhas, Recolher tudo fecha, e cada um desabilita no seu estado', async () => {
    render(<Handoffs hash="#/handoffs?state=all" />)
    await screen.findByText('Refatorar Y')
    const ids = ['HO-0007', 'HO-0006', 'HO-0005', 'HO-0004']
    const expand = screen.getByRole('button', { name: 'Expandir tudo' })
    const collapse = screen.getByRole('button', { name: 'Recolher tudo' })
    // crit. 6: the controls wear the F-0416 queue classes (font: inherit, 6px, 2px focus), none the browser default.
    expect([expand, collapse].map((b) => b.className)).toEqual(['queue-btn', 'queue-btn'])
    expect(ids.map((id) => toggle(id).className)).toEqual(Array(4).fill('queue-toggle'))
    expect(collapse).toBeDisabled()
    fireEvent.click(expand)
    expect(ids.map((id) => toggle(id).getAttribute('aria-expanded'))).toEqual(['true', 'true', 'true', 'true'])
    expect(expand).toBeDisabled()
    fireEvent.click(collapse)
    expect(ids.map((id) => toggle(id).getAttribute('aria-expanded'))).toEqual(['false', 'false', 'false', 'false'])
    expect(collapse).toBeDisabled()
  })
})

describe('Passagens — linha recolhida (F-0421 crit. 3)', () => {
  it('chevron, id, título do W- de origem e o metadado; a pendente sem chip', async () => {
    render(<Handoffs hash="#/handoffs" />)
    await screen.findByText('Investigar erro X')
    const t = toggle('HO-0007')
    expect(t).toHaveAttribute('aria-expanded', 'false')
    expect(t.querySelector('svg.queue-chevron')).not.toBeNull()
    expect(within(t).getByText('Investigar erro X')).toHaveClass('queue-title')
    const row = rowOf('HO-0007')
    expect(within(row).queryByText('pendente')).toBeNull()
    expect(within(row).getByRole('link', { name: 'W-0010' })).toHaveAttribute('href', '#/activity')
    expect(row).toHaveTextContent('passada por a@b')
    const age = within(row).getByText('há 3 horas')
    expect(age).toHaveAttribute('title', '2026-11-12 20:30 UTC')
    // Collapsed, the detail is not in the DOM.
    expect(within(row).queryByText('rodar X')).toBeNull()
  })

  it('a assumida: chip, W- de origem → W- novo, quem assumiu e a idade do fechamento', async () => {
    render(<Handoffs hash="#/handoffs?state=taken" />)
    await screen.findByText('Refatorar Y')
    const row = rowOf('HO-0006')
    expect(within(row).getByText('assumida')).toHaveClass('chip')
    expect(within(row).getByRole('link', { name: 'W-0011' })).toBeInTheDocument()
    expect(within(row).getByRole('link', { name: 'W-0012' })).toBeInTheDocument()
    expect(row).toHaveTextContent('W-0011 → W-0012')
    // The actor is capped by CSS; the whole value stays in the title.
    expect(within(row).getByText('assumida por c@d')).toHaveAttribute('title', 'c@d')
    expect(within(row).getByText('há 1 hora')).toHaveAttribute('title', '2026-11-12 22:15 UTC')
  })

  // Crítica do F-0386: a lista das fechadas é ordenada pelo fechamento, então
  // a idade da linha é a do fechamento (taken_at, senão updated_at).
  it('substituída e cancelada envelhecem desde o fechamento', async () => {
    render(<Handoffs hash="#/handoffs?state=all" />)
    await screen.findByText('Refatorar Y')
    expect(within(rowOf('HO-0005')).getByText('há 3 horas')).toHaveAttribute('title', '2026-11-12 20:30 UTC')
    expect(within(rowOf('HO-0004')).getByText('há 14 horas')).toHaveAttribute('title', '2026-11-12 09:00 UTC')
  })

  it('sem o W- de origem, o texto curto é o próximo passo', async () => {
    render(<Handoffs hash="#/handoffs?state=cancelled" />)
    expect(await screen.findByText('reproduzir o bug do cache')).toHaveClass('queue-title')
  })
})

describe('Passagens — detalhe na linha (F-0421 crit. 4)', () => {
  it('o toggle abre o detalhe logo abaixo, com aria-controls, e um segundo clique recolhe', async () => {
    render(<Handoffs hash="#/handoffs?state=taken" />)
    await screen.findByText('Refatorar Y')
    const t = toggle('HO-0006')
    fireEvent.click(t)
    expect(t).toHaveAttribute('aria-expanded', 'true')
    const detail = document.getElementById(t.getAttribute('aria-controls') ?? '') as HTMLElement
    expect(detail).toBeInTheDocument()
    expect(detail).toHaveTextContent('rodar os testes de Y')
    fireEvent.click(t)
    expect(t).toHaveAttribute('aria-expanded', 'false')
    expect(document.getElementById(t.getAttribute('aria-controls') ?? '')).toBeNull()
  })

  it('assumida: próximo passo primeiro, contingências, read-back, os dois W-, refs, horas UTC, brief e o link da página', async () => {
    render(<Handoffs hash="#/handoffs?state=taken" />)
    await screen.findByText('Refatorar Y')
    fireEvent.click(toggle('HO-0006'))
    const detail = rowOf('HO-0006').querySelector('.queue-detail') as HTMLElement
    const headings = within(detail).getAllByRole('heading').map((h) => h.textContent)
    expect(headings).toEqual(['Próximo passo', 'Contingências', 'Read-back de quem recebeu', 'Brief'])
    expect(within(detail).getByText('teste vermelho → reverter o último commit')).toBeInTheDocument()
    expect(within(detail).getByText('entendi Y; começo pelos testes')).toBeInTheDocument()
    expect(detail).toHaveTextContent('W- de origem: W-0011 Refatorar Y')
    expect(detail).toHaveTextContent('W- novo: W-0012')
    expect(within(detail).getByRole('link', { name: 'F-0386' })).toHaveAttribute('href', '#/f/F-0386')
    expect(within(detail).getByText('W-0003')).toBeInTheDocument()
    expect(within(detail).queryByRole('link', { name: 'W-0003' })).toBeNull()
    expect(detail).toHaveTextContent('passada por a@b · Criada em 2026-11-12 18:00 UTC')
    expect(detail).toHaveTextContent('assumida por c@d · Fechada em 2026-11-12 22:15 UTC · W- novo: W-0012')
    expect(within(detail).getByText('internal/x/parser.go')).toBeInTheDocument()
    expect(within(detail).getByRole('link', { name: 'Abrir a página da passagem' })).toHaveAttribute(
      'href',
      '#/handoffs/HO-0006',
    )
  })

  it('pendente: sem read-back nem hora de fechamento', async () => {
    render(<Handoffs hash="#/handoffs" />)
    await screen.findByText('Investigar erro X')
    fireEvent.click(toggle('HO-0007'))
    const detail = rowOf('HO-0007').querySelector('.queue-detail') as HTMLElement
    expect(within(detail).getAllByRole('heading').map((h) => h.textContent)).toEqual(['Próximo passo', 'Brief'])
    expect(detail).toHaveTextContent('Criada em 2026-11-12 20:30 UTC')
    expect(detail).not.toHaveTextContent('Fechada em')
  })

  it('assumida sem read-back diz que não há', async () => {
    api.fetchHandoffs.mockResolvedValue([{ ...ho6, taken_note: undefined }])
    render(<Handoffs hash="#/handoffs?state=taken" />)
    await screen.findByText('Refatorar Y')
    fireEvent.click(toggle('HO-0006'))
    expect(screen.getByText('Nenhum read-back registrado.')).toBeInTheDocument()
  })

  it('substituída: a sucessora como link; cancelada: o motivo', async () => {
    render(<Handoffs hash="#/handoffs?state=all" />)
    await screen.findByText('Refatorar Y')
    fireEvent.click(toggle('HO-0005'))
    fireEvent.click(toggle('HO-0004'))
    const sup = rowOf('HO-0005').querySelector('.queue-detail') as HTMLElement
    expect(within(sup).getByRole('link', { name: 'HO-0007' })).toHaveAttribute('href', '#/handoffs/HO-0007')
    expect(rowOf('HO-0004').querySelector('.queue-detail')).toHaveTextContent('Motivo: o bug sumiu')
  })

  it('a tela segue somente leitura: só pílulas, expandir e as linhas', async () => {
    render(<Handoffs hash="#/handoffs?state=all" />)
    await screen.findByText('Refatorar Y')
    fireEvent.click(screen.getByRole('button', { name: 'Expandir tudo' }))
    const names = screen.getAllByRole('button').map((b) => b.textContent)
    expect(names.filter((n) => !/^HO-\d{4}/.test(n ?? ''))).toEqual([
      'Pendentes 1',
      'Assumidas 1',
      'Substituídas 1',
      'Canceladas 1',
      'Todas 4',
      'Expandir tudo',
      'Recolher tudo',
    ])
  })
})

describe('Passagens — ao vivo (F-0386 crit. 6)', () => {
  it('re-busca quando o backend avança', async () => {
    api.fetchHandoffs.mockResolvedValue([])
    const { rerender } = render(<Handoffs hash="#/handoffs" />)
    await screen.findByText('cerne work handoff W-… --next "…"')
    api.fetchHandoffs.mockResolvedValue(all)
    live.version = 1
    rerender(<Handoffs hash="#/handoffs" />)
    expect(await screen.findByText('Investigar erro X')).toBeInTheDocument()
  })
})

describe('Passagens — página da passagem (F-0421 crit. 5)', () => {
  it('o cabeçalho de hoje e o mesmo detalhe da linha', async () => {
    api.fetchHandoff.mockResolvedValue(ho6)
    render(<HandoffPage id="HO-0006" />)
    expect(await screen.findByText('Parei no parser.')).toBeInTheDocument()
    expect(api.fetchHandoff).toHaveBeenCalledWith('HO-0006')
    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('HO-0006')
    expect(screen.getByRole('link', { name: /Passagens/ })).toHaveAttribute('href', '#/handoffs')
    const main = screen.getByRole('main')
    expect(within(main).getAllByRole('heading', { level: 2 }).map((h) => h.textContent)).toEqual([
      'Próximo passo',
      'Contingências',
      'Read-back de quem recebeu',
      'Brief',
    ])
    expect(main).toHaveTextContent('W- novo: W-0012')
    expect(main).toHaveTextContent('Fechada em 2026-11-12 22:15 UTC')
    expect(screen.getByRole('link', { name: 'F-0386' })).toHaveAttribute('href', '#/f/F-0386')
    // The page is where the link points: it does not link to itself.
    expect(screen.queryByRole('link', { name: 'Abrir a página da passagem' })).toBeNull()
    expect(screen.queryByRole('button')).toBeNull()
  })

  it('substituída: link para a passagem nova', async () => {
    api.fetchHandoff.mockResolvedValue(ho5)
    render(<HandoffPage id="HO-0005" />)
    expect(await screen.findByText('substituída')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'HO-0007' })).toHaveAttribute('href', '#/handoffs/HO-0007')
  })

  it('um id desconhecido mostra o erro da API', async () => {
    api.fetchHandoff.mockRejectedValue(new Error('handoff not found'))
    render(<HandoffPage id="HO-9999" />)
    expect(await screen.findByText('Erro ao carregar a passagem: handoff not found')).toBeInTheDocument()
  })
})

describe('Passagens — inglês (F-0421 crit. 9)', () => {
  it('título, pílulas, estados, contagem e detalhe em inglês', async () => {
    localStorage.setItem('cerne-lang', 'en')
    render(
      <I18nProvider>
        <Handoffs hash="#/handoffs?state=all" />
      </I18nProvider>,
    )
    await screen.findByText('Refatorar Y')
    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('Handoffs')
    const group = screen.getByRole('group', { name: 'State' })
    expect(within(group).getAllByRole('button').map((b) => b.textContent)).toEqual([
      'Pending 1',
      'Taken 1',
      'Superseded 1',
      'Cancelled 1',
      'All 4',
    ])
    for (const s of ['taken', 'superseded', 'cancelled']) expect(screen.getByText(s)).toBeInTheDocument()
    expect(screen.getByText('4 handoffs')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Expand all' }))
    expect(rowOf('HO-0006')).toHaveTextContent('Closed at 2026-11-12 22:15 UTC')
    expect(screen.getAllByRole('link', { name: "Open the handoff's page" })).toHaveLength(4)
  })
})

describe('Passagens — CSS (F-0421 crit. 6)', () => {
  const css = readFileSync('src/app.css', 'utf8')
  it('nenhum literal de cor e nenhuma transição nas regras da passagem (DESIGN.md)', () => {
    const own = css.match(/\n[^\n{}]*\.handoff-[^{]*\{[^}]*\}/g)!.join('\n')
    expect(own).toContain('.handoff-next')
    expect(own).not.toMatch(/#[0-9a-f]{3,8}\b|rgb\(|transition|animation/i)
  })
})
