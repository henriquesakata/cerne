import { readFileSync } from 'node:fs'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import WarRoom, { WarRoomDetail } from './WarRoom'
import { I18nProvider } from './i18n'
import { LiveProvider, useProjects } from './live'
import { fetchProjects, HTTPError, type FacilitationItem, type Minutes, type ProjectInfo, type Room, type RoomMessage, type RoomSession } from './api'

const fetchRooms = vi.fn<() => Promise<Room[]>>()
const joinRoom = vi.fn<(id: string, nick: string, bio: string) => Promise<{ room: Room; session: RoomSession }>>()
const sayRoom = vi.fn<(id: string, sessionID: string, body: string) => Promise<RoomMessage>>()
const resumeRoomSession = vi.fn<(id: string, sessionID: string) => Promise<RoomSession>>()
const leaveRoom = vi.fn<(id: string, sessionID: string) => Promise<Room>>()
const readRoomMessages = vi.fn<
  (id: string, since: number, follow: boolean, signal?: AbortSignal) => Promise<RoomMessage[]>
>()
const postRoomOpen = vi.fn<(topic: string) => Promise<{ room: Room; facilitator_id: string }>>()
const postRoomClose = vi.fn<(id: string, facilitatorId: string, minutes: Minutes, allow?: boolean) => Promise<Room>>()
const postRoomFacilitatorHandoff = vi.fn<(id: string, facilitatorId: string) => Promise<string>>()
const fetchRoomPending = vi.fn<(id: string, facilitatorId: string) => Promise<FacilitationItem[]>>()
const postRoomResolveItem =
  vi.fn<(id: string, facilitatorId: string, itemId: string, result: string) => Promise<FacilitationItem>>()

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  fetchRooms: () => fetchRooms(),
  joinRoom: (id: string, nick: string, bio: string) => joinRoom(id, nick, bio),
  sayRoom: (id: string, sessionID: string, body: string) => sayRoom(id, sessionID, body),
  resumeRoomSession: (id: string, sessionID: string) => resumeRoomSession(id, sessionID),
  leaveRoom: (id: string, sessionID: string) => leaveRoom(id, sessionID),
  readRoomMessages: (id: string, since: number, follow: boolean, signal?: AbortSignal) =>
    readRoomMessages(id, since, follow, signal),
  postRoomOpen: (topic: string) => postRoomOpen(topic),
  postRoomClose: (id: string, facilitatorId: string, minutes: Minutes, allow?: boolean) =>
    postRoomClose(id, facilitatorId, minutes, allow),
  postRoomFacilitatorHandoff: (id: string, facilitatorId: string) => postRoomFacilitatorHandoff(id, facilitatorId),
  fetchRoomPending: (id: string, facilitatorId: string) => fetchRoomPending(id, facilitatorId),
  postRoomResolveItem: (id: string, facilitatorId: string, itemId: string, result: string) =>
    postRoomResolveItem(id, facilitatorId, itemId, result),
  fetchProjects: vi.fn(),
  setApiProject: vi.fn(),
  subscribeLive: vi.fn(() => () => {}),
}))

const room = (over: Partial<Room> = {}): Room => ({
  id: 'R1',
  topic: 'Como resolver a fila de deliberação',
  opened_at: '2026-08-06T00:00:00Z',
  participants: [],
  ...over,
})

const session = (over: Partial<RoomSession> = {}): RoomSession => ({
  session_id: 'cafebabecafebabecafebabecafebabe',
  room_id: 'R1',
  nick: 'ana',
  joined_at: '2026-08-06T00:00:01Z',
  last_seq: 2,
  ...over,
})

// chave usada pelo componente (pid vazio nos testes)
const SESSION_KEY = 'warroom-session--R1'

beforeEach(() => {
  cleanup()
  fetchRooms.mockReset()
  joinRoom.mockReset()
  sayRoom.mockReset()
  resumeRoomSession.mockReset()
  leaveRoom.mockReset()
  readRoomMessages.mockReset()
  postRoomOpen.mockReset()
  postRoomClose.mockReset()
  postRoomFacilitatorHandoff.mockReset()
  fetchRoomPending.mockReset()
  postRoomResolveItem.mockReset()
  localStorage.clear()
  window.location.hash = ''
  // toda chamada com follow=true trava por padrão até o teste decidir
  // (evita um loop infinito síncrono comendo o event loop do teste)
  readRoomMessages.mockImplementation((_id, _since, follow) =>
    follow ? new Promise(() => {}) : Promise.resolve([]),
  )
  fetchRoomPending.mockResolvedValue([])
  // WarRoomDetail busca fetchRooms() ao fechar (F-0238, vínculo minutes_note)
  // — default vazio para não quebrar testes que não se importam com o link.
  fetchRooms.mockResolvedValue([])
  vi.mocked(fetchProjects).mockReset()
})

describe('WarRoom (T-0003)', () => {
  // F-0511 crit. 5: the card counts the active participants, not the history.
  it('lista salas abertas e fechadas', async () => {
    fetchRooms.mockResolvedValue([
      room({
        id: 'R1',
        topic: 'Fila serial',
        participants: [
          { nick: 'ana', joined_at: '2026-08-06T00:00:00Z' },
          { nick: 'bia', joined_at: '2026-08-06T00:00:01Z' },
        ],
        active_participants: [{ nick: 'ana', joined_at: '2026-08-06T00:00:00Z' }],
      }),
      room({ id: 'R2', topic: 'Já resolvido', closed_at: '2026-08-06T01:00:00Z' }),
    ])
    render(<WarRoom />)

    await waitFor(() => expect(screen.getByText('Fila serial')).toBeInTheDocument())
    expect(screen.getByText('Já resolvido')).toBeInTheDocument()
    expect(screen.getAllByRole('link')).toHaveLength(2)
    // B-0050: the header counts the rooms; each card counts its participants.
    expect(screen.getByText('2 sala(s)')).toBeInTheDocument()
    expect(screen.getByText('1 participante(s)')).toBeInTheDocument()
  })

  it('fixa o canal permanente no topo, com selo, fora da contagem e do vazio (F-0325 critério 21)', async () => {
    fetchRooms.mockResolvedValue([
      room({ id: 'channel', topic: '', permanent: true, participants: [{ nick: 'W-0290', joined_at: '' }] }),
    ])
    render(<WarRoom />)
    await waitFor(() => expect(screen.getByText(/canal do projeto/i)).toBeInTheDocument())
    const links = screen.getAllByRole('link')
    expect(links).toHaveLength(1)
    expect(links[0]).toHaveAttribute('href', '#/rooms/channel')
    expect(links[0]).toHaveTextContent(/permanente/i)
    expect(screen.getByText(/nenhuma sala aberta/i)).toBeInTheDocument()
    // B-0050: the header counts rooms, so it says rooms, not participants.
    expect(screen.getByText('0 sala(s)')).toBeInTheDocument()
  })

  it('canal ilegível aparece com o erro e as salas formais continuam (crítico C8)', async () => {
    fetchRooms.mockResolvedValue([
      room({ id: 'channel', topic: '', permanent: true, error: 'permission denied' }),
      room({ id: 'R1', topic: 'Fila serial' }),
    ])
    render(<WarRoom />)
    await waitFor(() => expect(screen.getByText('Fila serial')).toBeInTheDocument())
    expect(screen.getByText(/canal indisponível: permission denied/i)).toBeInTheDocument()
  })

  it('o canal vem antes das salas formais', async () => {
    fetchRooms.mockResolvedValue([
      room({ id: 'channel', topic: '', permanent: true }),
      room({ id: 'R1', topic: 'Fila serial' }),
    ])
    render(<WarRoom />)
    await waitFor(() => expect(screen.getByText('Fila serial')).toBeInTheDocument())
    const links = screen.getAllByRole('link')
    expect(links.map((l) => l.getAttribute('href'))).toEqual(['#/rooms/channel', '#/rooms/R1'])
  })

  it('mostra estado vazio quando não há salas', async () => {
    fetchRooms.mockResolvedValue([])
    render(<WarRoom />)
    await waitFor(() => expect(screen.getByText(/nenhuma sala aberta/i)).toBeInTheDocument())
  })

  it('mostra erro de carregamento', async () => {
    fetchRooms.mockRejectedValue(new Error('offline'))
    render(<WarRoom />)
    await waitFor(() => expect(screen.getByText(/erro ao carregar as salas/i)).toBeInTheDocument())
  })

  it('abre sala e navega para ela (critério 1)', async () => {
    fetchRooms.mockResolvedValue([])
    postRoomOpen.mockResolvedValue({
      room: room({ id: 'R9', topic: 'Novo tópico' }),
      facilitator_id: 'facfacfacfacfacfacfacfacfacfacfa',
    })
    render(<WarRoom />)
    await waitFor(() => expect(screen.getByText(/nenhuma sala aberta/i)).toBeInTheDocument())

    fireEvent.change(screen.getByPlaceholderText(/tópico da sala/i), {
      target: { value: 'Novo tópico' },
    })
    fireEvent.click(screen.getByRole('button', { name: /abrir sala/i }))

    await waitFor(() => expect(postRoomOpen).toHaveBeenCalledWith('Novo tópico'))
    await waitFor(() => expect(window.location.hash).toBe('#/rooms/R9'))
    expect(localStorage.getItem('warroom-facilitator--R9')).toBe('facfacfacfacfacfacfacfacfacfacfa')
  })
})

