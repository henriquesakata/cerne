import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import Channel from './Channel'
import { LiveProvider, useProjects } from './live'
import { fetchProjects, type ChannelMessage, type ChannelSnapshot, type ProjectInfo } from './api'

const readChannel = vi.fn<(follow: boolean, version: number, signal?: AbortSignal) => Promise<ChannelSnapshot>>()
const postChannel = vi.fn<(body: string) => Promise<ChannelMessage>>()

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  readChannel: (follow: boolean, version: number, signal?: AbortSignal) => readChannel(follow, version, signal),
  postChannel: (body: string) => postChannel(body),
  fetchProjects: vi.fn(),
  setApiProject: vi.fn(),
  subscribeLive: vi.fn(() => () => {}),
}))

const msg = (over: Partial<ChannelMessage> = {}): ChannelMessage => ({
  seq: 1,
  type: 'message',
  nick: 'W-0290',
  body: 'vou mexer em internal/server/rooms.go',
  time: '2026-11-05T10:00:00Z',
  state: 'not_delivered',
  ...over,
})

// A long-poll that only settles when the test says so.
function parkedFollow() {
  let release: (snap: ChannelSnapshot) => void = () => {}
  readChannel.mockImplementation((follow) =>
    follow ? new Promise<ChannelSnapshot>((resolve) => (release = resolve)) : Promise.resolve(snapshot),
  )
  return (snap: ChannelSnapshot) => {
    snapshot = snap
    release(snap)
  }
}

let snapshot: ChannelSnapshot = { messages: [], version: 0 }

// A target line names the target in its own span (F-0489: ids in mono), so it
// is matched as the whole <li> text.
const targetLine = (text: string) => (_: string, el: Element | null) =>
  el?.tagName === 'LI' && el.textContent?.trim() === text

