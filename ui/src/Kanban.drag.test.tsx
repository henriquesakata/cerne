import { readFileSync } from 'node:fs'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, cleanup, createEvent, fireEvent, render, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import Kanban, { offscreenRight } from './Kanban'
import type { Feature, StateResponse } from './api'
import { postTransition, transitionBody } from './api'

const feature = (id: string, state: Feature['state']): Feature => ({
  id,
  title: `Feature ${id}`,
  module: '',
  state,
  created_at: '2026-07-25T00:00:00Z',
  updated_at: '2026-07-25T00:00:00Z',
})

let payload: StateResponse
const mkState = (open?: Record<string, number>): StateResponse => ({
  project: { name: 'Demo', modules: [] },
  features: [feature('F-0001', 'candidate'), feature('F-0002', 'implemented'), feature('F-0003', 'in_development')],
  decisions: [],
  references: [],
  runbooks: [],
  notes: [],
  ...(open ? { open_clarifications: open } : {}),
})

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  fetchState: vi.fn(async () => payload),
  fetchLint: vi.fn(async () => []),
  postTransition: vi.fn(),
  transitionBody: vi.fn((action: string) => ({ action, sentinel: true })),
}))
const live = vi.hoisted(() => ({ version: 0 }))
vi.mock('./live', () => ({ useLiveVersion: () => live.version }))

const noticeEl = () => document.querySelector('.drag-notice') as HTMLElement
const findNotice = () => waitFor(() => { expect(noticeEl()).not.toBeEmptyDOMElement(); return noticeEl() })
const post = vi.mocked(postTransition)

const dt = () => ({ data: {} as Record<string, string>, dropEffect: 'move', effectAllowed: 'all',
  setData(k: string, v: string) { this.data[k] = v }, getData(k: string) { return this.data[k] ?? '' } })

const cardOf = (id: string) => screen.getAllByText(id).map((e) => e.closest('a.card')).find(Boolean) as HTMLElement
const columnOf = (name: string) => screen.getByRole('heading', { name: new RegExp(name) }).closest('section') as HTMLElement

async function drag(id: string, target: HTMLElement) {
  const dataTransfer = dt()
  fireEvent.dragStart(cardOf(id), { dataTransfer })
  fireEvent.dragOver(target, { dataTransfer })
  fireEvent.drop(target, { dataTransfer })
  return dataTransfer
}

beforeEach(() => {
  localStorage.clear()
  payload = mkState()
  post.mockReset()
})
afterEach(cleanup)