describe('WarRoomDetail (T-0003)', () => {
  it('carrega roster e histórico do transcript', async () => {
    readRoomMessages.mockImplementationOnce(() =>
      Promise.resolve([
        { seq: 1, type: 'open', topic: 'Fila serial', time: '2026-08-06T00:00:00Z' },
        { seq: 2, type: 'join', nick: 'ana', bio: 'pessimista', time: '2026-08-06T00:00:01Z' },
        { seq: 3, type: 'message', nick: 'ana', body: 'oi pessoal', time: '2026-08-06T00:00:02Z' },
      ]),
    )
    // F-0511 crit. 5: the panel lists the active participants the server reports.
    const ana = { nick: 'ana', bio: 'pessimista', joined_at: '2026-08-06T00:00:01Z' }
    fetchRooms.mockResolvedValue([room({ participants: [ana], active_participants: [ana] })])
    render(<WarRoomDetail id="R1" />)

    await waitFor(() => expect(screen.getByText(/Fila serial/)).toBeInTheDocument())
    const roster = screen.getByRole('region', { name: /participantes/i })
    await waitFor(() => expect(within(roster).getByText('ana')).toBeInTheDocument())
    expect(within(roster).getByText('pessimista', { exact: false })).toBeInTheDocument()
    expect(within(screen.getByRole('log')).getByText('ana')).toBeInTheDocument()
    expect(screen.getByText(/oi pessoal/)).toBeInTheDocument()
  })

  it('exige entrar na sala antes de liberar o envio e guarda a sessão (F-0226)', async () => {
    readRoomMessages.mockImplementationOnce(() =>
      Promise.resolve([{ seq: 1, type: 'open', topic: 'Fila serial', time: '2026-08-06T00:00:00Z' }]),
    )
    render(<WarRoomDetail id="R1" />)

    await waitFor(() => expect(screen.getByPlaceholderText(/apelido/i)).toBeInTheDocument())
    expect(screen.queryByPlaceholderText(/escreva uma mensagem/i)).not.toBeInTheDocument()

    joinRoom.mockResolvedValue({
      room: room({ id: 'R1', participants: [{ nick: 'ana', joined_at: 'x' }] }),
      session: session(),
    })
    fireEvent.change(screen.getByPlaceholderText(/apelido/i), { target: { value: 'ana' } })
    fireEvent.click(screen.getByRole('button', { name: /entrar/i }))
    await waitFor(() => expect(joinRoom).toHaveBeenCalledWith('R1', 'ana', ''))
    await waitFor(() => expect(screen.getByPlaceholderText(/escreva uma mensagem/i)).toBeInTheDocument())
    expect(JSON.parse(localStorage.getItem(SESSION_KEY) ?? '{}').session_id).toBe(
      'cafebabecafebabecafebabecafebabe',
    )
  })

  it('reload retoma a sessão guardada via resume, sem novo join (F-0226 critério 9)', async () => {
    localStorage.setItem(SESSION_KEY, JSON.stringify(session()))
    resumeRoomSession.mockResolvedValue(session())
    readRoomMessages.mockImplementationOnce(() =>
      Promise.resolve([
        { seq: 1, type: 'open', topic: 'Fila serial', time: '2026-08-06T00:00:00Z' },
        { seq: 2, type: 'join', nick: 'ana', time: '2026-08-06T00:00:01Z' },
      ]),
    )
    render(<WarRoomDetail id="R1" />)

    await waitFor(() => expect(screen.getByPlaceholderText(/escreva uma mensagem/i)).toBeInTheDocument())
    expect(resumeRoomSession).toHaveBeenCalledWith('R1', 'cafebabecafebabecafebabecafebabe')
    expect(joinRoom).not.toHaveBeenCalled()
  })

  it('token inválido volta ao formulário de join sem impersonar (F-0226 critério 9)', async () => {
    localStorage.setItem(SESSION_KEY, JSON.stringify(session()))
    resumeRoomSession.mockRejectedValue(new Error('invalid room session'))
    readRoomMessages.mockImplementationOnce(() =>
      Promise.resolve([{ seq: 1, type: 'open', topic: 'Fila serial', time: '2026-08-06T00:00:00Z' }]),
    )
    render(<WarRoomDetail id="R1" />)

    await waitFor(() => expect(resumeRoomSession).toHaveBeenCalled())
    await waitFor(() => expect(screen.getByPlaceholderText(/apelido/i)).toBeInTheDocument())
    expect(screen.queryByPlaceholderText(/escreva uma mensagem/i)).not.toBeInTheDocument()
    await waitFor(() => expect(localStorage.getItem(SESSION_KEY)).toBeNull())
  })

  it('envio usa a capability da sessão, nunca o nick (F-0226 critério 9)', async () => {
    localStorage.setItem(SESSION_KEY, JSON.stringify(session()))
    resumeRoomSession.mockResolvedValue(session())
    readRoomMessages.mockImplementationOnce(() =>
      Promise.resolve([{ seq: 1, type: 'open', topic: 'Fila serial', time: '2026-08-06T00:00:00Z' }]),
    )
    sayRoom.mockResolvedValue({ seq: 2, type: 'message', nick: 'ana', body: 'oi', time: 'x' })
    render(<WarRoomDetail id="R1" />)

    await waitFor(() => expect(screen.getByPlaceholderText(/escreva uma mensagem/i)).toBeInTheDocument())
    fireEvent.change(screen.getByPlaceholderText(/escreva uma mensagem/i), { target: { value: 'oi' } })
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))
    await waitFor(() =>
      expect(sayRoom).toHaveBeenCalledWith('R1', 'cafebabecafebabecafebabecafebabe', 'oi'),
    )
  })

  it('sala fechada esconde os formulários e mostra aviso', async () => {
    readRoomMessages.mockImplementationOnce(() =>
      Promise.resolve([
        { seq: 1, type: 'open', topic: 'Fila serial', time: '2026-08-06T00:00:00Z' },
        { seq: 2, type: 'close', time: '2026-08-06T00:05:00Z' },
      ]),
    )
    render(<WarRoomDetail id="R1" />)

    await waitFor(() => expect(screen.getByText(/sala fechada/i)).toBeInTheDocument())
    expect(screen.queryByPlaceholderText(/apelido/i)).not.toBeInTheDocument()
    expect(screen.queryByPlaceholderText(/escreva uma mensagem/i)).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /encerrar sala/i })).not.toBeInTheDocument()
  })

  it('mostra comando de entrada em sala aberta (critério 1)', async () => {
    readRoomMessages.mockImplementationOnce(() =>
      Promise.resolve([{ seq: 1, type: 'open', topic: 'Fila serial', time: '2026-08-06T00:00:00Z' }]),
    )
    render(<WarRoomDetail id="R1" />)
    await waitFor(() =>
      expect(screen.getByText(/cerne room join R1 --nick/)).toBeInTheDocument(),
    )
  })

  // --- F-0228: facilitação e ata estruturada ---

  it('sem capability de facilitador, guarda e permite assumir via handoff', async () => {
    readRoomMessages.mockImplementationOnce(() =>
      Promise.resolve([{ seq: 1, type: 'open', topic: 'Fila serial', time: '2026-08-06T00:00:00Z' }]),
    )
    postRoomFacilitatorHandoff.mockResolvedValue('novacapabilitynovacapabilitynova')
    render(<WarRoomDetail id="R1" />)

    await waitFor(() => expect(screen.getByText(/assumir facilitação/i)).toBeInTheDocument())
    // sem capability, nem fila nem formulário de fechamento aparecem.
    expect(screen.queryByText(/precisa de decisão humana/i)).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /encerrar sala/i })).not.toBeInTheDocument()

    fireEvent.change(screen.getByPlaceholderText(/cole a capability/i), {
      target: { value: 'capabilityemitidaparaoutro00000' },
    })
    fireEvent.click(screen.getByRole('button', { name: /^assumir$/i }))

    await waitFor(() =>
      expect(postRoomFacilitatorHandoff).toHaveBeenCalledWith('R1', 'capabilityemitidaparaoutro00000'),
    )
    await waitFor(() => expect(localStorage.getItem('warroom-facilitator--R1')).toBe('novacapabilitynovacapabilitynova'))
    await waitFor(() => expect(screen.getByText(/você é o.a. facilitador/i)).toBeInTheDocument())
  })

  // B-0083
  it('transferir facilitação pede confirmação antes de invalidar a capability do navegador', async () => {
    localStorage.setItem('warroom-facilitator--R1', 'facfacfacfacfacfacfacfacfacfacfa')
    readRoomMessages.mockImplementationOnce(() =>
      Promise.resolve([{ seq: 1, type: 'open', topic: 'Fila serial', time: '2026-08-06T00:00:00Z' }]),
    )
    postRoomFacilitatorHandoff.mockResolvedValue('novacapabilitynovacapabilitynova')
    const confirm = vi.spyOn(window, 'confirm').mockReturnValueOnce(false).mockReturnValueOnce(true)
    render(<WarRoomDetail id="R1" />)

    const transfer = await screen.findByRole('button', { name: /transferir facilitação/i })
    fireEvent.click(transfer)
    expect(confirm).toHaveBeenCalledTimes(1)
    expect(postRoomFacilitatorHandoff).not.toHaveBeenCalled()
    expect(localStorage.getItem('warroom-facilitator--R1')).toBe('facfacfacfacfacfacfacfacfacfacfa')

    fireEvent.click(transfer)
    await waitFor(() =>
      expect(postRoomFacilitatorHandoff).toHaveBeenCalledWith('R1', 'facfacfacfacfacfacfacfacfacfacfa'),
    )
    await waitFor(() => expect(screen.getByText(/novacapabilitynovacapabilitynova/)).toBeInTheDocument())
    confirm.mockRestore()
  })

  it('destaca a fila de itens que precisam de decisão humana e permite resolver (F-0228 critério 9)', async () => {
    localStorage.setItem('warroom-facilitator--R1', 'facfacfacfacfacfacfacfacfacfacfa')
    readRoomMessages.mockImplementationOnce(() =>
      Promise.resolve([{ seq: 1, type: 'open', topic: 'Fila serial', time: '2026-08-06T00:00:00Z' }]),
    )
    fetchRoomPending.mockResolvedValue([
      { id: 'I2', kind: 'proposal', author: 'ana', title: 'usar postgres', body: 'menos manutenção', source_seq: 2, status: 'open' },
    ])
    postRoomResolveItem.mockResolvedValue({
      id: 'I2',
      kind: 'proposal',
      author: 'ana',
      title: 'usar postgres',
      body: 'menos manutenção',
      source_seq: 2,
      status: 'resolved',
      result: 'aceito',
    })
    render(<WarRoomDetail id="R1" />)

    await waitFor(() => expect(screen.getByText('usar postgres')).toBeInTheDocument())
    fireEvent.change(screen.getByPlaceholderText(/resultado da decisão/i), { target: { value: 'aceito' } })
    fireEvent.click(screen.getByRole('button', { name: /^resolver$/i }))

    await waitFor(() =>
      expect(postRoomResolveItem).toHaveBeenCalledWith('R1', 'facfacfacfacfacfacfacfacfacfacfa', 'I2', 'aceito'),
    )
    await waitFor(() => expect(screen.queryByText('usar postgres')).not.toBeInTheDocument())
  })

  it('facilitador encerra sala com ata estruturada, substituindo o confirm() simples (F-0228)', async () => {
    localStorage.setItem('warroom-facilitator--R1', 'facfacfacfacfacfacfacfacfacfacfa')
    readRoomMessages.mockImplementationOnce(() =>
      Promise.resolve([{ seq: 1, type: 'open', topic: 'Fila serial', time: '2026-08-06T00:00:00Z' }]),
    )
    const minutes: Minutes = {
      conclusion: 'reverter o deploy',
      rationale: 'mais rápido que corrigir ao vivo',
      discarded_options: [],
      owners: [],
      next_steps: [],
    }
    postRoomClose.mockResolvedValue(room({ id: 'R1', closed_at: '2026-08-06T01:00:00Z', minutes }))
    render(<WarRoomDetail id="R1" />)

    await waitFor(() => expect(screen.getByRole('button', { name: /encerrar sala/i })).toBeInTheDocument())
    fireEvent.change(screen.getByLabelText(/^conclusão$/i), { target: { value: minutes.conclusion } })
    fireEvent.change(screen.getByLabelText(/^justificativa$/i), { target: { value: minutes.rationale } })
    fireEvent.click(screen.getByRole('button', { name: /encerrar sala/i }))

    await waitFor(() => expect(postRoomClose).toHaveBeenCalledWith('R1', 'facfacfacfacfacfacfacfacfacfacfa', minutes, false))
    await waitFor(() => expect(screen.getByText(/sala fechada/i)).toBeInTheDocument())
  })

  it('sala fechada mostra a ata antes do transcript (F-0228 critério 9)', async () => {
    readRoomMessages.mockImplementationOnce(() =>
      Promise.resolve([
        { seq: 1, type: 'open', topic: 'Fila serial', time: '2026-08-06T00:00:00Z' },
        {
          seq: 2,
          type: 'close',
          time: '2026-08-06T00:05:00Z',
          minutes: {
            conclusion: 'reverter o deploy',
            rationale: 'mais rápido',
            discarded_options: [],
            owners: [],
            next_steps: [],
          },
        },
      ]),
    )
    render(<WarRoomDetail id="R1" />)

    await waitFor(() => expect(screen.getByText('reverter o deploy')).toBeInTheDocument())
    const minutesHeading = screen.getByText(/ata da sala/i)
    const history = document.querySelector('.backlog-chat-history')
    expect(history).not.toBeNull()
    // eslint-disable-next-line testing-library/no-node-access
    expect(minutesHeading.compareDocumentPosition(history!) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })

  // F-0238: a sala fechada mostra o vínculo com a ata registrada.
  const closeEventWithMinutes = {
    seq: 2,
    type: 'close' as const,
    time: '2026-08-06T00:05:00Z',
    minutes: { conclusion: 'reverter o deploy', rationale: 'mais rápido', discarded_options: [], owners: [], next_steps: [] },
  }

  it('sala fechada com ata registrada mostra o link para a nota (F-0238 critério 4)', async () => {
    readRoomMessages.mockImplementationOnce(() =>
      Promise.resolve([{ seq: 1, type: 'open', topic: 'Fila serial', time: '2026-08-06T00:00:00Z' }, closeEventWithMinutes]),
    )
    fetchRooms.mockResolvedValue([room({ id: 'R1', closed_at: '2026-08-06T00:05:00Z', minutes_note: 'N-0007' })])
    render(<WarRoomDetail id="R1" />)

    const link = await screen.findByRole('link', { name: 'N-0007' })
    expect(link).toHaveAttribute('href', '#/n/N-0007')
  })

  it('sala fechada sem ata registrada não mostra o link', async () => {
    readRoomMessages.mockImplementationOnce(() =>
      Promise.resolve([{ seq: 1, type: 'open', topic: 'Fila serial', time: '2026-08-06T00:00:00Z' }, closeEventWithMinutes]),
    )
    render(<WarRoomDetail id="R1" />)

    await waitFor(() => expect(screen.getByText('reverter o deploy')).toBeInTheDocument())
    expect(screen.queryByText(/ata registrada/i)).not.toBeInTheDocument()
  })

  // F-0242: trocar de projeto deve re-buscar (critério 2) — antes, nenhum
  // efeito de WarRoom.tsx dependia da versão ao vivo (useLiveVersion, mesmo
  // mecanismo de Kanban.tsx), então a tela congelava mostrando dado velho.
  describe('re-busca ao trocar de projeto (F-0242 critério 2)', () => {
    const projects: ProjectInfo[] = [
      { pid: 'p1', name: 'P1' },
      { pid: 'p2', name: 'P2' },
    ]

    function Switcher() {
      const { setActivePid } = useProjects()
      return (
        <button type="button" onClick={() => setActivePid('p2')}>
          trocar projeto
        </button>
      )
    }

    it('bump de version dispara nova busca de salas', async () => {
      vi.mocked(fetchProjects).mockResolvedValue({ projects, default: 'p1' })
      fetchRooms.mockResolvedValue([])

      render(
        <LiveProvider>
          <Switcher />
          <WarRoom />
        </LiveProvider>,
      )

      await waitFor(() => expect(fetchRooms).toHaveBeenCalledTimes(1))
      fireEvent.click(screen.getByRole('button', { name: /trocar projeto/i }))
      await waitFor(() => expect(fetchRooms).toHaveBeenCalledTimes(2))
    })
  })
})

