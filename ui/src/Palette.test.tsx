import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import Palette, { entriesFrom, rank, type Entry } from './Palette'
import type { StateResponse } from './api'

const ts = { created_at: '2026-07-25T00:00:00Z', updated_at: '2026-07-25T00:00:00Z' }

const state: StateResponse = {
  project: { name: 'Projeto Demo', modules: [] },
  features: [
    { id: 'F-0195', title: 'Badge de esclarecimento', module: '', state: 'in_development', ...ts },
    { id: 'F-0042', title: 'Decisão sobre cache', module: '', state: 'candidate', ...ts },
  ],
  decisions: [{ id: 'D-0002', title: 'Eixos de classificação', state: 'active', ...ts }],
  references: [{ id: 'R-0007', title: 'Artigo externo', state: 'current', ...ts }],
  runbooks: [{ id: 'RB-0001', title: 'Deploy do hub', state: 'active', ...ts }],
  notes: [{ id: 'N-0018', title: 'TDD estrito', state: 'active', ...ts }],
}

const ids = (es: Entry[]) => es.map((e) => e.id)

describe('entriesFrom (F-0196 C10)', () => {
  it('indexa as cinco espécies com o href da rota de detalhe de cada uma', () => {
    const byId = new Map(entriesFrom(state).map((e) => [e.id, e]))
    expect(byId.get('F-0195')?.href).toBe('#/f/F-0195')
    expect(byId.get('D-0002')?.href).toBe('#/d/D-0002')
    expect(byId.get('R-0007')?.href).toBe('#/r/R-0007')
    expect(byId.get('RB-0001')?.href).toBe('#/rb/RB-0001')
    expect(byId.get('N-0018')?.href).toBe('#/n/N-0018')
    expect(byId.get('N-0018')?.kind).toBe('note')
  })
})

describe('rank (F-0196 C4, C5, C11)', () => {
  const all = entriesFrom(state)

  // C4: título casa sem caixa e sem acento — "decisao" acha "Decisão".
  it('casa por título ignorando caixa e diacrítico', () => {
    expect(ids(rank(all, 'decisao'))).toContain('F-0042')
    expect(ids(rank(all, 'BADGE'))).toContain('F-0195')
  })

  // C5: o id alcança a entidade com ou sem prefixo, e ganha do título.
  it('alcança a entidade por 195, f-0195 e F-0195', () => {
    for (const q of ['195', 'f-0195', 'F-0195']) {
      expect(rank(all, q)[0]?.id).toBe('F-0195')
    }
  })

  it('ranqueia prefixo de id acima de match por título', () => {
    // 'D-0002' casa por id; 'Decisão sobre cache' (F-0042) casa por título.
    const got = rank(all, 'd-000')
    expect(got[0]?.id).toBe('D-0002')
    // E o termo só de zeros não pode virar curinga: normalizar "000" para ""
    // faria startsWith('') casar com o backend inteiro.
    expect(ids(got)).toEqual(['D-0002'])
  })

  // C11: teto duro para o teclado continuar navegável.
  it('nunca devolve mais de 20 entradas', () => {
    const many: Entry[] = Array.from({ length: 50 }, (_, i) => ({
      id: `F-${String(i).padStart(4, '0')}`,
      title: 'assunto comum',
      kind: 'feature',
      href: '#/f/x',
    }))
    expect(rank(many, 'assunto')).toHaveLength(20)
  })

  it('devolve vazio quando nada casa', () => {
    expect(rank(all, 'zzzznadaaqui')).toHaveLength(0)
  })
})

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  fetchState: vi.fn(async () => state),
}))

// open dispara o atalho primário e espera a palette montar com os dados.
async function open() {
  fireEvent.keyDown(document, { key: 'k', metaKey: true })
  return waitFor(() => screen.getByRole('combobox'))
}

const rowIDs = () =>
  Array.from(document.querySelectorAll('.palette-row')).map((r) =>
    r.querySelector('.palette-row-id')?.textContent,
  )

const selected = () =>
  document
    .querySelector('.palette-row[aria-selected="true"]')
    ?.querySelector('.palette-row-id')?.textContent