describe('Kanban drag (F-0388)', () => {
  it('Backlog -> Aprovadas aprova com o mesmo pedido da pagina', async () => {
    post.mockResolvedValue({ ok: true, data: { id: 'F-0001', from: 'candidate', to: 'approved' } })
    render(<Kanban />)
    await screen.findByText('F-0001')
    await drag('F-0001', columnOf('Aprovadas'))
    // pagina chama postTransition(id, { action, by: undefined, reason: undefined })
    await waitFor(() => expect(post).toHaveBeenCalledWith('F-0001', { action: 'approve', sentinel: true }))
    expect(await findNotice()).toHaveTextContent('Candidata → Aprovada')
  })

  it('Pronta -> Concluida aceita', async () => {
    post.mockResolvedValue({ ok: true, data: { id: 'F-0002', from: 'implemented', to: 'done' } })
    render(<Kanban />)
    await screen.findByText('F-0002')
    await drag('F-0002', columnOf('Conclu'))
    await waitFor(() => expect(post).toHaveBeenCalledWith('F-0002', { action: 'accept', sentinel: true }))
  })

  it('esclarecimento aberto: bloqueia no dragover, avisa no drop, sem API', async () => {
    payload = mkState({ 'F-0001': 2 })
    render(<Kanban />)
    await screen.findByText('F-0001')
    const col = columnOf('Aprovadas')
    const dataTransfer = dt()
    fireEvent.dragStart(cardOf('F-0001'), { dataTransfer })
    fireEvent.dragOver(col, { dataTransfer })
    expect(col).toHaveClass('drop-blocked')
    fireEvent.drop(col, { dataTransfer })
    const msg = await screen.findByText(/Responda os 2 esclarecimentos antes de aprovar/)
    expect(msg.closest('.drag-notice')?.querySelector('a')).toHaveAttribute('href', '#/f/F-0001')
    expect(post).not.toHaveBeenCalled()
  })

  it('recusa do servidor: mostra a mensagem do gate com link, sem mover o card', async () => {
    post.mockResolvedValue({ ok: false, error: 'gate: spec vazia' })
    render(<Kanban />)
    await screen.findByText('F-0001')
    await drag('F-0001', columnOf('Aprovadas'))
    const alert = await findNotice()
    expect(alert).toHaveTextContent('gate: spec vazia')
    expect(alert.querySelector('a')).toHaveAttribute('href', '#/f/F-0001')
    expect(columnOf('Aprovadas')).not.toHaveTextContent('F-0001')
  })

  it('recusa do accept tambem mostra a mensagem', async () => {
    post.mockResolvedValue({ ok: false, error: 'humano apenas' })
    render(<Kanban />)
    await screen.findByText('F-0002')
    await drag('F-0002', columnOf('Conclu'))
    expect(await findNotice()).toHaveTextContent('humano apenas')
  })

  it('pares invalidos: dropEffect none e nenhuma chamada', async () => {
    render(<Kanban />)
    await screen.findByText('F-0001')
    for (const [id, col] of [['F-0001', 'Conclu'], ['F-0002', 'Aprovadas'], ['F-0003', 'Aprovadas'], ['F-0003', 'Conclu']] as const) {
      const dataTransfer = dt()
      fireEvent.dragStart(cardOf(id), { dataTransfer })
      fireEvent.dragOver(columnOf(col), { dataTransfer })
      expect(dataTransfer.dropEffect).toBe('none')
      fireEvent.drop(columnOf(col), { dataTransfer })
    }
    expect(post).not.toHaveBeenCalled()
  })

  it('card arrastavel tem aria-description; os demais nao', async () => {
    render(<Kanban />)
    await screen.findByText('F-0001')
    expect(cardOf('F-0001')).toHaveAttribute('draggable', 'true')
    expect(cardOf('F-0001').getAttribute('aria-description')).toMatch(/página da feature/)
    expect(cardOf('F-0002')).toHaveAttribute('draggable', 'true')
    expect(cardOf('F-0003')).not.toHaveAttribute('draggable')
  })

  it('o pedido passa pelo transitionBody compartilhado com a pagina', async () => {
    post.mockResolvedValue({ ok: true, data: { id: 'F-0001', from: 'candidate', to: 'approved' } })
    render(<Kanban />)
    await screen.findByText('F-0001')
    await drag('F-0001', columnOf('Aprovadas'))
    await waitFor(() => expect(post).toHaveBeenCalled())
    expect(vi.mocked(transitionBody)).toHaveBeenCalledWith('approve')
  })

  it('arraste cancelado (dragend sem drop): arraste de card nao arrastavel nao aprova', async () => {
    render(<Kanban />)
    await screen.findByText('F-0001')
    fireEvent.dragStart(cardOf('F-0001'), { dataTransfer: dt() })
    fireEvent.dragEnd(cardOf('F-0001'))
    const other = dt()
    fireEvent.dragStart(cardOf('F-0003'), { dataTransfer: other })
    fireEvent.dragOver(columnOf('Aprovadas'), { dataTransfer: other })
    fireEvent.drop(columnOf('Aprovadas'), { dataTransfer: other })
    expect(post).not.toHaveBeenCalled()
  })

  it('drop de card nao arrastavel sem dragend previo tambem nao aprova', async () => {
    render(<Kanban />)
    await screen.findByText('F-0001')
    fireEvent.dragStart(cardOf('F-0001'), { dataTransfer: dt() })
    const other = dt()
    fireEvent.dragStart(cardOf('F-0003'), { dataTransfer: other })
    fireEvent.drop(columnOf('Aprovadas'), { dataTransfer: other })
    expect(post).not.toHaveBeenCalled()
  })

  it('drop sem o payload do gesto (arraste externo) nao aprova', async () => {
    render(<Kanban />)
    await screen.findByText('F-0001')
    fireEvent.dragStart(cardOf('F-0001'), { dataTransfer: dt() })
    fireEvent.drop(columnOf('Aprovadas'), { dataTransfer: dt() })
    expect(post).not.toHaveBeenCalled()
  })

  it('drop em coluna recusada e depois outro drop: nao reaproveita o arraste', async () => {
    post.mockResolvedValue({ ok: true, data: { id: 'F-0001', from: 'candidate', to: 'approved' } })
    render(<Kanban />)
    await screen.findByText('F-0001')
    const a = dt()
    fireEvent.dragStart(cardOf('F-0001'), { dataTransfer: a })
    fireEvent.drop(columnOf('Conclu'), { dataTransfer: a })
    fireEvent.dragEnd(cardOf('F-0001'))
    const b = dt()
    fireEvent.dragStart(cardOf('F-0003'), { dataTransfer: b })
    fireEvent.drop(columnOf('Aprovadas'), { dataTransfer: b })
    expect(post).not.toHaveBeenCalled()
  })

  it('esclarecimento aberto bloqueia so o approve: implemented com pendencia aceita', async () => {
    payload = mkState({ 'F-0002': 3 })
    post.mockResolvedValue({ ok: true, data: { id: 'F-0002', from: 'implemented', to: 'done' } })
    render(<Kanban />)
    await screen.findByText('F-0002')
    await drag('F-0002', columnOf('Conclu'))
    await waitFor(() => expect(post).toHaveBeenCalledWith('F-0002', { action: 'accept', sentinel: true }))
    expect(columnOf('Conclu')).not.toHaveClass('drop-blocked')
  })

  it('rejeicao de rede mostra aviso de recusa', async () => {
    post.mockRejectedValue(new Error('network down'))
    render(<Kanban />)
    await screen.findByText('F-0001')
    await drag('F-0001', columnOf('Aprovadas'))
    expect(await findNotice()).toHaveTextContent('network down')
  })

  it('sem pendencia nao ha drop-blocked; coluna so fica bloqueada durante o arraste', async () => {
    render(<Kanban />)
    await screen.findByText('F-0001')
    const dataTransfer = dt()
    fireEvent.dragStart(cardOf('F-0001'), { dataTransfer })
    fireEvent.dragOver(columnOf('Aprovadas'), { dataTransfer })
    expect(columnOf('Aprovadas')).not.toHaveClass('drop-blocked')
  })

  it('pendencia: dragend limpa o drop-blocked', async () => {
    payload = mkState({ 'F-0001': 1 })
    render(<Kanban />)
    await screen.findByText('F-0001')
    const dataTransfer = dt()
    fireEvent.dragStart(cardOf('F-0001'), { dataTransfer })
    fireEvent.dragOver(columnOf('Aprovadas'), { dataTransfer })
    expect(columnOf('Aprovadas')).toHaveClass('drop-blocked')
    fireEvent.dragEnd(cardOf('F-0001'))
    expect(columnOf('Aprovadas')).not.toHaveClass('drop-blocked')
  })

  it('dragleave para um filho nao limpa o bloqueio', async () => {
    payload = mkState({ 'F-0001': 1 })
    render(<Kanban />)
    await screen.findByText('F-0001')
    const col = columnOf('Aprovadas')
    const dataTransfer = dt()
    fireEvent.dragStart(cardOf('F-0001'), { dataTransfer })
    fireEvent.dragOver(col, { dataTransfer })
    const leave = (to: Element) => {
      const ev = createEvent.dragLeave(col)
      Object.defineProperty(ev, 'relatedTarget', { value: to })
      fireEvent(col, ev)
    }
    leave(col.querySelector('h2')!)
    expect(col).toHaveClass('drop-blocked')
    // F-0415 critério 3: o bloqueio é do card, não do cursor — fica até o dragend.
    leave(document.body)
    expect(col).toHaveClass('drop-blocked')
    fireEvent.dragEnd(cardOf('F-0001'))
    expect(col).not.toHaveClass('drop-blocked')
  })

  it('colunas fechadas e destinos dentro da mesma coluna dao dropEffect none e nao chamam', async () => {
    payload = { ...mkState(), features: [...mkState().features, feature('F-0004', 'rejected')] }
    render(<Kanban />)
    await screen.findByText('F-0001')
    fireEvent.click(screen.getByLabelText(/fechad|encerrad/i))
    for (const [id, target] of [['F-0001', () => screen.getByRole('heading', { name: /Backlog/ }).closest('details')!], ['F-0002', () => columnOf('Prontas')], ['F-0001', () => columnOf('Rejeitadas')]] as const) {
      const dataTransfer = dt()
      fireEvent.dragStart(cardOf(id), { dataTransfer })
      fireEvent.dragOver(target(), { dataTransfer })
      expect(dataTransfer.dropEffect).toBe('none')
      fireEvent.drop(target(), { dataTransfer })
    }
    expect(post).not.toHaveBeenCalled()
  })

  it('aviso some no proximo dragstart', async () => {
    post.mockResolvedValue({ ok: false, error: 'gate: x' })
    render(<Kanban />)
    await screen.findByText('F-0001')
    await drag('F-0001', columnOf('Aprovadas'))
    expect(await findNotice()).toHaveTextContent('gate: x')
    fireEvent.dragStart(cardOf('F-0001'), { dataTransfer: dt() })
    expect(noticeEl()).toBeEmptyDOMElement()
  })

  it('clique em card arrastavel continua navegando', async () => {
    render(<Kanban />)
    await screen.findByText('F-0001')
    expect(fireEvent.click(cardOf('F-0001'))).toBe(true)
    expect(cardOf('F-0001')).toHaveAttribute('href', '#/f/F-0001')
  })
})