// F-0292: a transcrição abre no fim e acompanha as mensagens novas enquanto o
// leitor estiver lá — o mesmo glue-scroll do chat do Backlog (F-0239), ligado
// aqui pela isAtBottom que já era exportada.
describe('glue-scroll da transcrição (F-0292)', () => {
  const CLIENT_HEIGHT = 400
  const SCROLL_HEIGHT = 1000
  // Fim exato: scrollHeight - scrollTop - clientHeight === 0.
  const AT_BOTTOM = SCROLL_HEIGHT - CLIENT_HEIGHT

  let restoreMetrics: (() => void) | null = null

  // jsdom não faz layout: scrollHeight/clientHeight são sempre 0 e scrollTop é
  // um acessor inerte, então sem stub o glue-scroll não é observável. Os stubs
  // vão no protótipo, não no nó, porque o ajuste de montagem (critério 1) roda
  // antes de o teste conseguir a referência do elemento.
  beforeEach(() => {
    const original = {
      scrollHeight: Object.getOwnPropertyDescriptor(Element.prototype, 'scrollHeight'),
      clientHeight: Object.getOwnPropertyDescriptor(Element.prototype, 'clientHeight'),
      scrollTop: Object.getOwnPropertyDescriptor(Element.prototype, 'scrollTop'),
    }
    Object.defineProperty(Element.prototype, 'scrollHeight', {
      configurable: true,
      get: () => SCROLL_HEIGHT,
    })
    Object.defineProperty(Element.prototype, 'clientHeight', {
      configurable: true,
      get: () => CLIENT_HEIGHT,
    })
    // Data property: escrever em um nó cria a própria cópia, então scrollTop
    // volta a ser legível — o acessor do jsdom descarta a escrita.
    Object.defineProperty(Element.prototype, 'scrollTop', {
      configurable: true,
      writable: true,
      value: 0,
    })
    restoreMetrics = () => {
      for (const [key, desc] of Object.entries(original)) {
        if (desc) Object.defineProperty(Element.prototype, key, desc)
      }
    }
  })

  afterEach(() => {
    restoreMetrics?.()
    restoreMetrics = null
  })

  const openEvent: RoomMessage = {
    seq: 1,
    type: 'open',
    topic: 'Fila serial',
    time: '2026-08-06T00:00:00Z',
  }

  const msg = (seq: number, body: string): RoomMessage => ({
    seq,
    type: 'message',
    nick: 'ana',
    body,
    time: '2026-08-06T00:00:00Z',
  })

  const history = () => {
    const el = document.querySelector('.backlog-chat-history')
    if (!el) throw new Error('transcrição não montou')
    return el as HTMLDivElement
  }

  // Transcrição estática: a leitura inicial devolve os eventos, o long-poll
  // trava (nenhuma mensagem nova durante o teste).
  const staticTranscript = (initial: RoomMessage[]) =>
    readRoomMessages.mockImplementation((_id, _since, follow) =>
      follow ? new Promise<RoomMessage[]>(() => {}) : Promise.resolve(initial),
    )

  // Transcrição controlada: cada espera com follow=true fica na mão do teste,
  // que decide quando (e com o quê) a mensagem seguinte chega.
  function controllableTranscript(initial: RoomMessage[]) {
    const box: { deliver: ((events: RoomMessage[]) => void) | null } = { deliver: null }
    readRoomMessages.mockImplementation((_id, _since, follow) => {
      if (!follow) return Promise.resolve(initial)
      return new Promise<RoomMessage[]>((resolve) => {
        box.deliver = resolve
      })
    })
    return async (events: RoomMessage[]) => {
      await waitFor(() => expect(box.deliver).not.toBeNull())
      const deliver = box.deliver!
      box.deliver = null
      await act(async () => deliver(events))
    }
  }

  it('abre a transcrição no fim (critério 1)', async () => {
    staticTranscript([openEvent, msg(2, 'primeira'), msg(3, 'última')])
    render(<WarRoomDetail id="R1" />)

    await waitFor(() => expect(screen.getByText(/última/)).toBeInTheDocument())
    expect(history().scrollTop).toBe(SCROLL_HEIGHT)
  })

  it('cola no fim quando chega mensagem nova e o leitor está no fim (critério 2)', async () => {
    const deliver = controllableTranscript([openEvent, msg(2, 'primeira')])
    render(<WarRoomDetail id="R1" />)
    await waitFor(() => expect(screen.getByText(/primeira/)).toBeInTheDocument())

    history().scrollTop = 0
    await deliver([msg(3, 'segunda')])

    await waitFor(() => expect(screen.getByText(/segunda/)).toBeInTheDocument())
    expect(history().scrollTop).toBe(SCROLL_HEIGHT)
  })

  it('preserva a posição quando o leitor rolou para cima (critério 3)', async () => {
    const deliver = controllableTranscript([openEvent, msg(2, 'primeira')])
    render(<WarRoomDetail id="R1" />)
    await waitFor(() => expect(screen.getByText(/primeira/)).toBeInTheDocument())

    // 100px acima do fim: bem além da tolerância de 24px do isAtBottom.
    history().scrollTop = AT_BOTTOM - 100
    fireEvent.scroll(history())
    await deliver([msg(3, 'segunda')])

    await waitFor(() => expect(screen.getByText(/segunda/)).toBeInTheDocument())
    expect(history().scrollTop).toBe(AT_BOTTOM - 100)
  })

  it('volta a colar quando o leitor retorna ao fim (critério 4)', async () => {
    const deliver = controllableTranscript([openEvent, msg(2, 'primeira')])
    render(<WarRoomDetail id="R1" />)
    await waitFor(() => expect(screen.getByText(/primeira/)).toBeInTheDocument())

    history().scrollTop = AT_BOTTOM - 100
    fireEvent.scroll(history())
    await deliver([msg(3, 'segunda')])
    await waitFor(() => expect(screen.getByText(/segunda/)).toBeInTheDocument())
    expect(history().scrollTop).toBe(AT_BOTTOM - 100)

    history().scrollTop = AT_BOTTOM
    fireEvent.scroll(history())
    await deliver([msg(4, 'terceira')])

    await waitFor(() => expect(screen.getByText(/terceira/)).toBeInTheDocument())
    expect(history().scrollTop).toBe(SCROLL_HEIGHT)
  })

  it('trocar de sala reposiciona no fim sem herdar o descolamento (critério 5)', async () => {
    staticTranscript([openEvent, msg(2, 'primeira')])
    const { rerender } = render(<WarRoomDetail id="R1" />)
    await waitFor(() => expect(screen.getByText(/primeira/)).toBeInTheDocument())

    history().scrollTop = 0
    fireEvent.scroll(history())
    expect(history().scrollTop).toBe(0)

    staticTranscript([openEvent, msg(2, 'outra sala')])
    rerender(<WarRoomDetail id="R2" />)

    await waitFor(() => expect(screen.getByText(/outra sala/)).toBeInTheDocument())
    expect(history().scrollTop).toBe(SCROLL_HEIGHT)
  })

  it('reusa a isAtBottom do Backlog, sem duplicar a tolerância (critério 6)', () => {
    // vitest roda com cwd na raiz do pacote da UI.
    const src = readFileSync('src/WarRoom.tsx', 'utf8')
    expect(src).toMatch(/import \{ isAtBottom \} from '\.\/BacklogChat'/)
    expect(src).not.toMatch(/TOLERANCE/)
  })
})

