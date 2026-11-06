import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import Settings from './Settings'
import { I18nProvider } from './i18n'
import type { ResolvedSetting, SettingScope, SettingsView } from './api'

// F-0315: a tela de Settings é derivada do registro de F-0314 — nenhuma chave
// é conhecida pela tela; título, descrição, tipo e escopos vêm do payload.

const fetchSettings = vi.fn<() => Promise<SettingsView>>()
const putSetting = vi.fn<(id: string, scope: SettingScope, value: unknown) => Promise<void>>()
const unsetSetting = vi.fn<(id: string, scope: SettingScope) => Promise<void>>()

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  fetchSettings: () => fetchSettings(),
  putSetting: (id: string, scope: SettingScope, value: unknown) => putSetting(id, scope, value),
  unsetSetting: (id: string, scope: SettingScope) => unsetSetting(id, scope),
}))

let liveVersion = 0
vi.mock('./live', () => ({
  useProjects: () => ({ projects: [{ pid: 'p1', name: 'Projeto A' }], activePid: 'p1' }),
  useLiveVersion: () => liveVersion,
}))

const both: SettingScope[] = ['project', 'host']

const key = (over: Partial<ResolvedSetting> & Pick<ResolvedSetting, 'id'>): ResolvedSetting => ({
  title: over.id,
  type: 'bool',
  default: true,
  scopes: both,
  description: '',
  value: true,
  origin: 'default',
  ...over,
})

// As 4 chaves reais (textos iguais aos do registro Go) + duas que a tela
// nunca viu: test.extra (bool) e test.count (tipo sem editor).
function fixture(over: Partial<SettingsView> = {}): SettingsView {
  return {
    files: { project: '/repo/.cernerc', host: '/home/u/.config/cerne/settings.toml' },
    settings: [
      key({
        id: 'channel.human_name',
        title: 'Channel name',
        type: 'string',
        default: '',
        scopes: ['host'],
        description:
          "Your name on the project's permanent channel, on messages sent from the UI. Empty uses your OS user name.",
        value: '',
      }),
      key({
        id: 'graph.enabled',
        title: 'Graph',
        description: 'Shows the Graph tab in the UI. Off hides it.',
        value: false,
        origin: 'project',
        stored: { project: false },
      }),
      key({ id: 'mindmap.enabled', title: 'Mindmap', description: 'Shows the Mindmap tab in the UI. Off hides it.' }),
      key({
        id: 'wiki.enabled',
        title: 'Wiki',
        description:
          'Not wired yet (F-0316): off will stop compiling the wiki on push and counting its debt as pending work. Today it changes nothing.',
      }),
      key({
        id: 'test.extra',
        title: 'Extra toggle',
        default: false,
        description: 'An injected key the screen never heard of.',
        value: true,
        origin: 'host',
        stored: { host: true },
      }),
      key({
        id: 'test.count',
        title: 'Retry count',
        type: 'int',
        default: 3,
        scopes: ['project'],
        description: 'A key whose type has no editor on screen.',
        value: 3,
      }),
    ],
    ...over,
  }
}

// row devolve o <li> da chave pelo título exibido.
const row = (title: string) => screen.getByRole('listitem', { name: title })

beforeEach(() => {
  cleanup()
  localStorage.clear()
  liveVersion = 0
  fetchSettings.mockReset()
  putSetting.mockReset()
  unsetSetting.mockReset()
  fetchSettings.mockResolvedValue(fixture())
  putSetting.mockResolvedValue()
  unsetSetting.mockResolvedValue()
})