describe('destaque de destino (F-0415)', () => {
  const backlog = () => screen.getByRole('heading', { name: /Backlog/ }).closest('details') as HTMLElement
  const start = (id: string) => {
    const dataTransfer = dt()
    fireEvent.dragStart(cardOf(id), { dataTransfer })
    return dataTransfer
  }
  const leave = (col: HTMLElement, to: Element) => {
    const ev = createEvent.dragLeave(col)
    Object.defineProperty(ev, 'relatedTarget', { value: to })
    fireEvent(col, ev)
  }
  const MARKS = '.drop-ready, .drop-active, .drop-blocked, .drop-off, .card-in-flight, .drop-hint'
  afterEach(() => vi.useRealTimers())

  it('critério 1: o destino fica pronto desde o dragstart, com o texto do resultado', async () => {
    render(<Kanban />)
    await screen.findByText('F-0001')
    start('F-0001')
    expect(columnOf('Aprovadas')).toHaveClass('drop-ready')
    expect(columnOf('Aprovadas')).toHaveTextContent('Solte para aprovar')
    expect(columnOf('Conclu')).not.toHaveClass('drop-ready')
    fireEvent.dragEnd(cardOf('F-0001'))
    start('F-0002')
    expect(columnOf('Conclu')).toHaveClass('drop-ready')
    expect(columnOf('Conclu')).toHaveTextContent('Solte para aceitar')
    expect(columnOf('Aprovadas')).not.toHaveClass('drop-ready')
  })

  it('critério 2: cursor sobre o destino o ativa; filho não é saída; sair volta a pronto', async () => {
    render(<Kanban />)
    await screen.findByText('F-0001')
    const col = columnOf('Aprovadas')
    const dataTransfer = start('F-0001')
    fireEvent.dragOver(col, { dataTransfer })
    expect(col).toHaveClass('drop-active')
    expect(col).not.toHaveClass('drop-ready')
    leave(col, col.querySelector('h2')!)
    expect(col).toHaveClass('drop-active')
    leave(col, document.body)
    expect(col).toHaveClass('drop-ready')
    expect(col).not.toHaveClass('drop-active')
  })

  it('critério 3: pendência bloqueia desde o dragstart, com o motivo, até o dragend', async () => {
    payload = mkState({ 'F-0001': 2 })
    render(<Kanban />)
    await screen.findByText('F-0001')
    const col = columnOf('Aprovadas')
    start('F-0001')
    expect(col).toHaveClass('drop-blocked')
    expect(col).not.toHaveClass('drop-ready')
    expect(col).toHaveTextContent('Responda os 2 esclarecimentos antes de aprovar')
    expect(col).not.toHaveTextContent('Solte para aprovar')
    leave(col, document.body)
    expect(col).toHaveClass('drop-blocked')
    fireEvent.dragEnd(cardOf('F-0001'))
    expect(col).not.toHaveClass('drop-blocked')
  })

  it('critério 4: colunas que não aceitam o card esmaecem, menos a de origem', async () => {
    payload = { ...mkState(), features: [...mkState().features, feature('F-0004', 'rejected')] }
    render(<Kanban />)
    await screen.findByText('F-0001')
    fireEvent.click(screen.getByLabelText(/fechad|encerrad/i))
    start('F-0001')
    for (const name of ['Em desenvolvimento', 'Prontas', 'Conclu', 'Rejeitadas', 'Substitu']) {
      expect(columnOf(name), name).toHaveClass('drop-off')
    }
    expect(backlog()).not.toHaveClass('drop-off')
    expect(columnOf('Aprovadas')).not.toHaveClass('drop-off')
    fireEvent.dragEnd(cardOf('F-0001'))
    start('F-0002')
    expect(backlog()).toHaveClass('drop-off')
    expect(columnOf('Aprovadas')).toHaveClass('drop-off')
    expect(columnOf('Prontas')).not.toHaveClass('drop-off')
    expect(columnOf('Conclu')).not.toHaveClass('drop-off')
  })

  it('critério 5: o fantasma só liga depois que o browser capturou a imagem de arraste', async () => {
    render(<Kanban />)
    await screen.findByText('F-0001')
    vi.useFakeTimers()
    start('F-0001')
    expect(cardOf('F-0001')).not.toHaveClass('card-in-flight')
    act(() => void vi.runAllTimers())
    expect(cardOf('F-0001')).toHaveClass('card-in-flight')
    expect(cardOf('F-0002')).not.toHaveClass('card-in-flight')
  })

  it('critério 6: só os cards arrastáveis têm o cursor de agarrar', async () => {
    render(<Kanban />)
    await screen.findByText('F-0001')
    expect(cardOf('F-0001')).toHaveClass('card-draggable')
    expect(cardOf('F-0002')).toHaveClass('card-draggable')
    expect(cardOf('F-0003')).not.toHaveClass('card-draggable')
    expect(readFileSync('src/app.css', 'utf8')).toMatch(/\.card-draggable\s*\{[^}]*cursor:\s*grab/)
  })

  it('critério 7: o fim do gesto (dragend, drop, dragend antes do timer) limpa todas as marcas', async () => {
    post.mockResolvedValue({ ok: true, data: { id: 'F-0001', from: 'candidate', to: 'approved' } })
    render(<Kanban />)
    await screen.findByText('F-0001')
    vi.useFakeTimers()
    const col = columnOf('Aprovadas')
    let dataTransfer = start('F-0001')
    act(() => void vi.runAllTimers())
    fireEvent.dragOver(col, { dataTransfer })
    expect(document.querySelectorAll(MARKS).length).toBeGreaterThan(0)
    fireEvent.dragEnd(cardOf('F-0001'))
    expect(document.querySelectorAll(MARKS)).toHaveLength(0)

    dataTransfer = start('F-0001')
    act(() => void vi.runAllTimers())
    fireEvent.dragOver(col, { dataTransfer })
    fireEvent.drop(col, { dataTransfer })
    expect(document.querySelectorAll(MARKS)).toHaveLength(0)

    start('F-0001')
    fireEvent.dragEnd(cardOf('F-0001'))
    act(() => void vi.runAllTimers())
    expect(document.querySelectorAll(MARKS)).toHaveLength(0)
  })

  it('critério 7: o card em voo muda de estado no meio do gesto (live reload) e as marcas somem', async () => {
    live.version = 0
    const { rerender } = render(<Kanban />)
    await screen.findByText('F-0001')
    start('F-0001')
    expect(document.querySelectorAll(MARKS).length).toBeGreaterThan(0)
    // Outro ator aprovou F-0001: o <a> de origem sai do Backlog e o dragend dele
    // nunca chega ao board.
    payload = { ...mkState(), features: [feature('F-0001', 'approved'), ...mkState().features.slice(1)] }
    live.version = 1
    rerender(<Kanban />)
    await waitFor(() => expect(columnOf('Aprovadas')).toHaveTextContent('F-0001'))
    expect(document.querySelectorAll(MARKS)).toHaveLength(0)
    live.version = 0
  })

  it('critério 8: estados distintos pela forma da borda; transições só sem movimento reduzido', () => {
    const css = readFileSync('src/app.css', 'utf8')
    const media = /@media \(prefers-reduced-motion: no-preference\)\s*\{[\s\S]*?\}\s*\}/g
    const motion = css.match(media) ?? []
    expect(motion.join('\n')).toMatch(/\.column[^{]*\{[^}]*transition/)
    const rest = css.replace(media, '')
    // Todas as regras com exatamente esse seletor, não só a primeira.
    const rule = (sel: string) =>
      [...rest.matchAll(new RegExp(`(?:^|[}\\s])${sel.replace(/\./g, '\\.')}\\s*\\{([^}]*)\\}`, 'g'))].map((m) => m[1]).join('\n')
    expect(rule('.column.drop-ready')).toMatch(/dashed/)
    expect(rule('.column.drop-active')).toMatch(/solid/)
    for (const sel of ['.column', '.column.drop-ready', '.column.drop-active', '.column.drop-off', '.card.card-in-flight']) {
      expect(rule(sel), sel).not.toMatch(/transition/)
    }
  })
})