const SHORT: Intl.DateTimeFormatOptions = { day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit' }

beforeEach(() => {
  cleanup()
  readChannel.mockReset()
  postChannel.mockReset()
  vi.mocked(fetchProjects).mockReset()
  snapshot = { messages: [], version: 0 }
  readChannel.mockImplementation((follow) => (follow ? new Promise(() => {}) : Promise.resolve(snapshot)))
})

describe('Channel (F-0325)', () => {
  it('mensagem da página de um card aparece marcada com o card (F-0486 critério 12)', async () => {
    snapshot = { messages: [msg({ human: true, nick: 'Henrique', body: 'corrige?', ref: 'F-0486', directed: true })], version: 2 }
    render(<Channel />)
    const link = await screen.findByRole('link', { name: 'F-0486' })
    expect(link).toHaveAttribute('href', '#/f/F-0486')
  })

  it('abre transcrição e compositor, sem os painéis de objetivo e ata (critério 21)', async () => {
    snapshot = { messages: [msg()], version: 10 }
    render(<Channel />)
    await waitFor(() => expect(screen.getByText(/vou mexer em internal\/server/)).toBeInTheDocument())
    expect(screen.getByRole('heading', { name: /canal do projeto/i })).toBeInTheDocument()
    expect(screen.getByPlaceholderText(/mensagem para os agentes/i)).toBeInTheDocument()
    expect(screen.queryByText(/encerrar sala/i)).toBeNull()
    expect(screen.queryByText(/^ata$/i)).toBeNull()
    expect(screen.queryByText(/entrar na sala/i)).toBeNull()
    expect(screen.queryByText(/facilitador/i)).toBeNull()
    expect(screen.getByText('não entregue')).toBeInTheDocument()
  })

  it('o envio publica no canal e a mensagem aparece como humana (critério 22)', async () => {
    snapshot = { messages: [], version: 1 }
    const sent = msg({ seq: 2, nick: 'Henrique', human: true, body: 'segura o deploy' })
    postChannel.mockImplementation(async () => {
      snapshot = { messages: [sent], version: 2 }
      return sent
    })
    render(<Channel />)
    await waitFor(() => expect(screen.getByText(/canal vazio/i)).toBeInTheDocument())
    fireEvent.change(screen.getByPlaceholderText(/mensagem para os agentes/i), { target: { value: 'segura o deploy' } })
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))
    await waitFor(() => expect(postChannel).toHaveBeenCalledWith('segura o deploy'))
    await waitFor(() => expect(screen.getByText('segura o deploy')).toBeInTheDocument())
    expect(screen.getByText(/Henrique \(humano\)/)).toBeInTheDocument()
    expect((screen.getByPlaceholderText(/mensagem para os agentes/i) as HTMLTextAreaElement).value).toBe('')
  })

  it('dirigida entregue mostra o processamento e passa a respondida pelo long-poll (critérios 23 e 24)', async () => {
    snapshot = {
      messages: [msg({ directed: true, state: 'delivered', targets: [{ work: 'W-0291', state: 'delivered' }] })],
      version: 5,
    }
    const advance = parkedFollow()
    render(<Channel />)
    await waitFor(() => expect(screen.getByText(targetLine('W-0291 recebeu — sem resposta'))).toBeInTheDocument())
    await waitFor(() => expect(readChannel).toHaveBeenCalledWith(true, 5, expect.anything()))

    await act(async () =>
      advance({
        messages: [
          msg({ directed: true, state: 'answered', targets: [{ work: 'W-0291', state: 'answered' }] }),
          msg({ seq: 2, nick: 'W-0291', body: 'ok, seguro' }),
        ],
        version: 9,
      }),
    )
    await waitFor(() => expect(screen.getByText(targetLine('W-0291 respondeu'))).toBeInTheDocument())
    expect(screen.queryByText(targetLine('W-0291 recebeu — sem resposta'))).toBeNull()
    expect(screen.getByText('ok, seguro')).toBeInTheDocument()
  })

  it('reconhecimento e work encerrado têm rótulos próprios (crítico C17)', async () => {
    snapshot = {
      messages: [
        msg({ directed: true, state: 'answered', targets: [{ work: 'W-0291', state: 'answered', acked: true }] }),
        msg({ seq: 2, directed: true, state: 'not_delivered', targets: [{ work: 'W-0004', state: 'not_delivered', closed: true }] }),
      ],
      version: 2,
    }
    render(<Channel />)
    await waitFor(() => expect(screen.getByText(targetLine('W-0291 reconheceu'))).toBeInTheDocument())
    expect(screen.queryByText(targetLine('W-0291 respondeu'))).toBeNull()
    expect(screen.getByText(targetLine('W-0004 encerrado — não vai receber'))).toBeInTheDocument()
  })

  it('não entregue diz por quê — a sessão viva só recebe num ponto de contato (B-0054)', async () => {
    snapshot = {
      messages: [
        msg({ nick: 'Henrique', human: true, body: 'alguém aí?', directed: true, targets: [{ work: 'W-0291', state: 'not_delivered' }] }),
        msg({ seq: 2, directed: true, targets: [{ work: 'W-0004', state: 'not_delivered', closed: true }] }),
        msg({ seq: 3, body: 'já chegou', state: 'delivered' }),
      ],
      version: 4,
    }
    render(<Channel />)
    await waitFor(() => expect(screen.getByText('alguém aí?')).toBeInTheDocument())
    // Only the message a live session can still receive: a closed work never will.
    expect(screen.getAllByText(/só recebe ao passar por um ponto de contato/i)).toHaveLength(1)
    expect(screen.getByText(/próximo prompt, comando, edição ou início de sessão do agente/i)).toBeInTheDocument()
  })

  it('a versão ao vivo (F-0031) também refaz a carga — some a mensagem de um work fechado (critério 24)', async () => {
    const projects: ProjectInfo[] = [
      { pid: 'p1', name: 'P1' },
      { pid: 'p2', name: 'P2' },
    ]
    vi.mocked(fetchProjects).mockResolvedValue({ projects, default: 'p1' })
    function Switcher() {
      const { setActivePid } = useProjects()
      return (
        <button type="button" onClick={() => setActivePid('p2')}>
          trocar projeto
        </button>
      )
    }
    snapshot = { messages: [msg({ body: 'de um work que vai fechar' })], version: 3 }
    render(
      <LiveProvider>
        <Switcher />
        <Channel />
      </LiveProvider>,
    )
    await waitFor(() => expect(screen.getByText('de um work que vai fechar')).toBeInTheDocument())
    snapshot = { messages: [], version: 3 }
    fireEvent.click(screen.getByRole('button', { name: /trocar projeto/i }))
    await waitFor(() => expect(screen.queryByText('de um work que vai fechar')).toBeNull())
    expect(screen.getByText(/canal vazio/i)).toBeInTheDocument()
  })

  it('canal vazio mostra estado vazio explícito, sem erro (critério 25)', async () => {
    render(<Channel />)
    await waitFor(() => expect(screen.getByText(/canal vazio/i)).toBeInTheDocument())
    expect(screen.queryByRole('status')).toBeNull()
  })

  it('falha de carga mantém o cabeçalho e oferece nova tentativa (critério 25)', async () => {
    readChannel.mockImplementation((follow) =>
      follow ? new Promise(() => {}) : Promise.reject(new Error('offline')),
    )
    render(<Channel />)
    await waitFor(() => expect(screen.getByText(/erro ao carregar o canal: offline/i)).toBeInTheDocument())
    expect(screen.getByRole('heading', { name: /canal do projeto/i })).toBeInTheDocument()
    readChannel.mockImplementation((follow) => (follow ? new Promise(() => {}) : Promise.resolve(snapshot)))
    fireEvent.click(screen.getByRole('button', { name: /tentar de novo/i }))
    await waitFor(() => expect(screen.getByText(/canal vazio/i)).toBeInTheDocument())
  })

  it('cada mensagem mostra data e hora de envio, com o instante completo no title (F-0489 crit. 5)', async () => {
    const iso = '2026-11-21T14:43:37Z'
    snapshot = { messages: [msg({ time: iso })], version: 1 }
    const { container } = render(<Channel />)
    await waitFor(() => expect(screen.getByText(/vou mexer em internal\/server/)).toBeInTheDocument())
    const stamp = container.querySelector(`time[datetime="${iso}"]`)
    expect(stamp).not.toBeNull()
    expect(stamp).toHaveTextContent(new Date(iso).toLocaleString('pt-BR', SHORT))
    expect(stamp).toHaveAttribute('title', new Date(iso).toLocaleString('pt-BR'))
  })

  it('destinatário que respondeu mostra quando (F-0489 crit. 7)', async () => {
    const at = '2026-11-21T14:50:00Z'
    snapshot = {
      messages: [msg({ directed: true, state: 'answered', targets: [{ work: 'W-0291', state: 'answered', answered_at: at }] })],
      version: 1,
    }
    const { container } = render(<Channel />)
    await waitFor(() => expect(container.querySelector(`li time[datetime="${at}"]`)).not.toBeNull())
    const line = container.querySelector(`li time[datetime="${at}"]`)!.closest('li') as HTMLElement
    expect(line).toHaveTextContent(`W-0291 respondeu ${new Date(at).toLocaleString('pt-BR', SHORT)}`)
  })

  it('mensagem do humano é o balão próprio; autor e destinatário id saem em mono (F-0489 crit. 4)', async () => {
    snapshot = {
      messages: [
        msg({ seq: 1, nick: 'Henrique', human: true, body: 'alguém aí?', directed: true, targets: [{ work: 'W-0291', state: 'delivered' }] }),
        msg({ seq: 2, nick: 'B-0063·d17c7558', body: 'aqui', state: 'delivered' }),
      ],
      version: 2,
    }
    render(<Channel />)
    const own = await screen.findByText('alguém aí?')
    expect(own.closest('.room-msg')).toHaveClass('room-msg-own')
    expect(screen.getByText(/Henrique \(humano\)/)).toHaveClass('sr-only')
    expect(screen.getByText('aqui').closest('.room-msg')).not.toHaveClass('room-msg-own')
    expect(screen.getByText('B-0063·d17c7558')).toHaveClass('channel-id')
    expect(screen.getByText('W-0291')).toHaveClass('channel-id')
  })

  it('Enter envia, Shift+Enter e composição de IME não (F-0489 crit. 3)', async () => {
    snapshot = { messages: [], version: 1 }
    postChannel.mockResolvedValue(msg({ seq: 2, human: true, nick: 'Henrique', body: 'oi' }))
    render(<Channel />)
    await waitFor(() => expect(screen.getByText(/canal vazio/i)).toBeInTheDocument())
    const box = screen.getByPlaceholderText(/mensagem para os agentes/i)
    fireEvent.change(box, { target: { value: 'oi' } })
    fireEvent.keyDown(box, { key: 'Enter', shiftKey: true })
    fireEvent.keyDown(box, { key: 'Enter', isComposing: true })
    expect(postChannel).not.toHaveBeenCalled()
    fireEvent.keyDown(box, { key: 'Enter' })
    await waitFor(() => expect(postChannel).toHaveBeenCalledWith('oi'))
    expect(screen.getByRole('button', { name: /enviar/i }).closest('.backlog-chat-composer')).not.toBeNull()
    expect(screen.getByText('Enter envia · Shift+Enter quebra linha')).toHaveClass('room-composer-hint')
  })
})