// F-0291: o painel da ata mostrava 2 dos 6 campos da projeção canônica. A
// referência é RenderMinutesMarkdown (internal/room/minutes.go) — a UI não
// pode ficar mais pobre que a CLI sobre a mesma ata.
describe('painel da ata (F-0291)', () => {
  const fullMinutes: Minutes = {
    conclusion: 'Reverter o deploy e investigar amanhã.',
    rationale: 'O rollback é mais rápido que um fix ao vivo sob pressão.',
    discarded_options: ['hotfix ao vivo', 'esperar até segunda'],
    owners: ['alice'],
    next_steps: ['abrir post-mortem', 'revisar alertas de canary'],
    open_items: ['I3', 'I5'],
  }

  // A saída de RenderMinutesMarkdown para exatamente esta ata: o golden de
  // internal/room/minutes_test.go (TestRenderMinutesMarkdownGolden) somado à
  // seção de exceção de TestRenderMinutesMarkdownShowsOpenItemsException.
  const CANONICAL_MARKDOWN =
    '# Ata — R1 (deploy quebrado)\n\n' +
    '## Conclusão\n\nReverter o deploy e investigar amanhã.\n\n' +
    '## Justificativa\n\nO rollback é mais rápido que um fix ao vivo sob pressão.\n\n' +
    '## Opções descartadas\n\n- hotfix ao vivo\n- esperar até segunda\n\n' +
    '## Responsáveis\n\n- alice\n\n' +
    '## Próximos passos\n\n- abrir post-mortem\n- revisar alertas de canary\n\n' +
    '## Itens deixados em aberto (exceção registrada)\n\n- I3\n- I5\n\n'

  const closedTranscript = (minutes: Minutes) =>
    readRoomMessages.mockImplementation((_id, _since, follow) =>
      follow
        ? new Promise<RoomMessage[]>(() => {})
        : Promise.resolve([
            { seq: 1, type: 'open', topic: 'deploy quebrado', time: '2026-08-06T00:00:00Z' },
            { seq: 2, type: 'close', time: '2026-08-06T00:05:00Z', minutes },
          ]),
    )

  const renderClosed = async (minutes: Minutes) => {
    closedTranscript(minutes)
    render(<WarRoomDetail id="R1" />)
    await waitFor(() => expect(screen.getByText(minutes.conclusion)).toBeInTheDocument())
  }

  const panelText = () => {
    const panel = document.querySelector('.room-minutes')
    if (!panel) throw new Error('painel da ata não montou')
    return panel.textContent ?? ''
  }

  it('mostra as opções descartadas (critério 1)', async () => {
    await renderClosed(fullMinutes)
    expect(screen.getByText('Opções descartadas')).toBeInTheDocument()
    expect(screen.getByText('hotfix ao vivo')).toBeInTheDocument()
    expect(screen.getByText('esperar até segunda')).toBeInTheDocument()
  })

  it('mostra os responsáveis (critério 2)', async () => {
    await renderClosed(fullMinutes)
    expect(screen.getByText('Responsáveis')).toBeInTheDocument()
    expect(screen.getByText('alice')).toBeInTheDocument()
  })

  it('mostra os próximos passos (critério 3)', async () => {
    await renderClosed(fullMinutes)
    expect(screen.getByText('Próximos passos')).toBeInTheDocument()
    expect(screen.getByText('abrir post-mortem')).toBeInTheDocument()
    expect(screen.getByText('revisar alertas de canary')).toBeInTheDocument()
  })

  it('destaca os itens em aberto como exceção registrada (critério 4)', async () => {
    await renderClosed(fullMinutes)
    expect(screen.getByText('Itens deixados em aberto (exceção registrada)')).toBeInTheDocument()
    expect(screen.getByText('I3')).toBeInTheDocument()
    expect(screen.getByText('I5')).toBeInTheDocument()
    // Distinta das demais: só ela carrega a marca de exceção.
    expect(document.querySelector('.room-minutes-open-items')).not.toBeNull()
    expect(document.querySelectorAll('.room-minutes-open-items')).toHaveLength(1)
  })

  it('omite a seção inteira quando a lista está vazia, sem placeholder (critério 5)', async () => {
    await renderClosed({
      conclusion: 'Seguir com o deploy.',
      rationale: 'Sem regressão medida.',
      discarded_options: [],
      owners: [],
      next_steps: [],
    })
    expect(screen.queryByText('Opções descartadas')).toBeNull()
    expect(screen.queryByText('Responsáveis')).toBeNull()
    expect(screen.queryByText('Próximos passos')).toBeNull()
    expect(screen.queryByText(/Itens deixados em aberto/)).toBeNull()
    expect(panelText()).not.toMatch(/Nenhum/)
  })

  it('renderiza campo a campo o mesmo que a projeção canônica (critério 6)', async () => {
    await renderClosed(fullMinutes)
    const text = panelText()

    const sections = [...CANONICAL_MARKDOWN.matchAll(/^## (.+)$/gm)].map((m) => m[1])
    const items = [...CANONICAL_MARKDOWN.matchAll(/^- (.+)$/gm)].map((m) => m[1])
    expect(sections).toHaveLength(6)
    expect(items).toHaveLength(7)

    for (const section of sections) expect(text).toContain(section)
    for (const item of items) expect(text).toContain(item)
    expect(text).toContain(fullMinutes.conclusion)
    expect(text).toContain(fullMinutes.rationale)
  })

  it('tem os rótulos novos também em inglês (critério 7)', async () => {
    localStorage.setItem('cerne-lang', 'en')
    closedTranscript(fullMinutes)
    render(
      <I18nProvider>
        <WarRoomDetail id="R1" />
      </I18nProvider>,
    )

    await waitFor(() => expect(screen.getByText('Discarded options')).toBeInTheDocument())
    expect(screen.getByText('Owners')).toBeInTheDocument()
    expect(screen.getByText('Next steps')).toBeInTheDocument()
    expect(screen.getByText('Items left open (recorded exception)')).toBeInTheDocument()
  })

  it('o painel tem moldura própria e preserva a contenção de altura (critério 8)', () => {
    const css = readFileSync('src/app.css', 'utf8')
    const block = /\.room-minutes \{[^}]*\}/.exec(css)?.[0] ?? ''
    expect(block).toMatch(/border:/)
    expect(block).toMatch(/background:/)
    // A contenção do F-0243/F-0241 continua de pé.
    expect(block).toMatch(/max-height: 35%/)
    expect(block).toMatch(/min-height: 0/)
    expect(block).toMatch(/overflow-y: auto/)
    expect(css).toMatch(/\.room-minutes-open-items \{/)
  })
})

// B-0046: the room borrowed .backlog-layout and broke when F-0431 pinned that
// grid's cells to the Backlog's own children — the room's chat fell into the
// first free cell, on top of the side column. The room now owns its grid, and
// the chat follows the conventions of the chats its users already know.
describe('layout e conversa da sala (B-0046)', () => {
  const open: RoomMessage = { seq: 1, type: 'open', topic: 'Fila serial', time: '2026-08-06T00:00:00Z' }
  const say = (seq: number, nick: string, body: string, time: string): RoomMessage => ({
    seq,
    type: 'message',
    nick,
    body,
    time,
  })

  const joinedAs = (nick: string) => {
    localStorage.setItem(SESSION_KEY, JSON.stringify(session({ nick })))
    resumeRoomSession.mockResolvedValue(session({ nick }))
  }

  it('a sala tem grid próprio, independente do grid do Backlog (critério de regressão)', async () => {
    readRoomMessages.mockImplementationOnce(() => Promise.resolve([open]))
    const { container } = render(<WarRoomDetail id="R1" />)
    await waitFor(() => expect(screen.getByText(/Fila serial/)).toBeInTheDocument())

    // eslint-disable-next-line testing-library/no-container, testing-library/no-node-access
    const main = container.querySelector('main')
    expect(main).toHaveClass('room-layout')
    expect(main).not.toHaveClass('backlog-layout')
    // eslint-disable-next-line testing-library/no-container, testing-library/no-node-access
    expect(container.querySelector('.room-chat')).not.toBeNull()
    // eslint-disable-next-line testing-library/no-container, testing-library/no-node-access
    expect(container.querySelector('.room-side')).not.toBeNull()

    // Cada região tem célula declarada: nenhuma depende do auto-placement.
    const css = readFileSync('src/app.css', 'utf8')
    const rule = (sel: string) => new RegExp(`\\n${sel.replace('.', '\\.')} \\{[^}]*\\}`).exec(css)?.[0] ?? ''
    expect(rule('.room-chat')).toMatch(/grid-column: 1;[\s\S]*grid-row: 2;/)
    expect(rule('.room-side')).toMatch(/grid-column: 2;[\s\S]*grid-row: 2;/)
    expect(rule('.room-head')).toMatch(/grid-column: 1 \/ -1/)
  })

  it('Enter envia e Shift+Enter não (padrão Claude Code / Cursor / Gemini)', async () => {
    joinedAs('ana')
    readRoomMessages.mockImplementationOnce(() => Promise.resolve([open]))
    sayRoom.mockResolvedValue(say(2, 'ana', 'oi', '2026-08-06T00:00:02Z'))
    render(<WarRoomDetail id="R1" />)

    const box = await screen.findByPlaceholderText(/escreva uma mensagem/i)
    fireEvent.change(box, { target: { value: 'linha 1' } })
    fireEvent.keyDown(box, { key: 'Enter', shiftKey: true })
    expect(sayRoom).not.toHaveBeenCalled()

    fireEvent.keyDown(box, { key: 'Enter' })
    await waitFor(() => expect(sayRoom).toHaveBeenCalledWith('R1', 'cafebabecafebabecafebabecafebabe', 'linha 1'))
    await waitFor(() => expect(box).toHaveValue(''))
  })

  it('Enter durante composição de IME não envia', async () => {
    joinedAs('ana')
    readRoomMessages.mockImplementationOnce(() => Promise.resolve([open]))
    render(<WarRoomDetail id="R1" />)

    const box = await screen.findByPlaceholderText(/escreva uma mensagem/i)
    fireEvent.change(box, { target: { value: 'にほ' } })
    fireEvent.keyDown(box, { key: 'Enter', isComposing: true })
    expect(sayRoom).not.toHaveBeenCalled()
  })

  // F-0493 crit. 1, 2
  it('F-0493 crit. 1: a hora da mensagem segue o locale do navegador, e o title diz o fuso', async () => {
    localStorage.setItem('cerne-time-zone', 'UTC')
    vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['en-US'])
    vi.spyOn(navigator, 'language', 'get').mockReturnValue('en-US')
    readRoomMessages.mockImplementationOnce(() => Promise.resolve([open, say(2, 'bia', 'primeira', '2026-08-06T00:00:02Z')]))
    const { container } = render(
      <I18nProvider>
        <WarRoomDetail id="R1" />
      </I18nProvider>,
    )
    await waitFor(() => expect(screen.getByText('primeira')).toBeInTheDocument())
    // eslint-disable-next-line testing-library/no-container, testing-library/no-node-access
    const stamp = container.querySelector('time[datetime="2026-08-06T00:00:02Z"]')
    expect(stamp).toHaveTextContent('12:00 AM')
    expect(stamp).toHaveAttribute('title', '8/6/2026, 12:00:02 AM UTC')
  })

  it('mensagens seguidas do mesmo autor mostram o autor uma vez só', async () => {
    readRoomMessages.mockImplementationOnce(() =>
      Promise.resolve([
        open,
        say(2, 'bia', 'primeira', '2026-08-06T00:00:02Z'),
        say(3, 'bia', 'segunda', '2026-08-06T00:01:00Z'),
        say(4, 'caio', 'outra pessoa', '2026-08-06T00:01:30Z'),
        say(5, 'bia', 'de volta', '2026-08-06T00:02:00Z'),
        say(6, 'bia', 'bem depois', '2026-08-06T00:30:00Z'),
      ]),
    )
    render(<WarRoomDetail id="R1" />)
    await waitFor(() => expect(screen.getByText('bem depois')).toBeInTheDocument())

    const log = screen.getByRole('log')
    // primeira, de volta (outro autor no meio), bem depois (janela passou)
    expect(within(log).getAllByText('bia')).toHaveLength(3)
    expect(within(log).getAllByText('caio')).toHaveLength(1)
  })

  it('a própria mensagem fica à direita sem repetir o apelido; a dos outros traz o autor', async () => {
    joinedAs('ana')
    readRoomMessages.mockImplementationOnce(() =>
      Promise.resolve([
        open,
        say(2, 'ana', 'minha', '2026-08-06T00:00:02Z'),
        say(3, 'bia', 'dela', '2026-08-06T00:00:03Z'),
      ]),
    )
    render(<WarRoomDetail id="R1" />)
    await waitFor(() => expect(screen.getByText('dela')).toBeInTheDocument())

    // eslint-disable-next-line testing-library/no-node-access
    const mine = screen.getByText('minha').closest('.room-msg')
    // eslint-disable-next-line testing-library/no-node-access
    const theirs = screen.getByText('dela').closest('.room-msg')
    expect(mine).toHaveClass('room-msg-own')
    expect(theirs).not.toHaveClass('room-msg-own')
    expect(within(screen.getByRole('log')).queryByText('ana')).not.toBeInTheDocument()
    expect(within(theirs as HTMLElement).getByText('bia')).toBeInTheDocument()
    expect(screen.getByText(/falando como ana/i)).toBeInTheDocument()
  })

  // --- F-0511: observar, participar e sair ---

  // F-0511 crit. 1
  it('sem sessão, identifica o observador, mostra a transcrição e diz que entrar só é preciso para publicar', async () => {
    readRoomMessages.mockImplementationOnce(() => Promise.resolve([open, say(2, 'bia', 'oi pessoal', '2026-08-06T00:00:02Z')]))
    render(<WarRoomDetail id="R1" />)

    await waitFor(() => expect(screen.getByText('oi pessoal')).toBeInTheDocument())
    expect(screen.getByText('Observando')).toBeInTheDocument()
    expect(screen.getByText(/entrar só é preciso para publicar/i)).toBeInTheDocument()
    expect(screen.getByText('Entrar para participar')).toBeInTheDocument()
    expect(screen.queryByPlaceholderText(/escreva uma mensagem/i)).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /^sair$/i })).not.toBeInTheDocument()
  })

  // F-0511 crit. 6
  it('sair revoga no servidor antes de apagar a sessão local e volta a observar, sem tocar a facilitação', async () => {
    joinedAs('ana')
    localStorage.setItem('warroom-facilitator--R1', 'facfacfacfacfacfacfacfacfacfacfa')
    readRoomMessages.mockImplementationOnce(() => Promise.resolve([open]))
    let finish: (r: Room) => void = () => {}
    leaveRoom.mockImplementation(() => new Promise<Room>((resolve) => (finish = resolve)))
    render(<WarRoomDetail id="R1" />)

    await screen.findByPlaceholderText(/escreva uma mensagem/i)
    expect(screen.getByText('Participando')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /^sair$/i }))
    await waitFor(() => expect(leaveRoom).toHaveBeenCalledWith('R1', 'cafebabecafebabecafebabecafebabe'))
    // still waiting for the server: the local session stays.
    expect(localStorage.getItem(SESSION_KEY)).not.toBeNull()
    expect(screen.getByPlaceholderText(/escreva uma mensagem/i)).toBeInTheDocument()

    await act(async () => finish(room({ active_participants: [] })))
    await waitFor(() => expect(screen.getByPlaceholderText(/apelido/i)).toBeInTheDocument())
    expect(localStorage.getItem(SESSION_KEY)).toBeNull()
    expect(screen.getByText('Observando')).toBeInTheDocument()
    expect(screen.queryByPlaceholderText(/escreva uma mensagem/i)).not.toBeInTheDocument()
    expect(localStorage.getItem('warroom-facilitator--R1')).toBe('facfacfacfacfacfacfacfacfacfacfa')
  })

  // F-0511 crit. 6: a failure that can be retried — network, 5xx, 409.
  it.each([
    ['rede', new Error('conexão recusada')],
    ['5xx', new HTTPError('conexão recusada', 500)],
    ['409', new HTTPError('conexão recusada', 409)],
  ])('erro ao sair (%s) mantém a sessão local, mostra a falha e deixa tentar de novo', async (_kind, failure) => {
    joinedAs('ana')
    readRoomMessages.mockImplementationOnce(() => Promise.resolve([open]))
    leaveRoom.mockRejectedValueOnce(failure).mockResolvedValueOnce(room({ active_participants: [] }))
    render(<WarRoomDetail id="R1" />)

    await screen.findByPlaceholderText(/escreva uma mensagem/i)
    fireEvent.click(screen.getByRole('button', { name: /^sair$/i }))
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent(/conexão recusada/))
    expect(localStorage.getItem(SESSION_KEY)).not.toBeNull()
    expect(screen.getByPlaceholderText(/escreva uma mensagem/i)).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: /^sair$/i }))
    await waitFor(() => expect(leaveRoom).toHaveBeenCalledTimes(2))
    await waitFor(() => expect(localStorage.getItem(SESSION_KEY)).toBeNull())
  })

  // F-0511 crit. 6: a 401 means the server had already revoked the capability
  // (another tab, the terminal): nothing to retry, so the browser forgets it.
  it('sessão já revogada (401) ao sair: esquece a sessão local, volta a observar e avisa', async () => {
    joinedAs('ana')
    readRoomMessages.mockImplementationOnce(() => Promise.resolve([open]))
    leaveRoom.mockRejectedValueOnce(new HTTPError('invalid room session', 401))
    render(<WarRoomDetail id="R1" />)

    await screen.findByPlaceholderText(/escreva uma mensagem/i)
    fireEvent.click(screen.getByRole('button', { name: /^sair$/i }))
    await waitFor(() => expect(screen.getByPlaceholderText(/apelido/i)).toBeInTheDocument())
    expect(localStorage.getItem(SESSION_KEY)).toBeNull()
    expect(screen.getByText('Observando')).toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent(/já não era válida/i)
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    expect(screen.queryByPlaceholderText(/escreva uma mensagem/i)).not.toBeInTheDocument()
    expect(leaveRoom).toHaveBeenCalledTimes(1)
  })

  // F-0511 crit. 5
  it('o painel conta os participantes ativos, não o histórico', async () => {
    readRoomMessages.mockImplementationOnce(() =>
      Promise.resolve([
        open,
        { seq: 2, type: 'join', nick: 'ana', time: '2026-08-06T00:00:01Z' },
        { seq: 3, type: 'join', nick: 'bia', bio: 'cética', time: '2026-08-06T00:00:02Z' },
        { seq: 4, type: 'leave', nick: 'ana', time: '2026-08-06T00:00:03Z' },
      ]),
    )
    fetchRooms.mockResolvedValue([
      room({
        participants: [
          { nick: 'ana', joined_at: '2026-08-06T00:00:01Z' },
          { nick: 'bia', bio: 'cética', joined_at: '2026-08-06T00:00:02Z' },
        ],
        active_participants: [{ nick: 'bia', bio: 'cética', joined_at: '2026-08-06T00:00:02Z' }],
      }),
    ])
    render(<WarRoomDetail id="R1" />)

    const roster = screen.getByRole('region', { name: /participantes/i })
    await waitFor(() => expect(within(roster).getByText('bia')).toBeInTheDocument())
    expect(within(roster).queryByText('ana')).not.toBeInTheDocument()
    expect(within(roster).getByText('1')).toBeInTheDocument()
  })

  // F-0511 crit. 2, 4
  it('entrada e saída aparecem na transcrição', async () => {
    readRoomMessages.mockImplementationOnce(() =>
      Promise.resolve([
        open,
        { seq: 2, type: 'join', nick: 'ana', time: '2026-08-06T00:00:01Z' },
        { seq: 3, type: 'leave', nick: 'ana', time: '2026-08-06T00:00:02Z' },
        { seq: 4, type: 'join', nick: 'ana', time: '2026-08-06T00:00:03Z' },
      ]),
    )
    render(<WarRoomDetail id="R1" />)

    const log = screen.getByRole('log')
    await waitFor(() => expect(within(log).getByText('ana saiu')).toBeInTheDocument())
    expect(within(log).getAllByText('ana entrou')).toHaveLength(2)
  })

  // F-0511 crit. 1, 6
  it('tem os rótulos de observar, participar e sair também em inglês', async () => {
    localStorage.setItem('cerne-lang', 'en')
    readRoomMessages.mockImplementationOnce(() => Promise.resolve([open, { seq: 2, type: 'leave', nick: 'ana', time: '2026-08-06T00:00:02Z' }]))
    render(
      <I18nProvider>
        <WarRoomDetail id="R1" />
      </I18nProvider>,
    )

    await waitFor(() => expect(screen.getByText('Observing')).toBeInTheDocument())
    expect(screen.getByText('Join to take part')).toBeInTheDocument()
    expect(screen.getByText(/joining is only needed to post/i)).toBeInTheDocument()
    expect(screen.getByText('ana left')).toBeInTheDocument()
  })

  it('o encerramento com ata fica recolhido até ser pedido', async () => {
    localStorage.setItem('warroom-facilitator--R1', 'facfacfacfacfacfacfacfacfacfacfa')
    readRoomMessages.mockImplementationOnce(() => Promise.resolve([open]))
    render(<WarRoomDetail id="R1" />)

    const summary = await screen.findByText(/encerrar sala com ata/i)
    // eslint-disable-next-line testing-library/no-node-access
    const details = summary.closest('details')
    expect(details).not.toBeNull()
    expect(details).not.toHaveAttribute('open')
  })
})