describe('Palette — overlay (F-0196)', () => {
  beforeEach(() => {
    window.location.hash = ''
  })
  afterEach(cleanup)

  // C1: atalho primário, input focado.
  it('abre com Cmd+K e com Ctrl+K, com o campo focado', async () => {
    render(<Palette />)
    const input = await open()
    expect(document.activeElement).toBe(input)

    fireEvent.keyDown(document, { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('combobox')).toBeNull())

    fireEvent.keyDown(document, { key: 'k', ctrlKey: true })
    await waitFor(() => expect(screen.getByRole('combobox')).toBeInTheDocument())
  })

  // C1 (parte 2): reabrir seleciona o termo anterior — digitar substitui.
  it('seleciona o texto anterior ao reabrir', async () => {
    render(<Palette />)
    const input = (await open()) as HTMLInputElement
    fireEvent.change(input, { target: { value: 'badge' } })
    fireEvent.keyDown(document, { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('combobox')).toBeNull())

    const again = (await open()) as HTMLInputElement
    expect(again.value).toBe('badge')
    expect([again.selectionStart, again.selectionEnd]).toEqual([0, 5])
  })

  // C2: 's' fora de campo editável abre.
  it('abre com s quando o foco não está num campo', async () => {
    render(<Palette />)
    fireEvent.keyDown(document.body, { key: 's' })
    await waitFor(() => expect(screen.getByRole('combobox')).toBeInTheDocument())
  })

  // C3: 's' dentro de campo editável é texto, não atalho.
  it('não abre com s dentro de input, textarea ou contenteditable', async () => {
    render(<Palette />)
    const field = document.createElement('input')
    const area = document.createElement('textarea')
    const rich = document.createElement('div')
    // setAttribute (e não a propriedade): o jsdom não implementa
    // contentEditable, e o atributo é o que existe no markup real.
    rich.setAttribute('contenteditable', 'true')
    document.body.append(field, area, rich)

    for (const el of [field, area, rich]) {
      const ev = new KeyboardEvent('keydown', { key: 's', bubbles: true, cancelable: true })
      el.dispatchEvent(ev)
      expect(ev.defaultPrevented).toBe(false)
    }
    expect(screen.queryByRole('combobox')).toBeNull()
    field.remove()
    area.remove()
    rich.remove()
  })

  // C6: setas com wrap e Enter navegando.
  it('move a seleção com as setas (com wrap) e navega com Enter', async () => {
    render(<Palette />)
    const input = await open()
    fireEvent.change(input, { target: { value: 'f-0' } })
    await waitFor(() => expect(rowIDs()).toEqual(['F-0042', 'F-0195']))

    expect(selected()).toBe('F-0042')
    fireEvent.keyDown(input, { key: 'ArrowDown' })
    expect(selected()).toBe('F-0195')
    fireEvent.keyDown(input, { key: 'ArrowDown' }) // wrap: última → primeira
    expect(selected()).toBe('F-0042')
    fireEvent.keyDown(input, { key: 'ArrowUp' }) // wrap: primeira → última
    expect(selected()).toBe('F-0195')

    fireEvent.keyDown(input, { key: 'Enter' })
    expect(window.location.hash).toBe('#/f/F-0195')
    await waitFor(() => expect(screen.queryByRole('combobox')).toBeNull())
  })

  // C7: Esc e clique fora fecham sem navegar.
  it('fecha com Escape e com clique fora, sem navegar', async () => {
    render(<Palette />)
    await open()
    fireEvent.keyDown(document, { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('combobox')).toBeNull())
    expect(window.location.hash).toBe('')

    await open()
    fireEvent.mouseDown(document.body)
    await waitFor(() => expect(screen.queryByRole('combobox')).toBeNull())
    expect(window.location.hash).toBe('')
  })

  // C8: sem match local, a saída é o FTS5 com o termo já aplicado.
  it('oferece a busca no conteúdo quando nada casa localmente', async () => {
    render(<Palette />)
    const input = await open()
    fireEvent.change(input, { target: { value: 'zzzznadaaqui' } })

    await waitFor(() => expect(document.querySelector('.palette-fallback')).toBeInTheDocument())
    fireEvent.keyDown(input, { key: 'Enter' })
    expect(window.location.hash).toBe('#/search/zzzznadaaqui')
  })
})
