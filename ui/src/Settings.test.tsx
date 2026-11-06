import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import Settings from './Settings'
import { I18nProvider } from './i18n'
import { HTTPError, type ResolvedSetting, type SettingScope, type SettingsView } from './api'

// F-0315: a tela de Settings é derivada do registro de F-0314 — nenhuma chave
// é conhecida pela tela; título, descrição, tipo e escopos vêm do payload.

const fetchSettings = vi.fn<() => Promise<SettingsView>>()
const putSetting = vi.fn<(id: string, scope: SettingScope, value: unknown) => Promise<SettingsView>>()
const unsetSetting = vi.fn<(id: string, scope: SettingScope) => Promise<SettingsView>>()

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  fetchSettings: () => fetchSettings(),
  putSetting: (id: string, scope: SettingScope, value: unknown) => putSetting(id, scope, value),
  unsetSetting: (id: string, scope: SettingScope) => unsetSetting(id, scope),
}))

let liveVersion = 0
// Estável entre renders, como no LiveProvider: só muda de identidade quando a
// lista é relida (o evento SSE "projects").
let mockProjects = [{ pid: 'p1', name: 'Projeto A' }]
vi.mock('./live', () => ({
  useProjects: () => ({ projects: mockProjects, activePid: 'p1' }),
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
  mockProjects = [{ pid: 'p1', name: 'Projeto A' }]
  fetchSettings.mockReset()
  putSetting.mockReset()
  unsetSetting.mockReset()
  fetchSettings.mockResolvedValue(fixture())
  putSetting.mockResolvedValue(fixture())
  unsetSetting.mockResolvedValue(fixture())
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

// with reescreve uma chave da fixture (o resto fica igual).
const withKey = (id: string, over: Partial<ResolvedSetting>) =>
  fixture({ settings: fixture().settings.map((s) => (s.id === id ? { ...s, ...over } : s)) })

const scopeGroup = () => screen.getByRole('group', { name: 'Escopo de edição' })

describe('Settings — edição por escopo (F-0315)', () => {
  it('c4: valor nos dois escopos mostra o vencedor e o seletor troca o escopo editado', async () => {
    fetchSettings.mockResolvedValue(
      withKey('mindmap.enabled', { value: false, origin: 'project', stored: { project: false, host: true } }),
    )
    render(<Settings />)
    const mm = await screen.findByRole('listitem', { name: 'Mapa mental' })
    expect(within(mm).getByText('Efetivo: desligado, de Projeto')).toBeInTheDocument()
    expect(within(mm).getByText('Também modificado em Host')).toBeInTheDocument()
    expect(within(mm).getByRole('checkbox', { name: 'Mapa mental' })).not.toBeChecked()

    const project = within(scopeGroup()).getByRole('button', { name: /Projeto/ })
    const host = within(scopeGroup()).getByRole('button', { name: /Host/ })
    expect(project).toHaveAttribute('aria-pressed', 'true')
    fireEvent.click(host)
    expect(host).toHaveAttribute('aria-pressed', 'true')
    expect(project).toHaveAttribute('aria-pressed', 'false')
    // Em Host, a linha mostra o valor do host e quem o sobrescreve.
    expect(within(row('Mapa mental')).getByRole('checkbox')).toBeChecked()
    expect(within(row('Mapa mental')).getByText('Sobrescrito por Projeto')).toBeInTheDocument()
    expect(within(row('Mapa mental')).getByText('Efetivo: desligado, de Projeto')).toBeInTheDocument()

    // O atalho volta o seletor e entrega o foco ao controle da linha (o botão some).
    fireEvent.click(within(row('Mapa mental')).getByRole('button', { name: 'Editar em Projeto' }))
    expect(project).toHaveAttribute('aria-pressed', 'true')
    expect(within(row('Mapa mental')).queryByRole('button', { name: 'Editar em Projeto' })).toBeNull()
    expect(within(row('Mapa mental')).getByRole('checkbox')).toHaveFocus()
  })

  it('c5: alternar grava no escopo selecionado, com booleano, e o novo efetivo aparece sem reload', async () => {
    render(<Settings />)
    const box = within(await screen.findByRole('listitem', { name: 'Mapa mental' })).getByRole('checkbox')
    putSetting.mockResolvedValue(withKey('mindmap.enabled', { value: false, origin: 'project', stored: { project: false } }))
    fireEvent.click(box)
    await waitFor(() => expect(putSetting).toHaveBeenCalledWith('mindmap.enabled', 'project', false))
    expect(await within(row('Mapa mental')).findByText('Efetivo: desligado, de Projeto')).toBeInTheDocument()
    expect(within(row('Mapa mental')).getByText('Modificado')).toBeInTheDocument()
    // A view vem na resposta da escrita: nada de reler (crítica C5).
    expect(fetchSettings).toHaveBeenCalledTimes(1)
  })

  it('c5: com o seletor em Host, a escrita vai ao host', async () => {
    render(<Settings />)
    await screen.findByRole('listitem', { name: 'Grafo' })
    fireEvent.click(within(scopeGroup()).getByRole('button', { name: /Host/ }))
    fireEvent.click(within(row('Wiki')).getByRole('checkbox'))
    await waitFor(() => expect(putSetting).toHaveBeenCalledWith('wiki.enabled', 'host', false))
  })

  it('c5: chave só-host grava no host mesmo com o seletor em Projeto, pelo submit', async () => {
    render(<Settings />)
    const channel = await screen.findByRole('listitem', { name: 'Nome no canal' })
    expect(within(channel).getByText('Grava em Host (global)')).toBeInTheDocument()
    const input = within(channel).getByRole('textbox', { name: 'Nome no canal' })
    fireEvent.change(input, { target: { value: 'Ana' } })
    fireEvent.click(within(channel).getByRole('button', { name: 'Salvar' }))
    await waitFor(() => expect(putSetting).toHaveBeenCalledWith('channel.human_name', 'host', 'Ana'))
  })

  it('c5: redefinir só existe com valor no escopo, remove dele e devolve o foco ao controle', async () => {
    render(<Settings />)
    const graph = await screen.findByRole('listitem', { name: 'Grafo' })
    expect(within(row('Mapa mental')).queryByRole('button', { name: 'Redefinir neste escopo' })).toBeNull()
    // test.extra só tem valor no host: em Projeto não há o que redefinir.
    expect(within(row('Extra toggle')).queryByRole('button', { name: 'Redefinir neste escopo' })).toBeNull()
    const reset = within(graph).getByRole('button', { name: 'Redefinir neste escopo' })
    unsetSetting.mockResolvedValue(withKey('graph.enabled', { value: true, origin: 'default', stored: undefined }))
    reset.focus()
    fireEvent.click(reset)
    await waitFor(() => expect(unsetSetting).toHaveBeenCalledWith('graph.enabled', 'project'))
    await waitFor(() => expect(within(row('Grafo')).getByRole('checkbox')).toHaveFocus())
    expect(within(row('Grafo')).queryByRole('button', { name: 'Redefinir neste escopo' })).toBeNull()
    expect(within(row('Grafo')).getByText('Efetivo: ligado (padrão)')).toBeInTheDocument()
  })

  it('c8: o escopo Host é marcado como global, com a dica de que vale para todos os projetos', async () => {
    render(<Settings />)
    await screen.findByRole('listitem', { name: 'Grafo' })
    // O selo entra no nome com espaço: "Host global", nunca "Hostglobal".
    const host = within(scopeGroup()).getByRole('button', { name: 'Host global' })
    expect(host).toHaveAttribute('title', expect.stringMatching(/todos os projetos/))
    expect(screen.getByText(/versionado/)).toBeInTheDocument()
    fireEvent.click(host)
    expect(screen.getByText(/vale para todos os projetos/)).toBeInTheDocument()
    expect(within(row('Grafo')).getByText('global do host')).toBeInTheDocument()
  })

  it('c9: escrita recusada mantém a lista, mostra o erro na linha e a nova tentativa reenvia o mesmo PUT', async () => {
    putSetting.mockRejectedValueOnce(new Error('config file is locked by another writer'))
    render(<Settings />)
    fireEvent.click(within(await screen.findByRole('listitem', { name: 'Mapa mental' })).getByRole('checkbox'))
    const status = await within(row('Mapa mental')).findByRole('status')
    expect(status).toHaveTextContent('Não foi possível gravar: config file is locked by another writer')
    expect(row('Grafo')).toBeInTheDocument()
    fireEvent.click(within(status).getByRole('button', { name: 'Tentar de novo' }))
    await waitFor(() => expect(putSetting).toHaveBeenCalledTimes(2))
    expect(putSetting.mock.calls[1]).toEqual(putSetting.mock.calls[0])
    await waitFor(() => expect(within(row('Mapa mental')).queryByRole('status')).toBeNull())
  })

  it('c9: durante a escrita o controle fica aria-disabled (nunca disabled) e a linha aria-busy', async () => {
    let release = () => {}
    putSetting.mockImplementationOnce(() => new Promise<SettingsView>((r) => (release = () => r(fixture()))))
    render(<Settings />)
    const box = within(await screen.findByRole('listitem', { name: 'Mapa mental' })).getByRole('checkbox')
    fireEvent.click(box)
    await waitFor(() => expect(row('Mapa mental')).toHaveAttribute('aria-busy', 'true'))
    expect(box).toHaveAttribute('aria-disabled', 'true')
    expect(box).toBeEnabled()
    fireEvent.click(box) // pressionar de novo durante a escrita não grava nada
    expect(putSetting).toHaveBeenCalledTimes(1)
    release()
    await waitFor(() => expect(row('Mapa mental')).not.toHaveAttribute('aria-busy'))
  })
})

describe('Settings — escape para o arquivo cru (F-0315, barra R-0042)', () => {
  it('mostra o arquivo do escopo selecionado, copiável, e troca com o seletor', async () => {
    const writeText = vi.fn(async () => {})
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
    render(<Settings />)
    await screen.findByRole('listitem', { name: 'Grafo' })
    expect(screen.getByText('Arquivo:')).toBeInTheDocument()
    expect(screen.getByText('/repo/.cernerc')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Copiar bloco de código' }))
    expect(writeText).toHaveBeenCalledWith('/repo/.cernerc')

    fireEvent.click(within(scopeGroup()).getByRole('button', { name: /Host/ }))
    expect(screen.getByText('/home/u/.config/cerne/settings.toml')).toBeInTheDocument()
    expect(screen.queryByText('/repo/.cernerc')).toBeNull()
  })

  it('sem arquivo para o escopo (fora de um projeto), não há bloco', async () => {
    fetchSettings.mockResolvedValue(fixture({ files: { host: '/home/u/.config/cerne/settings.toml' } }))
    render(<Settings />)
    await screen.findByRole('listitem', { name: 'Grafo' })
    expect(screen.queryByText('Arquivo:')).toBeNull()
  })

  it('valor inválido guardado num escopo é dito na linha — é onde o usuário vai ao arquivo', async () => {
    fetchSettings.mockResolvedValue(
      withKey('wiki.enabled', {
        invalid: [{ scope: 'host', file: '/home/u/.config/cerne/settings.toml', raw: '"no"', error: 'want a bool' }],
      }),
    )
    render(<Settings />)
    const wiki = await screen.findByRole('listitem', { name: 'Wiki' })
    expect(within(wiki).getByText('Valor inválido em Host: "no" — ignorado')).toHaveClass('form-error')
  })
})

// deferred devolve uma promessa e as alavancas para resolvê-la/rejeitá-la no teste.
function deferred<T>() {
  let resolve!: (v: T) => void
  let reject!: (e: unknown) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

describe('Settings — escritas simultâneas (F-0315, crítica C1)', () => {
  it('cada linha guarda o próprio ocupado e a própria falha: uma escrita não destrava nem engole a outra', async () => {
    const wiki = deferred<SettingsView>()
    const graph = deferred<SettingsView>()
    putSetting.mockImplementation((id) => (id === 'wiki.enabled' ? wiki.promise : graph.promise))
    render(<Settings />)
    fireEvent.click(within(await screen.findByRole('listitem', { name: 'Wiki' })).getByRole('checkbox'))
    fireEvent.click(within(row('Grafo')).getByRole('checkbox'))
    await waitFor(() => expect(row('Grafo')).toHaveAttribute('aria-busy', 'true'))
    expect(row('Wiki')).toHaveAttribute('aria-busy', 'true')

    graph.resolve(fixture())
    await waitFor(() => expect(row('Grafo')).not.toHaveAttribute('aria-busy'))
    // A Wiki segue em voo: continua ocupada e um novo clique não grava de novo.
    expect(row('Wiki')).toHaveAttribute('aria-busy', 'true')
    fireEvent.click(within(row('Wiki')).getByRole('checkbox'))
    expect(putSetting.mock.calls.filter(([id]) => id === 'wiki.enabled')).toHaveLength(1)

    wiki.reject(new Error('config file is locked by another writer'))
    expect(await within(row('Wiki')).findByRole('status')).toHaveTextContent('config file is locked by another writer')
    expect(row('Wiki')).not.toHaveAttribute('aria-busy')
    expect(within(row('Grafo')).queryByRole('status')).toBeNull()
  })
})

describe('Settings — falha de escrita por chave e escopo (F-0315, crítica C6)', () => {
  it('o erro de Projeto não aparece em Host, e a nova tentativa reenvia ao escopo que falhou', async () => {
    fetchSettings.mockResolvedValue(
      withKey('mindmap.enabled', { value: true, origin: 'project', stored: { project: true, host: false } }),
    )
    putSetting.mockRejectedValueOnce(new Error('config file is locked by another writer'))
    render(<Settings />)
    fireEvent.click(within(await screen.findByRole('listitem', { name: 'Mapa mental' })).getByRole('checkbox'))
    expect(await within(row('Mapa mental')).findByRole('status')).toHaveTextContent('locked')
    expect(putSetting).toHaveBeenLastCalledWith('mindmap.enabled', 'project', false)

    fireEvent.click(within(row('Mapa mental')).getByRole('button', { name: 'Editar em Host' }))
    expect(within(scopeGroup()).getByRole('button', { name: 'Host global' })).toHaveAttribute('aria-pressed', 'true')
    expect(within(row('Mapa mental')).queryByRole('status')).toBeNull()

    fireEvent.click(within(row('Mapa mental')).getByRole('button', { name: 'Editar em Projeto' }))
    const status = within(row('Mapa mental')).getByRole('status')
    fireEvent.click(within(status).getByRole('button', { name: 'Tentar de novo' }))
    await waitFor(() => expect(putSetting).toHaveBeenCalledTimes(2))
    expect(putSetting).toHaveBeenLastCalledWith('mindmap.enabled', 'project', false)
  })
})

describe('Settings — a escrita aplica a view da própria resposta (F-0315, crítica C5)', () => {
  it('escrita que gravou mostra o valor novo mesmo se uma releitura falharia', async () => {
    render(<Settings />)
    const wiki = await screen.findByRole('listitem', { name: 'Wiki' })
    fetchSettings.mockRejectedValue(new Error('offline'))
    putSetting.mockResolvedValue(withKey('wiki.enabled', { value: false, origin: 'project', stored: { project: false } }))
    fireEvent.click(within(wiki).getByRole('checkbox'))
    expect(await within(row('Wiki')).findByText('Efetivo: desligado, de Projeto')).toBeInTheDocument()
    expect(within(row('Wiki')).getByRole('checkbox')).not.toBeChecked()
    expect(screen.queryByRole('status')).toBeNull()
  })
})

describe('Settings — escuta o evento projects (F-0315, crítica C3)', () => {
  it('a lista de projetos relida (escrita em outra aba) relê a tela aberta', async () => {
    const { rerender } = render(<Settings />)
    await screen.findByRole('listitem', { name: 'Mapa mental' })
    rerender(<Settings />)
    expect(fetchSettings).toHaveBeenCalledTimes(1)

    fetchSettings.mockResolvedValue(withKey('mindmap.enabled', { value: false, origin: 'host', stored: { host: false } }))
    mockProjects = [{ pid: 'p1', name: 'Projeto A' }]
    rerender(<Settings />)
    expect(await within(row('Mapa mental')).findByText('Efetivo: desligado, de Host')).toBeInTheDocument()
    expect(fetchSettings).toHaveBeenCalledTimes(2)
  })
})

describe('Settings — texto vazio é redefinir (F-0315, crítica C7)', () => {
  const ana = () => withKey('channel.human_name', { value: 'Ana', origin: 'host', stored: { host: 'Ana' } })

  it('salvar vazio ou só espaços remove o valor do escopo (DELETE), como diz "Vazio usa o nome do sistema"', async () => {
    fetchSettings.mockResolvedValue(ana())
    unsetSetting.mockResolvedValue(fixture())
    render(<Settings />)
    const channel = await screen.findByRole('listitem', { name: 'Nome no canal' })
    const input = within(channel).getByRole('textbox', { name: 'Nome no canal' })
    expect(input).toHaveValue('Ana')
    fireEvent.change(input, { target: { value: '   ' } })
    fireEvent.click(within(channel).getByRole('button', { name: 'Salvar' }))
    await waitFor(() => expect(unsetSetting).toHaveBeenCalledWith('channel.human_name', 'host'))
    expect(putSetting).not.toHaveBeenCalled()
    await waitFor(() => expect(within(row('Nome no canal')).getByText('Efetivo: (vazio) (padrão)')).toBeInTheDocument())
    expect(within(row('Nome no canal')).getByRole('textbox')).toHaveValue('')
  })

  it('o valor sai aparado, o campo limita 64 caracteres e mostra o valor gravado depois de salvar', async () => {
    fetchSettings.mockResolvedValue(ana())
    putSetting.mockResolvedValue(ana())
    render(<Settings />)
    const input = within(await screen.findByRole('listitem', { name: 'Nome no canal' })).getByRole('textbox')
    expect(input).toHaveAttribute('maxLength', '64')
    fireEvent.change(input, { target: { value: '  Ana  ' } })
    fireEvent.click(within(row('Nome no canal')).getByRole('button', { name: 'Salvar' }))
    await waitFor(() => expect(putSetting).toHaveBeenCalledWith('channel.human_name', 'host', 'Ana'))
    await waitFor(() => expect(within(row('Nome no canal')).getByRole('textbox')).toHaveValue('Ana'))
  })
})

describe('Settings — nova tentativa só quando tentar de novo ajuda (F-0315, crítica C8)', () => {
  it('400 e 422 mostram a mensagem e o arquivo cru, sem "Tentar de novo"', async () => {
    for (const status of [400, 422]) {
      cleanup()
      putSetting.mockRejectedValueOnce(new HTTPError('refused', status))
      render(<Settings />)
      fireEvent.click(within(await screen.findByRole('listitem', { name: 'Grafo' })).getByRole('checkbox'))
      const status_ = await within(row('Grafo')).findByRole('status')
      expect(within(status_).queryByRole('button', { name: 'Tentar de novo' })).toBeNull()
      expect(within(status_).getByText('/repo/.cernerc')).toBeInTheDocument()
    }
  })

  it('409 (travado), 5xx e falha de rede oferecem "Tentar de novo"', async () => {
    for (const err of [new HTTPError('busy', 409), new HTTPError('boom', 503), new TypeError('Failed to fetch')]) {
      cleanup()
      putSetting.mockRejectedValueOnce(err)
      render(<Settings />)
      fireEvent.click(within(await screen.findByRole('listitem', { name: 'Grafo' })).getByRole('checkbox'))
      const status = await within(row('Grafo')).findByRole('status')
      expect(within(status).getByRole('button', { name: 'Tentar de novo' })).toBeInTheDocument()
    }
  })
})