describe('presença ao vivo no painel (T-0021)', () => {
  const open: RoomMessage = { seq: 1, type: 'open', topic: 'Fila serial', time: '2026-08-06T00:00:00Z' }
  const fiveMinAgo = () => new Date(Date.now() - 5 * 60_000).toISOString()
  const presence = (lastSeen: string) =>
    room({
      active_participants: [
        { nick: 'ana', joined_at: '2026-08-06T00:00:01Z', listening: true, last_seen_at: lastSeen },
        { nick: 'bia', joined_at: '2026-08-06T00:00:02Z', last_seen_at: lastSeen },
      ],
    })
  const entryOf = (nick: string) =>
    // eslint-disable-next-line testing-library/no-node-access
    within(screen.getByRole('region', { name: /participantes|participants/i })).getByText(nick).closest('li')!

  afterEach(() => {
    vi.useRealTimers()
  })

  // T-0021 crit. 3
  // E-0006 crit. 9
  it('distingue quem está em escuta de quem está fora, com a última atividade', async () => {
    const lastSeen = fiveMinAgo()
    readRoomMessages.mockImplementationOnce(() => Promise.resolve([open]))
    fetchRooms.mockResolvedValue([presence(lastSeen)])
    render(<WarRoomDetail id="R1" />)

    await waitFor(() => expect(within(entryOf('ana')).getByText('em escuta')).toBeInTheDocument())
    expect(within(entryOf('ana')).queryByText(/última atividade/)).not.toBeInTheDocument()
    const bia = entryOf('bia')
    expect(within(bia).getByText(/fora de escuta/)).toBeInTheDocument()
    expect(within(bia).getByText(/última atividade há 5 minutos/)).toBeInTheDocument()
    // eslint-disable-next-line testing-library/no-node-access
    expect(bia.querySelector(`time[datetime="${lastSeen}"]`)).toHaveAttribute('title')
  })

  // T-0021 crit. 3
  // E-0006 crit. 9
  it('sala fechada não marca presença', async () => {
    readRoomMessages.mockImplementationOnce(() =>
      Promise.resolve([open, { seq: 2, type: 'close', time: '2026-08-06T00:05:00Z' }]),
    )
    fetchRooms.mockResolvedValue([{ ...presence(fiveMinAgo()), closed_at: '2026-08-06T00:05:00Z' }])
    render(<WarRoomDetail id="R1" />)

    await waitFor(() => expect(entryOf('bia')).toBeInTheDocument())
    expect(screen.queryByText('em escuta')).not.toBeInTheDocument()
    expect(screen.queryByText(/fora de escuta/)).not.toBeInTheDocument()
  })

  // T-0021 crit. 3
  // E-0006 crit. 9
  it('refaz a presença a cada lote de eventos, sem segundo long-poll', async () => {
    let deliver: (events: RoomMessage[]) => void = () => {}
    readRoomMessages.mockImplementationOnce(() => Promise.resolve([open]))
    readRoomMessages.mockImplementationOnce(() => new Promise((resolve) => (deliver = resolve)))
    render(<WarRoomDetail id="R1" />)
    await waitFor(() => expect(readRoomMessages).toHaveBeenCalledTimes(2))
    await waitFor(() => expect(fetchRooms).toHaveBeenCalled())
    const before = fetchRooms.mock.calls.length

    await act(async () => deliver([{ seq: 2, type: 'message', nick: 'ana', body: 'oi', time: '2026-08-06T00:00:03Z' }]))
    await waitFor(() => expect(fetchRooms.mock.calls.length).toBeGreaterThan(before))
    expect(readRoomMessages.mock.calls.filter(([, , follow]) => follow).length).toBe(2)
  })

  // T-0021 crit. 3
  // E-0006 crit. 9
  it('em sala aberta, relê a presença a cada 10 s; em sala fechada, não', async () => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] })
    readRoomMessages.mockImplementationOnce(() => Promise.resolve([open]))
    fetchRooms.mockResolvedValue([room({ active_participants: [{ nick: 'ana', joined_at: '2026-08-06T00:00:01Z' }] })])
    const { unmount } = render(<WarRoomDetail id="R1" />)
    await waitFor(() => expect(within(entryOf('ana')).getByText(/fora de escuta/)).toBeInTheDocument())

    fetchRooms.mockResolvedValue([presence(fiveMinAgo())])
    await act(async () => vi.advanceTimersByTime(10_000))
    await waitFor(() => expect(within(entryOf('ana')).getByText('em escuta')).toBeInTheDocument())
    expect(readRoomMessages.mock.calls.filter(([, , follow]) => follow).length).toBe(1)
    unmount()

    readRoomMessages.mockImplementationOnce(() =>
      Promise.resolve([open, { seq: 2, type: 'close', time: '2026-08-06T00:05:00Z' }]),
    )
    render(<WarRoomDetail id="R1" />)
    await waitFor(() => expect(screen.getByText(/sala fechada/i)).toBeInTheDocument())
    const settled = fetchRooms.mock.calls.length
    await act(async () => vi.advanceTimersByTime(30_000))
    expect(fetchRooms.mock.calls.length).toBe(settled)
  })

  // T-0021 crit. 4
  // E-0006 crit. 13
  it('tem os rótulos de presença também em inglês', async () => {
    localStorage.setItem('cerne-lang', 'en')
    readRoomMessages.mockImplementationOnce(() => Promise.resolve([open]))
    fetchRooms.mockResolvedValue([presence(fiveMinAgo())])
    render(
      <I18nProvider>
        <WarRoomDetail id="R1" />
      </I18nProvider>,
    )

    await waitFor(() => expect(within(entryOf('ana')).getByText('listening')).toBeInTheDocument())
    expect(within(entryOf('bia')).getByText(/^idle/)).toBeInTheDocument()
    expect(within(entryOf('bia')).getByText(/last active 5 minutes ago/)).toBeInTheDocument()
  })
})
