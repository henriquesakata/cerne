import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import FeatureAgents from './FeatureAgents'
import { I18nProvider } from './i18n'
import type { AgentSession, ChannelMessage, ChannelSnapshot, FeatureAgents as Agents } from './api'

const fetchFeatureAgents = vi.fn<(id: string) => Promise<Agents>>()
const readChannel = vi.fn<(follow: boolean, version: number, signal?: AbortSignal, ref?: string) => Promise<ChannelSnapshot>>()
const postChannel = vi.fn<(body: string, ref?: string, to?: string[]) => Promise<ChannelMessage>>()

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  fetchFeatureAgents: (id: string) => fetchFeatureAgents(id),
  readChannel: (follow: boolean, version: number, signal?: AbortSignal, ref?: string) => readChannel(follow, version, signal, ref),
  postChannel: (body: string, ref?: string, to?: string[]) => postChannel(body, ref, to),
}))
vi.mock('./live', () => ({ useLiveVersion: () => 0 }))

const tl: AgentSession = {
  session: '3af179af-b53e-478f-b253-616991b06680',
  harness: 'claude-code',
  roles: [
    { kind: 'opened', at: '2026-11-20T10:00:00Z' },
    { kind: 'spec', at: '2026-11-20T10:00:00Z' },
  ],
  info: { title: 'TL do lote', cwd: '/w/tl', branch: 'claude/tl', last: '2026-11-20T12:00:00Z', found: true },
}
const dev: AgentSession = {
  session: 'b3847c8c-b097-44cd-b804-835dc8e1ae4d',
  harness: 'claude-code',
  roles: [
    { kind: 'plan', at: '2026-11-20T10:30:00Z' },
    { kind: 'implemented', agent: 'a1', agent_type: 'dev', at: '2026-11-20T11:00:00Z' },
    { kind: 'result', at: '2026-11-20T11:30:00Z' },
  ],
  info: { title: 'HO-0008', cwd: '/w/ho', branch: 'feat/x', pr: 'https://github.com/o/r/pull/7', found: true },
}
const codex: AgentSession = {
  session: 'c0dexc0d-0000-0000-0000-000000000000',
  harness: 'other',
  roles: [{ kind: 'work', work: 'W-0010', work_title: 'revisão', at: '2026-11-20T11:40:00Z' }],
  info: { found: false },
}

let thread: ChannelSnapshot = { messages: [], version: 1 }

beforeEach(() => {
  cleanup()
  localStorage.setItem('cerne-lang', 'pt-BR')
  fetchFeatureAgents.mockReset()
  readChannel.mockReset()
  postChannel.mockReset()
  thread = { messages: [], version: 1 }
  readChannel.mockImplementation((follow) => (follow ? new Promise(() => {}) : Promise.resolve(thread)))
})

const renderPanel = () =>
  render(
    <I18nProvider>
      <FeatureAgents id="F-0486" />
    </I18nProvider>,
  )

