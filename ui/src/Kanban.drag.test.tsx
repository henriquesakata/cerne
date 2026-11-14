import { readFileSync } from 'node:fs'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, cleanup, createEvent, fireEvent, render, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import Kanban from './Kanban'
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
vi.mock('./live', () => ({ useLiveVersion: () => 0 }))

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

  it('critério 8: estados distintos pela forma da borda; transições só sem movimento reduzido', () => {
    const css = readFileSync('src/app.css', 'utf8')
    const media = /@media \(prefers-reduced-motion: no-preference\)\s*\{[\s\S]*?\}\s*\}/g
    const motion = css.match(media) ?? []
    expect(motion.join('\n')).toMatch(/\.column[^{]*\{[^}]*transition/)
    const rest = css.replace(media, '')
    const rule = (sel: string) => rest.match(new RegExp(`${sel.replace(/\./g, '\\.')}\\s*\\{([^}]*)\\}`))?.[1] ?? ''
    expect(rule('.column.drop-ready')).toMatch(/dashed/)
    expect(rule('.column.drop-active')).toMatch(/solid/)
    for (const sel of ['.column', '.column.drop-ready', '.column.drop-active', '.column.drop-off', '.card.card-in-flight']) {
      expect(rule(sel), sel).not.toMatch(/transition/)
    }
  })
})