describe('Settings — leitura derivada do registro (F-0315)', () => {
  it('c1: cada chave mostra título, descrição, efetivo com a origem, padrão e o indicador de modificado', async () => {
    render(<Settings />)
    const graph = await screen.findByRole('listitem', { name: 'Grafo' })
    expect(within(graph).getByText('Mostra a aba Grafo na UI. Desligado a esconde.')).toBeInTheDocument()
    expect(within(graph).getByText('Efetivo: desligado, de Projeto')).toBeInTheDocument()
    expect(within(graph).getByText('Padrão: ligado')).toBeInTheDocument()
    expect(within(graph).getByText('Modificado')).toBeInTheDocument()

    const mindmap = row('Mapa mental')
    expect(within(mindmap).getByText('Efetivo: ligado (padrão)')).toBeInTheDocument()
    expect(within(mindmap).queryByText('Modificado')).toBeNull()
    // O id fica visível e selecionável, como o "Copy Setting ID" da barra.
    expect(within(mindmap).getByText('mindmap.enabled')).toBeInTheDocument()
  })

  it('c2: chave que a tela não conhece aparece derivada do payload, com o controle do seu tipo', async () => {
    render(<Settings />)
    const extra = await screen.findByRole('listitem', { name: 'Extra toggle' })
    expect(within(extra).getByText('An injected key the screen never heard of.')).toBeInTheDocument()
    const box = within(extra).getByRole('checkbox', { name: 'Extra toggle' })
    expect(box).toBeChecked()
    expect(box).toHaveAccessibleDescription('An injected key the screen never heard of.')
    // Valor de outro escopo não é "modificado" no escopo em edição (Projeto).
    expect(within(extra).queryByText('Modificado')).toBeNull()
    expect(within(extra).getByText('Efetivo: ligado, de Host')).toBeInTheDocument()

    const count = row('Retry count')
    expect(within(count).queryByRole('checkbox')).toBeNull()
    expect(within(count).queryByRole('textbox')).toBeNull()
    expect(within(count).getByText('3')).toBeInTheDocument()
    expect(within(count).getByText(/edite no arquivo/)).toBeInTheDocument()
  })

  it('c3: a busca filtra por título e descrição, sem caixa nem diacrítico', async () => {
    render(<Settings />)
    await screen.findByRole('listitem', { name: 'Grafo' })
    const search = screen.getByRole('searchbox', { name: 'Buscar configurações' })
    expect(screen.getByText('6 configurações')).toBeInTheDocument()

    fireEvent.change(search, { target: { value: 'mapa' } })
    expect(screen.getAllByRole('listitem')).toHaveLength(1)
    expect(row('Mapa mental')).toBeInTheDocument()
    expect(screen.getByText('1 de 6 configurações')).toBeInTheDocument()

    // Só a descrição do canal tem "usuário": sem acento nem caixa, acha mesmo assim.
    fireEvent.change(search, { target: { value: 'USUARIO' } })
    expect(screen.getAllByRole('listitem')).toHaveLength(1)
    expect(row('Nome no canal')).toBeInTheDocument()

    fireEvent.change(search, { target: { value: 'zzz' } })
    expect(screen.queryAllByRole('listitem')).toHaveLength(0)
    expect(screen.getByText('Nenhuma configuração corresponde a “zzz”.')).toBeInTheDocument()
  })

  it('c8: chave de escopo host leva o selo "global do host"; o cabeçalho nomeia o projeto ativo', async () => {
    render(<Settings />)
    const channel = await screen.findByRole('listitem', { name: 'Nome no canal' })
    const badge = within(channel).getByText('global do host')
    expect(badge).toHaveAttribute('title', expect.stringMatching(/todos os projetos/))
    expect(within(row('Grafo')).queryByText('global do host')).toBeNull()
    expect(screen.getByText('Projeto A')).toBeInTheDocument()
  })

  it('c9: falha na 1ª carga mostra o erro acessível com nova tentativa, que carrega a tela', async () => {
    fetchSettings.mockRejectedValueOnce(new Error('boom'))
    render(<Settings />)
    const status = await screen.findByRole('status')
    expect(status).toHaveTextContent('Erro ao carregar as configurações: boom')
    fireEvent.click(within(status).getByRole('button', { name: 'Tentar de novo' }))
    expect(await screen.findByRole('listitem', { name: 'Grafo' })).toBeInTheDocument()
  })

  it('c9: falha numa recarga mantém o conteúdo e avisa', async () => {
    const { rerender } = render(<Settings />)
    await screen.findByRole('listitem', { name: 'Grafo' })
    fetchSettings.mockRejectedValueOnce(new Error('offline'))
    liveVersion = 1
    rerender(<Settings />)
    expect(await screen.findByRole('status')).toHaveTextContent('offline')
    expect(row('Grafo')).toBeInTheDocument()
  })

  it('c9: arquivo de escopo ilegível vira um aviso por arquivo, com nova tentativa', async () => {
    fetchSettings.mockResolvedValue(
      fixture({ errors: [{ scope: 'host', file: '/home/u/.config/cerne/settings.toml', error: 'toml: bare keys…' }] }),
    )
    render(<Settings />)
    const status = await screen.findByRole('status')
    expect(status).toHaveTextContent('/home/u/.config/cerne/settings.toml')
    expect(status).toHaveTextContent('toml: bare keys…')
    expect(row('Grafo')).toBeInTheDocument()
    fireEvent.click(within(status).getByRole('button', { name: 'Tentar de novo' }))
    await waitFor(() => expect(fetchSettings).toHaveBeenCalledTimes(2))
  })

  it('c10: com o I18nProvider (jsdom = en), a tela e os textos do registro saem em inglês', async () => {
    render(
      <I18nProvider>
        <Settings />
      </I18nProvider>,
    )
    expect(await screen.findByRole('heading', { level: 1, name: 'Settings' })).toBeInTheDocument()
    expect(screen.getByRole('searchbox', { name: 'Search settings' })).toBeInTheDocument()
    const graph = row('Graph')
    expect(within(graph).getByText('Shows the Graph tab in the UI. Off hides it.')).toBeInTheDocument()
    expect(within(graph).getByText('Effective: off, from Project')).toBeInTheDocument()
  })
})