// F-0489 crit. 2: the channel's transcript opens at the end and follows new
// messages while the reader is there; scrolling up turns that off and only
// scrolling back to the end, or sending, turns it on again. jsdom does no
// layout, so the metrics are stubbed on the prototype, as in the War Room's
// glue-scroll tests (F-0292).
describe('auto-scroll do canal (F-0489)', () => {
  const CLIENT_HEIGHT = 400
  const SCROLL_HEIGHT = 1000
  const AT_BOTTOM = SCROLL_HEIGHT - CLIENT_HEIGHT
  let restore: (() => void) | null = null

  beforeEach(() => {
    const original = {
      scrollHeight: Object.getOwnPropertyDescriptor(Element.prototype, 'scrollHeight'),
      clientHeight: Object.getOwnPropertyDescriptor(Element.prototype, 'clientHeight'),
      scrollTop: Object.getOwnPropertyDescriptor(Element.prototype, 'scrollTop'),
    }
    Object.defineProperty(Element.prototype, 'scrollHeight', { configurable: true, get: () => SCROLL_HEIGHT })
    Object.defineProperty(Element.prototype, 'clientHeight', { configurable: true, get: () => CLIENT_HEIGHT })
    Object.defineProperty(Element.prototype, 'scrollTop', { configurable: true, writable: true, value: 0 })
    restore = () => {
      for (const [key, desc] of Object.entries(original)) {
        if (desc) Object.defineProperty(Element.prototype, key, desc)
      }
    }
  })

  afterEach(() => {
    restore?.()
    restore = null
  })

  const history = () => document.querySelector('.room-transcript') as HTMLDivElement

  it('abre no fim, segue a mensagem nova, desliga ao subir e religa só no fim ou ao enviar', async () => {
    snapshot = { messages: [msg({ seq: 1, body: 'primeira' })], version: 1 }
    const advance = parkedFollow()
    render(<Channel />)
    await waitFor(() => expect(screen.getByText('primeira')).toBeInTheDocument())
    expect(history().scrollTop).toBe(SCROLL_HEIGHT)

    // at the end: a new message keeps the end in view
    history().scrollTop = AT_BOTTOM
    fireEvent.scroll(history())
    await act(async () => advance({ messages: [msg({ seq: 1, body: 'primeira' }), msg({ seq: 2, body: 'segunda' })], version: 2 }))
    await waitFor(() => expect(screen.getByText('segunda')).toBeInTheDocument())
    expect(history().scrollTop).toBe(SCROLL_HEIGHT)

    // scrolled up: off, a new message does not move the view
    history().scrollTop = AT_BOTTOM - 100
    fireEvent.scroll(history())
    await act(async () =>
      advance({ messages: [msg({ seq: 1, body: 'primeira' }), msg({ seq: 2, body: 'segunda' }), msg({ seq: 3, body: 'terceira' })], version: 3 }),
    )
    await waitFor(() => expect(screen.getByText('terceira')).toBeInTheDocument())
    expect(history().scrollTop).toBe(AT_BOTTOM - 100)

    // back to the end: on again
    history().scrollTop = AT_BOTTOM
    fireEvent.scroll(history())
    await act(async () =>
      advance({
        messages: [msg({ seq: 1, body: 'primeira' }), msg({ seq: 2, body: 'segunda' }), msg({ seq: 3, body: 'terceira' }), msg({ seq: 4, body: 'quarta' })],
        version: 4,
      }),
    )
    await waitFor(() => expect(screen.getByText('quarta')).toBeInTheDocument())
    expect(history().scrollTop).toBe(SCROLL_HEIGHT)

    // scrolled up and sending: the view goes to the end (clarified at the approve)
    history().scrollTop = AT_BOTTOM - 100
    fireEvent.scroll(history())
    const mine = msg({ seq: 5, human: true, nick: 'Henrique', body: 'minha' })
    postChannel.mockImplementation(async () => {
      snapshot = { messages: [...snapshot.messages, mine], version: 5 }
      return mine
    })
    fireEvent.change(screen.getByPlaceholderText(/mensagem para os agentes/i), { target: { value: 'minha' } })
    fireEvent.keyDown(screen.getByPlaceholderText(/mensagem para os agentes/i), { key: 'Enter' })
    await waitFor(() => expect(screen.getByText('minha')).toBeInTheDocument())
    expect(history().scrollTop).toBe(SCROLL_HEIGHT)
  })
})