describe('FeatureAgents (F-0486)', () => {
  it('lista cada sessão com o que fez, onde está e o comando de retomar (critérios 2, 3 e 4)', async () => {
    fetchFeatureAgents.mockResolvedValue({ sessions: [tl, dev, codex] })
    renderPanel()
    await waitFor(() => expect(screen.getByText('TL do lote')).toBeInTheDocument())
    expect(fetchFeatureAgents).toHaveBeenCalledWith('F-0486')
    expect(screen.getByText('abriu · escreveu a spec')).toBeInTheDocument()
    expect(screen.getByText('escreveu o plano · implementou (subagente dev) · escreveu o result')).toBeInTheDocument()
    expect(screen.getByText('/w/ho')).toBeInTheDocument()
    expect(screen.getByText('feat/x')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'https://github.com/o/r/pull/7' })).toBeInTheDocument()
    expect(screen.getByText(`cd "/w/tl" && claude --resume ${tl.session}`)).toBeInTheDocument()
    expect(screen.getByText(/trabalho W-0010 — revisão/)).toBeInTheDocument()
    expect(screen.getByText(/transcript não encontrado nesta máquina/)).toBeInTheDocument()
    expect(screen.getByText('Sessão c0dexc0d (sem título)')).toBeInTheDocument()
    expect(screen.queryByText(/claude --resume c0dex/)).toBeNull() // not a Claude Code session
  })

  it('sem sessão: diz isso e não deixa enviar, com o motivo (critério 6)', async () => {
    fetchFeatureAgents.mockResolvedValue({ sessions: [] })
    renderPanel()
    await waitFor(() => expect(screen.getByText(/nenhuma sessão desta máquina trabalhou neste card/i)).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: /mandar mensagem aos agentes/i }))
    expect(await screen.findByText(/não há sessão para receber/i)).toBeInTheDocument()
    expect(screen.getByPlaceholderText(/sessões marcadas/i)).toBeDisabled()
  })

  it('sob o cerne serve (rota ausente) diz que o painel é só do cerne ui (critério 14)', async () => {
    fetchFeatureAgents.mockResolvedValue({ unavailable: true })
    renderPanel()
    expect(await screen.findByText(/só existe no cerne ui local/i)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /mandar mensagem/i })).toBeNull()
  })

  it('o chat abre fechado e manda só para as sessões marcadas (critério 7)', async () => {
    fetchFeatureAgents.mockResolvedValue({ sessions: [tl, dev] })
    postChannel.mockResolvedValue({ seq: 9, type: 'message', nick: 'Henrique', body: 'x', time: '', state: 'not_delivered' })
    renderPanel()
    await waitFor(() => expect(screen.getByText('HO-0008')).toBeInTheDocument())
    expect(screen.queryByPlaceholderText(/sessões marcadas/i)).toBeNull()
    const toggle = screen.getByRole('button', { name: /mandar mensagem aos agentes/i })
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    fireEvent.click(toggle)
    await waitFor(() => expect(readChannel).toHaveBeenCalledWith(false, 0, undefined, 'F-0486'))
    fireEvent.click(screen.getByRole('checkbox', { name: /mandar para TL do lote/i }))
    fireEvent.change(screen.getByPlaceholderText(/sessões marcadas/i), { target: { value: 'corrige o teste?' } })
    fireEvent.click(screen.getByRole('button', { name: /^enviar$/i }))
    await waitFor(() => expect(postChannel).toHaveBeenCalledWith('corrige o teste?', 'F-0486', [dev.session]))
    fireEvent.click(screen.getByRole('checkbox', { name: /mandar para HO-0008/i }))
    expect(await screen.findByText(/marque ao menos uma sessão/i)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /fechar a conversa/i }))
    expect(screen.queryByPlaceholderText(/sessões marcadas/i)).toBeNull()
  })

  it('fio: estado por sessão pelo título e, sem entrega, onde abrir a sessão (critérios 11 e 12)', async () => {
    fetchFeatureAgents.mockResolvedValue({ sessions: [tl, dev] })
    thread = {
      version: 3,
      messages: [
        {
          seq: 4, type: 'message', nick: 'Henrique', human: true, body: 'quem escreveu o plano?', time: '', state: 'not_delivered',
          directed: true, ref: 'F-0486',
          targets: [
            { work: 'F-0486·3af179af', session: '3af179af', state: 'delivered' },
            { work: 'F-0486·b3847c8c', session: 'b3847c8c', state: 'not_delivered' },
          ],
        },
        { seq: 5, type: 'message', nick: 'F-0486·3af179af', body: 'foi o HO-0008', time: '', state: 'delivered', ref: 'F-0486' },
      ],
    }
    renderPanel()
    await waitFor(() => expect(screen.getByText('HO-0008')).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: /mandar mensagem aos agentes/i }))
    expect(await screen.findByText('quem escreveu o plano?')).toBeInTheDocument()
    expect(screen.getByText('TL do lote recebeu — sem resposta')).toBeInTheDocument()
    expect(screen.getByText('HO-0008 ainda não recebeu')).toBeInTheDocument()
    const hint = screen.getByText(/HO-0008 só lê a mensagem quando rodar um comando/).closest('.agents-unreached') as HTMLElement
    expect(within(hint).getByText(`cd "/w/ho" && claude --resume ${dev.session}`)).toBeInTheDocument()
    expect(screen.getByText('foi o HO-0008')).toBeInTheDocument()
  })

  it('fala inglês quando o leitor escolheu inglês (critério 15)', async () => {
    localStorage.setItem('cerne-lang', 'en')
    fetchFeatureAgents.mockResolvedValue({ sessions: [tl] })
    renderPanel()
    expect(await screen.findByText('opened it · wrote the spec')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Agents' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /message the agents/i })).toBeInTheDocument()
  })
})