describe('destino fora da tela (F-0420)', () => {
  type Box = { left: number; width: number }
  // jsdom não faz layout: todo retângulo vem zerado. A spy devolve o do board e
  // o de cada coluna (pelo data-state) e zero para o resto.
  const rect = (b: Box) =>
    ({ left: b.left, right: b.left + b.width, width: b.width, top: 0, bottom: 0, height: 0, x: b.left, y: 0 }) as DOMRect
  let spy: { mockRestore: () => void } | undefined
  const geometry = (board: Box, cols: Partial<Record<Feature['state'], Box>>) => {
    spy?.mockRestore()
    spy = vi.spyOn(Element.prototype, 'getBoundingClientRect').mockImplementation(function (this: Element) {
      if (this.classList.contains('board')) return rect(board)
      const s = this.getAttribute('data-state') as Feature['state'] | null
      return rect((s && cols[s]) || { left: 0, width: 0 })
    })
  }
  afterEach(() => {
    spy?.mockRestore()
    spy = undefined
  })
  const NARROW = { left: 0, width: 800 }
  const OFF = { left: 900, width: 208 }
  const edge = () => document.querySelector('.drop-edge') as HTMLElement | null
  const boardEl = () => document.querySelector('.board') as HTMLElement
  const start = (id: string) => {
    const dataTransfer = dt()
    fireEvent.dragStart(cardOf(id), { dataTransfer })
    return dataTransfer
  }

  it('critério 1: destino fora da vista ganha a faixa na borda desde o dragstart', async () => {
    render(<Kanban />)
    await screen.findByText('F-0002')
    geometry(NARROW, { done: OFF, approved: { left: 230, width: 208 } })
    start('F-0002')
    expect(edge()).not.toBeNull()
    expect(edge()).toHaveClass('drop-ready')
    expect(edge()).toHaveTextContent('Concluídas')
    expect(edge()).toHaveTextContent('→')
    expect(edge()).toHaveTextContent('Solte para aceitar')
    fireEvent.dragEnd(cardOf('F-0002'))
    geometry(NARROW, { approved: OFF })
    start('F-0001')
    expect(edge()).toHaveTextContent('Aprovadas')
    expect(edge()).toHaveTextContent('Solte para aprovar')
  })

  it('critério 2: destino com pelo menos metade visível não ganha faixa', async () => {
    render(<Kanban />)
    await screen.findByText('F-0002')
    geometry(NARROW, { done: { left: 400, width: 208 } })
    start('F-0002')
    expect(edge()).toBeNull()
    fireEvent.dragEnd(cardOf('F-0002'))
    // exatamente metade: 104 de 208 dentro do board
    geometry(NARROW, { done: { left: 696, width: 208 } })
    start('F-0002')
    expect(edge()).toBeNull()
  })

  it('critério 2: offscreenRight decide pela metade da largura', () => {
    const board = rect(NARROW)
    expect(offscreenRight(board, rect({ left: 696, width: 208 }))).toBe(false)
    expect(offscreenRight(board, rect({ left: 697, width: 208 }))).toBe(true)
    expect(offscreenRight(board, rect(OFF))).toBe(true)
    // coluna não medida (jsdom, display:none) nunca conta como fora da vista
    expect(offscreenRight(board, rect({ left: 900, width: 0 }))).toBe(false)
    expect(offscreenRight(rect({ left: 0, width: 0 }), rect({ left: 0, width: 0 }))).toBe(false)
  })

  it('critério 3: rolar o board durante o gesto mede de novo; sem gesto, nada', async () => {
    render(<Kanban />)
    await screen.findByText('F-0002')
    geometry(NARROW, { done: OFF })
    start('F-0002')
    expect(edge()).not.toBeNull()
    geometry(NARROW, { done: { left: 600, width: 208 } })
    fireEvent.scroll(boardEl())
    expect(edge()).toBeNull()
    geometry(NARROW, { done: OFF })
    fireEvent.scroll(boardEl())
    expect(edge()).not.toBeNull()
    fireEvent.dragEnd(cardOf('F-0002'))
    fireEvent.scroll(boardEl())
    expect(edge()).toBeNull()
  })

  it('critério 4: a faixa repete o destino bloqueado, com o motivo', async () => {
    payload = mkState({ 'F-0001': 2 })
    render(<Kanban />)
    await screen.findByText('F-0001')
    geometry(NARROW, { approved: OFF })
    start('F-0001')
    expect(edge()).toHaveClass('drop-blocked')
    expect(edge()).not.toHaveClass('drop-ready')
    expect(edge()).toHaveTextContent('Responda os 2 esclarecimentos antes de aprovar')
    expect(edge()).not.toHaveTextContent('Solte para aprovar')
  })

  it('critério 5: a faixa não é lugar de soltar nem recebe ponteiro', async () => {
    post.mockResolvedValue({ ok: true, data: { id: 'F-0002', from: 'implemented', to: 'done' } })
    render(<Kanban />)
    await screen.findByText('F-0002')
    geometry(NARROW, { done: OFF })
    const dataTransfer = start('F-0002')
    const band = edge()!
    fireEvent.dragOver(band, { dataTransfer })
    expect(document.querySelector('.drop-active')).toBeNull()
    fireEvent.drop(band, { dataTransfer })
    expect(post).not.toHaveBeenCalled()
    expect(readFileSync('src/app.css', 'utf8')).toMatch(/\.drop-edge\s*\{[^}]*pointer-events:\s*none/)
  })

  it('critério 6: a faixa fica fora do fluxo do board e nada rola', async () => {
    render(<Kanban />)
    await screen.findByText('F-0002')
    geometry(NARROW, { done: OFF })
    start('F-0002')
    const band = edge()!
    expect(boardEl().contains(band)).toBe(false)
    expect(band.parentElement).toHaveClass('board-wrap')
    expect(readFileSync('src/app.css', 'utf8')).toMatch(/\.drop-edge\s*\{[^}]*position:\s*absolute/)
    // jsdom nunca muda scrollLeft: a leitura do código é o que prova que nada rola
    expect(readFileSync('src/Kanban.tsx', 'utf8')).not.toMatch(/scrollTo|scrollBy|scrollIntoView|scrollLeft\s*=[^=]/)
  })

  it('critério 7: o fim do gesto tira a faixa (dragend, drop, live reload)', async () => {
    post.mockResolvedValue({ ok: true, data: { id: 'F-0002', from: 'implemented', to: 'done' } })
    live.version = 0
    const { rerender } = render(<Kanban />)
    await screen.findByText('F-0002')
    geometry(NARROW, { done: OFF })
    start('F-0002')
    expect(edge()).not.toBeNull()
    fireEvent.dragEnd(cardOf('F-0002'))
    expect(edge()).toBeNull()

    const dataTransfer = start('F-0002')
    expect(edge()).not.toBeNull()
    fireEvent.dragOver(columnOf('Conclu'), { dataTransfer })
    fireEvent.drop(columnOf('Conclu'), { dataTransfer })
    expect(edge()).toBeNull()
    await waitFor(() => expect(post).toHaveBeenCalled())

    start('F-0002')
    expect(edge()).not.toBeNull()
    // Outro ator aceitou F-0002: o <a> de origem sai de Prontas sem dragend.
    payload = { ...mkState(), features: [feature('F-0001', 'candidate'), feature('F-0002', 'done'), feature('F-0003', 'in_development')] }
    live.version = 1
    rerender(<Kanban />)
    await waitFor(() => expect(columnOf('Conclu')).toHaveTextContent('F-0002'))
    expect(edge()).toBeNull()
    live.version = 0
  })

  it('critério 8: faixa aria-hidden e sem transição', async () => {
    render(<Kanban />)
    await screen.findByText('F-0002')
    geometry(NARROW, { done: OFF })
    start('F-0002')
    expect(edge()).toHaveAttribute('aria-hidden', 'true')
    const rules = [...readFileSync('src/app.css', 'utf8').matchAll(/([^{}]*\.drop-edge[^{}]*)\{([^}]*)\}/g)]
    expect(rules.length).toBeGreaterThan(0)
    for (const [, sel, body] of rules) expect(body, sel.trim()).not.toMatch(/transition/)
  })
})
