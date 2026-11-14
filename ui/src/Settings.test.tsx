import { readFileSync } from 'node:fs'
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
const renameProject = vi.fn<(name: string) => Promise<string>>()

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  fetchSettings: () => fetchSettings(),
  putSetting: (id: string, scope: SettingScope, value: unknown) => putSetting(id, scope, value),
  unsetSetting: (id: string, scope: SettingScope) => unsetSetting(id, scope),
  renameProject: (name: string) => renameProject(name),
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
        title: 'Your name in the channel',
        type: 'string',
        default: '',
        scopes: ['host'],
        description:
          "The name that signs the messages you send from the UI on a project's permanent channel. It applies to every project on this machine. It is not the project's name, which is set in the “Project name” field of the Settings screen. Empty uses your OS user name.",
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
  renameProject.mockReset()
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
    expect(row('Seu nome no canal')).toBeInTheDocument()

    fireEvent.change(search, { target: { value: 'zzz' } })
    expect(screen.queryAllByRole('listitem')).toHaveLength(0)
    expect(screen.getByText('Nenhuma configuração corresponde a “zzz”.')).toBeInTheDocument()
  })

  it('c8: chave de escopo host leva o selo "global do host"; o cabeçalho nomeia o projeto ativo', async () => {
    render(<Settings />)
    const channel = await screen.findByRole('listitem', { name: 'Seu nome no canal' })
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
    const channel = await screen.findByRole('listitem', { name: 'Seu nome no canal' })
    expect(within(channel).getByText('Grava em Host (global)')).toBeInTheDocument()
    const input = within(channel).getByRole('textbox', { name: 'Seu nome no canal' })
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

// topFile: o "Arquivo:" do topo (escopo selecionado), fora das linhas — uma
// linha que grava em outro escopo mostra o seu próprio.
const topFile = () => screen.queryAllByText('Arquivo:').find((e) => e.closest('li') === null)

describe('Settings — escape para o arquivo cru (F-0315, barra R-0042)', () => {
  it('mostra o arquivo do escopo selecionado, copiável, e troca com o seletor', async () => {
    const writeText = vi.fn(async () => {})
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
    render(<Settings />)
    await screen.findByRole('listitem', { name: 'Grafo' })
    expect(topFile()).toBeInTheDocument()
    expect(screen.getByText('/repo/.cernerc')).toBeInTheDocument()
    fireEvent.click(within(topFile()!.parentElement!).getByRole('button', { name: 'Copiar bloco de código' }))
    expect(writeText).toHaveBeenCalledWith('/repo/.cernerc')

    fireEvent.click(within(scopeGroup()).getByRole('button', { name: /Host/ }))
    const top = topFile()!.parentElement!
    expect(within(top).getByText('/home/u/.config/cerne/settings.toml')).toBeInTheDocument()
    expect(within(top).queryByText('/repo/.cernerc')).toBeNull()
    // test.count só aceita Projeto: em Host, a linha mostra o arquivo em que ela vive.
    expect(within(row('Retry count')).getByText('/repo/.cernerc')).toBeInTheDocument()
  })

  it('sem arquivo para o escopo (fora de um projeto), não há bloco', async () => {
    fetchSettings.mockResolvedValue(fixture({ files: { host: '/home/u/.config/cerne/settings.toml' } }))
    render(<Settings />)
    await screen.findByRole('listitem', { name: 'Grafo' })
    expect(topFile()).toBeUndefined()
  })

  it('valor inválido guardado num escopo é dito na linha — é onde o usuário vai ao arquivo', async () => {
    fetchSettings.mockResolvedValue(
      withKey('wiki.enabled', {
        invalid: [{ scope: 'host', file: '/home/u/.config/cerne/settings.toml', raw: '"no"', error: 'want a bool' }],
      }),
    )
    render(<Settings />)
    const wiki = await screen.findByRole('listitem', { name: 'Wiki' })
    expect(
      within(wiki).getByText('Valor inválido em Host (/home/u/.config/cerne/settings.toml): "no" — ignorado.'),
    ).toHaveClass('form-error')
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
    const channel = await screen.findByRole('listitem', { name: 'Seu nome no canal' })
    const input = within(channel).getByRole('textbox', { name: 'Seu nome no canal' })
    expect(input).toHaveValue('Ana')
    fireEvent.change(input, { target: { value: '   ' } })
    fireEvent.click(within(channel).getByRole('button', { name: 'Salvar' }))
    await waitFor(() => expect(unsetSetting).toHaveBeenCalledWith('channel.human_name', 'host'))
    expect(putSetting).not.toHaveBeenCalled()
    await waitFor(() => expect(within(row('Seu nome no canal')).getByText('Efetivo: (vazio) (padrão)')).toBeInTheDocument())
    expect(within(row('Seu nome no canal')).getByRole('textbox')).toHaveValue('')
  })

  it('o valor sai aparado, o campo limita 64 caracteres e mostra o valor gravado depois de salvar', async () => {
    fetchSettings.mockResolvedValue(ana())
    putSetting.mockResolvedValue(ana())
    render(<Settings />)
    const input = within(await screen.findByRole('listitem', { name: 'Seu nome no canal' })).getByRole('textbox')
    expect(input).toHaveAttribute('maxLength', '64')
    fireEvent.change(input, { target: { value: '  Ana  ' } })
    fireEvent.click(within(row('Seu nome no canal')).getByRole('button', { name: 'Salvar' }))
    await waitFor(() => expect(putSetting).toHaveBeenCalledWith('channel.human_name', 'host', 'Ana'))
    await waitFor(() => expect(within(row('Seu nome no canal')).getByRole('textbox')).toHaveValue('Ana'))
  })
})

describe('Settings — o nome que o canal usa quando vazio (F-0333 crit. 3)', () => {
  const channelRow = () => screen.findByRole('listitem', { name: 'Seu nome no canal' })
  const ana = { value: 'Ana', origin: 'host' as const, stored: { host: 'Ana' }, fallback: 'ana' }

  it('com fallback do servidor e valor vazio, o campo mostra o fallback como placeholder, sem valor', async () => {
    fetchSettings.mockResolvedValue(withKey('channel.human_name', { fallback: 'ana' }))
    render(<Settings />)
    const input = within(await channelRow()).getByRole('textbox')
    expect(input).toHaveAttribute('placeholder', 'ana')
    expect(input).toHaveValue('')
  })

  // Crítica C3: o nome usado sai em texto na linha de efetivo, não só no
  // placeholder, e a linha não diz mais "(vazio)" quando o canal assina "ana".
  it('com fallback e valor vazio, a linha de efetivo diz o nome usado', async () => {
    fetchSettings.mockResolvedValue(withKey('channel.human_name', { fallback: 'ana' }))
    render(<Settings />)
    const channel = await channelRow()
    expect(within(channel).getByText('Efetivo: ana (seu usuário do sistema)')).toBeInTheDocument()
    expect(within(channel).queryByText('Efetivo: (vazio) (padrão)')).toBeNull()
  })

  // Crítica C6: um valor só de espaços (arquivo editado à mão) também é vazio.
  it('valor só com espaços: a linha de efetivo diz o fallback', async () => {
    fetchSettings.mockResolvedValue(
      withKey('channel.human_name', { value: '   ', origin: 'host', stored: { host: '   ' }, fallback: 'ana' }),
    )
    render(<Settings />)
    expect(within(await channelRow()).getByText('Efetivo: ana (seu usuário do sistema)')).toBeInTheDocument()
  })

  // Crítica C4: com um nome gravado, o placeholder já está lá para quando o
  // campo for apagado; a linha de efetivo diz o nome gravado.
  it('com nome gravado, o placeholder é o fallback e o efetivo é o nome', async () => {
    fetchSettings.mockResolvedValue(withKey('channel.human_name', ana))
    render(<Settings />)
    const channel = await channelRow()
    expect(within(channel).getByRole('textbox')).toHaveAttribute('placeholder', 'ana')
    expect(within(channel).getByText('Efetivo: Ana, de Host')).toBeInTheDocument()
  })

  // Crítica C11: depois de redefinir, a resposta do DELETE traz o fallback, e
  // o campo vazio volta a mostrá-lo.
  it('depois de redefinir, o campo vazio mostra o fallback', async () => {
    fetchSettings.mockResolvedValue(withKey('channel.human_name', ana))
    unsetSetting.mockResolvedValue(withKey('channel.human_name', { fallback: 'ana' }))
    render(<Settings />)
    const channel = await channelRow()
    fireEvent.change(within(channel).getByRole('textbox'), { target: { value: '' } })
    fireEvent.click(within(channel).getByRole('button', { name: 'Salvar' }))
    await waitFor(() => expect(unsetSetting).toHaveBeenCalledWith('channel.human_name', 'host'))
    await waitFor(() => expect(within(row('Seu nome no canal')).getByText('Efetivo: ana (seu usuário do sistema)')).toBeInTheDocument())
    const input = within(row('Seu nome no canal')).getByRole('textbox')
    expect(input).toHaveValue('')
    expect(input).toHaveAttribute('placeholder', 'ana')
  })

  it('sem fallback, o campo não tem placeholder', async () => {
    render(<Settings />)
    const input = within(await screen.findByRole('listitem', { name: 'Seu nome no canal' })).getByRole('textbox')
    expect(input).not.toHaveAttribute('placeholder')
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

describe('Settings — recusas conhecidas do servidor em pt-BR/en (F-0315, crítica C17)', () => {
  const failWith = async (err: Error) => {
    cleanup()
    putSetting.mockRejectedValueOnce(err)
    render(<Settings />)
    fireEvent.click(within(await screen.findByRole('listitem', { name: 'Grafo' })).getByRole('checkbox'))
    return within(row('Grafo')).findByRole('status')
  }

  it('409 travado, 422 e 400 viram mensagens traduzidas; erro desconhecido mantém o texto cru', async () => {
    const locked = await failWith(
      new HTTPError('/repo/.cernerc: config file is locked by another writer; if no cerne is running, remove /repo/.cernerc.lock', 409),
    )
    expect(locked).toHaveTextContent('O arquivo está travado por outra escrita')
    expect(locked).toHaveTextContent('/repo/.cernerc.lock')
    expect(locked).not.toHaveTextContent('locked by another writer')

    expect(await failWith(new HTTPError('cannot safely edit [graph] enabled; edit the file by hand', 422))).toHaveTextContent(
      'Não é possível editar este valor com segurança no arquivo',
    )
    expect(await failWith(new HTTPError('invalid setting value "x"', 400))).toHaveTextContent(
      'O servidor recusou o valor ou o escopo',
    )
    expect(await failWith(new HTTPError('boom', 500))).toHaveTextContent('Não foi possível gravar: boom')
  })

  it('em inglês, a mesma recusa sai no dicionário en', async () => {
    putSetting.mockRejectedValueOnce(new HTTPError('config file is locked by another writer', 409))
    render(
      <I18nProvider>
        <Settings />
      </I18nProvider>,
    )
    fireEvent.click(within(await screen.findByRole('listitem', { name: 'Graph' })).getByRole('checkbox'))
    expect(await within(row('Graph')).findByRole('status')).toHaveTextContent('The file is locked by another writer')
  })
})

describe('Settings — valor inválido visível e removível (F-0315, crítica C9)', () => {
  it('o problema diz escopo, arquivo e valor em texto, e "Remover valor inválido" apaga daquele escopo', async () => {
    fetchSettings.mockResolvedValue(
      withKey('wiki.enabled', { invalid: [{ scope: 'project', file: '/repo/.cernerc', raw: '"yes"', error: 'want a bool' }] }),
    )
    render(<Settings />)
    const wiki = await screen.findByRole('listitem', { name: 'Wiki' })
    const problem = within(wiki).getByText(/Valor inválido em Projeto/)
    expect(problem).toHaveTextContent('/repo/.cernerc')
    expect(problem).toHaveTextContent('"yes"')
    expect(problem).not.toHaveAttribute('title')
    fireEvent.click(within(wiki).getByRole('button', { name: 'Remover valor inválido' }))
    await waitFor(() => expect(unsetSetting).toHaveBeenCalledWith('wiki.enabled', 'project'))
    await waitFor(() => expect(within(row('Wiki')).queryByText(/Valor inválido/)).toBeNull())
  })

  it('valor inválido de outro escopo leva a ele, onde a remoção age', async () => {
    fetchSettings.mockResolvedValue(
      withKey('wiki.enabled', {
        invalid: [{ scope: 'host', file: '/home/u/.config/cerne/settings.toml', raw: '"no"', error: 'want a bool' }],
      }),
    )
    render(<Settings />)
    const wiki = await screen.findByRole('listitem', { name: 'Wiki' })
    expect(within(wiki).queryByRole('button', { name: 'Remover valor inválido' })).toBeNull()
    fireEvent.click(within(wiki).getByRole('button', { name: 'Editar em Host' }))
    fireEvent.click(within(row('Wiki')).getByRole('button', { name: 'Remover valor inválido' }))
    await waitFor(() => expect(unsetSetting).toHaveBeenCalledWith('wiki.enabled', 'host'))
  })
})

describe('Settings — a linha diz o escopo em que age (F-0315, críticas C10 e C18)', () => {
  it('chave só-host com Projeto selecionado: o redefinir nomeia Host (global) e o arquivo é o do host', async () => {
    fetchSettings.mockResolvedValue(withKey('channel.human_name', { value: 'Ana', origin: 'host', stored: { host: 'Ana' } }))
    render(<Settings />)
    const channel = await screen.findByRole('listitem', { name: 'Seu nome no canal' })
    expect(within(scopeGroup()).getByRole('button', { name: 'Projeto' })).toHaveAttribute('aria-pressed', 'true')
    expect(within(channel).queryByRole('button', { name: 'Redefinir neste escopo' })).toBeNull()
    expect(within(channel).getByRole('button', { name: 'Redefinir em Host (global)' })).toBeInTheDocument()
    expect(within(channel).getByText('Arquivo:')).toBeInTheDocument()
    expect(within(channel).getByText('/home/u/.config/cerne/settings.toml')).toBeInTheDocument()
    // Linhas que gravam no escopo selecionado não repetem o arquivo do topo.
    expect(within(row('Grafo')).queryByText('Arquivo:')).toBeNull()
  })
})

describe('Settings — chave só-host grava e redefine no host (F-0315, crítica C11)', () => {
  const hostOnly = () =>
    fixture({
      settings: [
        ...fixture().settings,
        key({ id: 'test.hostflag', title: 'Host flag', scopes: ['host'], value: true, origin: 'host', stored: { host: true } }),
      ],
    })

  it('com Projeto selecionado, o checkbox de uma chave só-host grava no host', async () => {
    fetchSettings.mockResolvedValue(hostOnly())
    render(<Settings />)
    fireEvent.click(within(await screen.findByRole('listitem', { name: 'Host flag' })).getByRole('checkbox'))
    await waitFor(() => expect(putSetting).toHaveBeenCalledWith('test.hostflag', 'host', false))
  })

  it('com Projeto selecionado, o redefinir de uma chave só-host remove do host', async () => {
    fetchSettings.mockResolvedValue(hostOnly())
    render(<Settings />)
    const flag = await screen.findByRole('listitem', { name: 'Host flag' })
    fireEvent.click(within(flag).getByRole('button', { name: 'Redefinir em Host (global)' }))
    await waitFor(() => expect(unsetSetting).toHaveBeenCalledWith('test.hostflag', 'host'))
  })
})

// Contraste WCAG (crítica C13): o texto pequeno da tela — Efetivo/Padrão, a dica
// do escopo, "Arquivo:" — usa um token com 4.5:1 sobre o fundo da página e o da
// linha em foco, nos dois temas; nunca a classe .muted (--faint, 3.08:1 no claro).
describe('Settings — contraste do texto secundário (F-0315, crítica C13)', () => {
  const css = readFileSync('src/app.css', 'utf8')
  const tokens = (block: string) =>
    Object.fromEntries([...block.matchAll(/--([\w-]+):\s*(#[0-9a-f]{3,6})/gi)].map((m) => [m[1], m[2]]))
  const light = tokens(/:root \{[^}]*\}/.exec(css)![0])
  const dark = tokens(/\[data-theme='dark'\] \{[^}]*\}/.exec(css)![0])
  const lum = (hex: string) => {
    const h = hex.length === 4 ? hex.replace(/\w/g, (c) => c + c).slice(1) : hex.slice(1)
    const [r, g, b] = [0, 2, 4].map((i) => {
      const c = parseInt(h.slice(i, i + 2), 16) / 255
      return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4
    })
    return 0.2126 * r + 0.7152 * g + 0.0722 * b
  }
  const contrast = (a: string, b: string) => {
    const [hi, lo] = [lum(a), lum(b)].sort((x, y) => y - x)
    return (hi + 0.05) / (lo + 0.05)
  }

  it.each(['.settings-meta', '.settings-hint', '.settings-file > span'])('%s alcança 4.5:1 nos dois temas', (sel) => {
    const rule = new RegExp(`${sel.replace(/[.>]/g, (c) => `\\${c}`)} \\{([^}]*)\\}`).exec(css)?.[1] ?? ''
    const token = /color: var\(--([\w-]+)\)/.exec(rule)?.[1]
    expect(token, `${sel} has no color token`).toBeDefined()
    for (const theme of [light, { ...light, ...dark }]) {
      for (const bg of ['bg', 'surface-2']) {
        expect(contrast(theme[token!], theme[bg])).toBeGreaterThanOrEqual(4.5)
      }
    }
  })

  it('nenhum desses textos usa a classe .muted', async () => {
    render(<Settings />)
    await screen.findByRole('listitem', { name: 'Grafo' })
    const nodes = [
      within(row('Grafo')).getByText('Padrão: ligado').parentElement!,
      screen.getByText(/versionado/),
      topFile()!,
    ]
    for (const n of nodes) expect(n).not.toHaveClass('muted')
  })
})

describe('Settings — cópia que falha avisa e seleciona o caminho (F-0315, crítica C14)', () => {
  it.each([
    ['sem navigator.clipboard', undefined],
    ['com a escrita recusada', { writeText: vi.fn(async () => Promise.reject(new Error('denied'))) }],
  ])('%s', async (_, clipboard) => {
    Object.defineProperty(navigator, 'clipboard', { value: clipboard, configurable: true })
    render(<Settings />)
    await screen.findByRole('listitem', { name: 'Grafo' })
    const top = topFile()!.parentElement!
    fireEvent.click(within(top).getByRole('button', { name: 'Copiar bloco de código' }))
    expect(await within(top).findByText('Não foi possível copiar — selecione o caminho')).toBeInTheDocument()
    expect(window.getSelection()?.toString()).toBe('/repo/.cernerc')
  })
})

describe('Settings — nome do projeto (F-0331)', () => {
  const region = async () => {
    render(<Settings />)
    return screen.findByRole('region', { name: 'Nome do projeto' })
  }
  const submit = (r: HTMLElement, value: string) => {
    fireEvent.change(within(r).getByRole('textbox', { name: 'Nome do projeto' }), { target: { value } })
    fireEvent.click(within(r).getByRole('button', { name: 'Salvar' }))
  }

  it('mostra o nome atual, o destino e grava o nome sem os espaços das pontas', async () => {
    renameProject.mockResolvedValue('Núcleo')
    const r = await region()
    expect(within(r).getByRole('textbox', { name: 'Nome do projeto' })).toHaveValue('Projeto A')
    expect(within(r).getByText('Grava no backend do projeto: vale para todo clone.')).toBeInTheDocument()
    submit(r, '  Núcleo ')
    await waitFor(() => expect(renameProject).toHaveBeenCalledWith('Núcleo'))
  })

  it.each([
    ['   ', 'O nome do projeto não pode ficar vazio.'],
    ['é'.repeat(65), 'O nome do projeto tem no máximo 64 caracteres.'],
    ['a\tb', 'O nome do projeto não pode ter quebra de linha nem caractere de controle.'],
  ])('crit. 4: %j é recusado na tela, com a regra, sem pedido', async (value, msg) => {
    const r = await region()
    submit(r, value)
    expect(within(r).getByRole('alert')).toHaveTextContent(msg)
    expect(renameProject).not.toHaveBeenCalled()
  })

  it.each([
    [new HTTPError('could not acquire backend lock: /repo/.cerne.lock', 409), 'O backend está ocupado ou mudou por outro caminho. Tente de novo.'],
    [new HTTPError('push rejected', 500), 'Não foi possível gravar o nome: push rejected'],
  ])('crit. 7: a falha %s fica junto do campo, mantém o texto e o nome do backend', async (err, msg) => {
    renameProject.mockRejectedValueOnce(err)
    const r = await region()
    submit(r, 'Novo nome')
    expect(await within(r).findByRole('alert')).toHaveTextContent(msg)
    expect(within(r).getByRole('textbox', { name: 'Nome do projeto' })).toHaveValue('Novo nome')
    expect(document.querySelector('.settings-project strong')).toHaveTextContent('Projeto A')

    renameProject.mockResolvedValueOnce('Novo nome')
    fireEvent.click(within(r).getByRole('button', { name: 'Salvar' }))
    await waitFor(() => expect(renameProject).toHaveBeenCalledTimes(2))
    expect(renameProject).toHaveBeenLastCalledWith('Novo nome')
  })

  it('crítica C9: da mensagem do servidor (stderr do git, multilinha) o alerta mostra só a primeira linha', async () => {
    renameProject.mockRejectedValueOnce(
      new HTTPError(
        "git push --porcelain origin refs/heads/main: exit status 1\nremote: denied\nerror: failed to push some refs to '/private/tmp/x/backend.git'",
        500,
      ),
    )
    const r = await region()
    submit(r, 'Novo nome')
    const alert = await within(r).findByRole('alert')
    expect(alert).toHaveTextContent(
      /^Não foi possível gravar o nome: git push --porcelain origin refs\/heads\/main: exit status 1$/,
    )
  })

  it('crit. 7: durante a escrita o botão diz Salvando… e um segundo clique não grava de novo', async () => {
    const write = deferred<string>()
    renameProject.mockReturnValue(write.promise)
    const r = await region()
    submit(r, 'Novo nome')
    const btn = await within(r).findByRole('button', { name: 'Salvando…' })
    expect(btn).toHaveAttribute('aria-disabled', 'true')
    expect(btn).not.toBeDisabled()
    fireEvent.click(btn)
    expect(renameProject).toHaveBeenCalledTimes(1)
    // Crítica C6: o que se digitasse agora sumiria na remontagem do sucesso.
    const input = within(r).getByRole('textbox', { name: 'Nome do projeto' })
    expect(input).toHaveAttribute('readonly')
    write.resolve('Novo nome')
    expect(await within(r).findByRole('button', { name: 'Salvar' })).not.toHaveAttribute('aria-disabled')
    expect(input).not.toHaveAttribute('readonly')
  })

  // Crítica C5: a lista relida remonta o campo com o nome novo; o foco não pode
  // cair no <body>, e o sucesso precisa ser anunciado. As duas ordens são
  // reais: o servidor emite "projects" antes de responder ao PUT.
  it.each([
    ['a resposta chega antes da lista relida', false],
    ['a lista relida chega antes da resposta', true],
  ])('crítica C5: %s — o foco volta ao campo remontado e o sucesso é anunciado', async (_, listFirst) => {
    const write = deferred<string>()
    renameProject.mockReturnValue(write.promise)
    const { rerender } = render(<Settings />)
    const r = await screen.findByRole('region', { name: 'Nome do projeto' })
    const input = within(r).getByRole('textbox', { name: 'Nome do projeto' })
    input.focus()
    fireEvent.change(input, { target: { value: 'Núcleo' } })
    fireEvent.submit(input.closest('form')!)
    const reread = () => {
      mockProjects = [{ pid: 'p1', name: 'Núcleo' }]
      rerender(<Settings />)
    }
    if (listFirst) reread()
    write.resolve('Núcleo')
    expect(await within(r).findByRole('status')).toHaveTextContent('Nome gravado.')
    if (!listFirst) reread()
    const fresh = within(r).getByRole('textbox', { name: 'Nome do projeto' })
    expect(fresh).not.toBe(input)
    expect(fresh).toHaveValue('Núcleo')
    await waitFor(() => expect(fresh).toHaveFocus())
  })

  it('crítica C5: a recusa fica ligada ao campo (aria-invalid e aria-describedby)', async () => {
    const r = await region()
    const input = within(r).getByRole('textbox', { name: 'Nome do projeto' })
    expect(input).not.toHaveAttribute('aria-invalid')
    submit(r, '   ')
    expect(input).toHaveAttribute('aria-invalid', 'true')
    expect(input).toHaveAccessibleDescription(expect.stringContaining('O nome do projeto não pode ficar vazio.'))
    expect(within(r).queryByRole('status')).toBeNull()
  })

  it('crítica C13: o bloco do nome não depende da carga dos settings, e o texto digitado sobrevive a ela', async () => {
    fetchSettings.mockRejectedValueOnce(new Error('boom'))
    render(<Settings />)
    const status = await screen.findByRole('status')
    expect(status).toHaveTextContent('Erro ao carregar as configurações: boom')
    const field = () =>
      within(screen.getByRole('region', { name: 'Nome do projeto' })).getByRole('textbox', { name: 'Nome do projeto' })
    expect(field()).toHaveValue('Projeto A')
    fireEvent.change(field(), { target: { value: 'Digitado' } })
    fireEvent.click(within(status).getByRole('button', { name: 'Tentar de novo' }))
    await screen.findByRole('listitem', { name: 'Grafo' })
    expect(field()).toHaveValue('Digitado')
  })

  it('crit. 10: com o I18nProvider (jsdom = en), rótulo, destino e regra saem em inglês', async () => {
    render(
      <I18nProvider>
        <Settings />
      </I18nProvider>,
    )
    const r = await screen.findByRole('region', { name: 'Project name' })
    expect(within(r).getByText("Saved to the project's backend: applies to every clone.")).toBeInTheDocument()
    fireEvent.change(within(r).getByRole('textbox', { name: 'Project name' }), { target: { value: ' ' } })
    fireEvent.click(within(r).getByRole('button', { name: 'Save' }))
    expect(within(r).getByRole('alert')).toHaveTextContent('The project name cannot be empty.')
  })
})

// F-0392: a string key with declared values is a choice, not free text.
describe('Settings: chave de texto com valores declarados', () => {
  const gauntletDesc =
    "Turns the Gauntlet on or off for this project. With off (light mode), approve accepts a spec without a bar, complete accepts a result without a ## Gauntlet section, and lint stops charging both. Only the literal off turns it off; any other stored value counts as on. An agent cannot change this key through cerne settings: it is the project owner's decision."
  const gauntlet = (over: Partial<ResolvedSetting> = {}) =>
    key({
      id: 'gates.gauntlet',
      title: 'Gauntlet',
      type: 'string',
      default: 'on',
      enum: ['on', 'off'],
      scopes: ['project'],
      description: gauntletDesc,
      value: 'off',
      origin: 'project',
      stored: { project: 'off' },
      ...over,
    })
  const withGauntlet = (g = gauntlet()) => fixture({ settings: [...fixture().settings, g] })

  beforeEach(() => {
    cleanup()
    fetchSettings.mockReset()
    putSetting.mockReset()
  })

  it('mostra um select com os valores declarados e o efetivo selecionado, em pt-BR', async () => {
    fetchSettings.mockResolvedValue(withGauntlet())
    render(<Settings />)
    const r = await screen.findByRole('listitem', { name: 'Gauntlet' })
    const select = within(r).getByRole('combobox', { name: 'Gauntlet' }) as HTMLSelectElement
    expect(within(select).getAllByRole('option').map((o) => o.textContent)).toEqual(['ligado', 'desligado'])
    expect(select.value).toBe('off')
    expect(within(r).queryByRole('textbox')).toBeNull()
    expect(within(r).getByText(/Liga ou desliga o Gauntlet/)).toBeInTheDocument()
  })

  it('trocar a opção grava a string escolhida no escopo editado', async () => {
    fetchSettings.mockResolvedValue(withGauntlet())
    putSetting.mockResolvedValue(withGauntlet(gauntlet({ value: 'on', stored: { project: 'on' } })))
    render(<Settings />)
    const r = await screen.findByRole('listitem', { name: 'Gauntlet' })
    fireEvent.change(within(r).getByRole('combobox', { name: 'Gauntlet' }), { target: { value: 'on' } })
    await waitFor(() => expect(putSetting).toHaveBeenCalledWith('gates.gauntlet', 'project', 'on'))
  })

  it('no I18nProvider (en) os rótulos são os valores', async () => {
    fetchSettings.mockResolvedValue(withGauntlet())
    render(
      <I18nProvider>
        <Settings />
      </I18nProvider>,
    )
    const r = await screen.findByRole('listitem', { name: 'Gauntlet' })
    const select = within(r).getByRole('combobox', { name: 'Gauntlet' })
    expect(within(select).getAllByRole('option').map((o) => o.textContent)).toEqual(['on', 'off'])
  })

  it('a linha Efetivo/Padrão traduz o valor como o select: ligado/desligado em pt-BR, on/off em en', async () => {
    fetchSettings.mockResolvedValue(withGauntlet())
    const { unmount } = render(<Settings />)
    const r = await screen.findByRole('listitem', { name: 'Gauntlet' })
    expect(within(r).getByText('Efetivo: desligado, de Projeto')).toBeInTheDocument()
    expect(within(r).getByText('Padrão: ligado')).toBeInTheDocument()
    unmount()
    render(
      <I18nProvider>
        <Settings />
      </I18nProvider>,
    )
    const en = await screen.findByRole('listitem', { name: 'Gauntlet' })
    expect(within(en).getByText('Effective: off, from Project')).toBeInTheDocument()
    expect(within(en).getByText('Default: on')).toBeInTheDocument()
  })

  it('durante a escrita o select fica aria-disabled e mudar de novo não grava', async () => {
    fetchSettings.mockResolvedValue(withGauntlet())
    let release = () => {}
    putSetting.mockImplementationOnce(() => new Promise<SettingsView>((r) => (release = () => r(withGauntlet()))))
    render(<Settings />)
    const r = await screen.findByRole('listitem', { name: 'Gauntlet' })
    const select = within(r).getByRole('combobox', { name: 'Gauntlet' })
    fireEvent.change(select, { target: { value: 'on' } })
    await waitFor(() => expect(row('Gauntlet')).toHaveAttribute('aria-busy', 'true'))
    expect(select).toHaveAttribute('aria-disabled', 'true')
    fireEvent.change(select, { target: { value: 'off' } })
    expect(putSetting).toHaveBeenCalledTimes(1)
    release()
    await waitFor(() => expect(row('Gauntlet')).not.toHaveAttribute('aria-busy'))
  })

  it('lint.cli_output_language também é um select: o padrão vazio e en', async () => {
    const lang = key({
      id: 'lint.cli_output_language',
      title: 'CLI output language',
      type: 'string',
      default: '',
      enum: ['en'],
      scopes: ['project'],
      description: 'x',
      value: '',
    })
    fetchSettings.mockResolvedValue(fixture({ settings: [...fixture().settings, lang] }))
    putSetting.mockResolvedValue(fixture())
    render(<Settings />)
    const r = await screen.findByRole('listitem', { name: 'Idioma da saída da CLI' })
    const select = within(r).getByRole('combobox') as HTMLSelectElement
    expect(select.value).toBe('')
    fireEvent.change(select, { target: { value: 'en' } })
    await waitFor(() => expect(putSetting).toHaveBeenCalledWith('lint.cli_output_language', 'project', 'en'))
  })
})

describe('Settings — grupos (F-0413)', () => {
  const grouped = () =>
    fixture({
      settings: [
        key({ id: 'channel.human_name', title: 'Your name in the channel', type: 'string', default: '', scopes: ['host'], value: '', group: 'general' }),
        key({ id: 'gates.gauntlet', title: 'Gauntlet', type: 'string', default: 'on', scopes: ['project'], value: 'on', group: 'process' }),
        key({ id: 'graph.enabled', title: 'Graph', group: 'interface' }),
        key({ id: 'wiki.enabled', title: 'Wiki', group: 'interface' }),
      ],
    })
  const nav = () => within(screen.getByRole('navigation', { name: 'Grupos de configurações' }))

  beforeEach(() => {
    window.location.hash = '#/settings'
    fetchSettings.mockResolvedValue(grouped())
  })

  it('c4: a navegação lista só grupos com chave, e o nome do projeto conta para Geral', async () => {
    render(<Settings />)
    await screen.findByRole('listitem', { name: 'Seu nome no canal' })
    expect(nav().getAllByRole('link').map((a) => a.textContent)).toEqual(['Geral', 'Processo', 'Interface'])
    cleanup()
    fetchSettings.mockResolvedValue(fixture({ settings: [key({ id: 'graph.enabled', title: 'Graph', group: 'interface' })] }))
    render(<Settings group="interface" />)
    await screen.findByRole('listitem', { name: 'Grafo' })
    expect(nav().getAllByRole('link').map((a) => a.textContent)).toEqual(['Geral', 'Interface'])
  })

  it('c4: um grupo aberto mostra só as suas chaves; o aberto tem aria-current', async () => {
    render(<Settings group="process" />)
    await screen.findByRole('listitem', { name: 'Gauntlet' })
    expect(screen.queryByRole('listitem', { name: 'Grafo' })).toBeNull()
    expect(screen.queryByRole('listitem', { name: 'Wiki' })).toBeNull()
    expect(screen.queryByRole('textbox', { name: 'Nome do projeto' })).toBeNull()
    expect(nav().getByRole('link', { name: 'Processo' })).toHaveAttribute('aria-current', 'page')
    expect(nav().getByRole('link', { name: 'Interface' })).not.toHaveAttribute('aria-current')
    expect(nav().getByRole('link', { name: 'Interface' })).toHaveAttribute('href', '#/settings/interface')
  })

  it('c4: chave sem grupo (payload antigo) cai em Geral', async () => {
    fetchSettings.mockResolvedValue(fixture())
    render(<Settings group="general" />)
    expect(await screen.findByRole('listitem', { name: 'Grafo' })).toBeInTheDocument()
  })

  it('c4: abaixo de 640 px a navegação é um select rotulado que troca o hash', async () => {
    render(<Settings group="process" />)
    await screen.findByRole('listitem', { name: 'Gauntlet' })
    const select = screen.getByRole('combobox', { name: 'Grupos de configurações' })
    expect(select).toHaveValue('process')
    fireEvent.change(select, { target: { value: 'interface' } })
    expect(window.location.hash).toBe('#/settings/interface')
  })

  it('c5: com texto na busca aparecem chaves de todos os grupos, cada uma com o nome do grupo; limpar volta ao grupo aberto', async () => {
    render(<Settings group="process" />)
    await screen.findByRole('listitem', { name: 'Gauntlet' })
    const box = screen.getByRole('searchbox', { name: 'Buscar configurações' })
    fireEvent.change(box, { target: { value: 'enabled' } })
    const graph = screen.getByRole('listitem', { name: 'Grafo' })
    expect(within(graph).getByText('Interface')).toBeInTheDocument()
    expect(screen.getByRole('listitem', { name: 'Wiki' })).toBeInTheDocument()
    expect(screen.queryByRole('listitem', { name: 'Gauntlet' })).toBeNull()
    fireEvent.change(box, { target: { value: '' } })
    expect(screen.getByRole('listitem', { name: 'Gauntlet' })).toBeInTheDocument()
    expect(screen.queryByRole('listitem', { name: 'Grafo' })).toBeNull()
  })

  it('c6: o seletor Projeto | Host aparece em toda página de grupo', async () => {
    render(<Settings group="interface" />)
    await screen.findByRole('listitem', { name: 'Grafo' })
    expect(screen.getByRole('group', { name: /escopo/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /^Host/ })).toBeInTheDocument()
  })
})

describe('Settings — livro-razão (F-0413)', () => {
  beforeEach(() => {
    window.location.hash = '#/settings'
  })

  it('checkbox afirmativo: o rótulo visível ao lado da caixa é o título; a descrição fica abaixo, por aria-describedby', async () => {
    render(<Settings />)
    const extra = await screen.findByRole('listitem', { name: 'Extra toggle' })
    const box = within(extra).getByRole('checkbox', { name: 'Extra toggle' })
    expect(box.closest('label')).toHaveTextContent(/^Extra toggle$/)
    const desc = within(extra).getByText('An injected key the screen never heard of.')
    expect(box.closest('label')).not.toContainElement(desc)
    expect(box).toHaveAccessibleDescription('An injected key the screen never heard of.')
  })

  it('escopo visto sem valor e outro com valor: "Herdado de Host", sem "Também modificado em"', async () => {
    render(<Settings />)
    const extra = await screen.findByRole('listitem', { name: 'Extra toggle' })
    expect(within(extra).getByText('Herdado de Host')).toBeInTheDocument()
    expect(within(extra).queryByText(/Também modificado em/)).toBeNull()
  })

  it('en: "Inherited from Host"', async () => {
    render(
      <I18nProvider>
        <Settings />
      </I18nProvider>,
    )
    const extra = await screen.findByRole('listitem', { name: 'Extra toggle' })
    expect(within(extra).getByText('Inherited from Host')).toBeInTheDocument()
  })

  it('a precedência é dita junto do seletor de escopo', async () => {
    render(<Settings />)
    await screen.findByRole('listitem', { name: 'Extra toggle' })
    const sel = screen.getByRole('group', { name: /escopo/i })
    expect(within(sel.parentElement as HTMLElement).getByText('Projeto vence Host, que vence o padrão.')).toBeInTheDocument()
  })

  it('cada linha mostra o escopo em que a escrita cai', async () => {
    render(<Settings />)
    const extra = await screen.findByRole('listitem', { name: 'Extra toggle' })
    expect(within(extra).getByText('Grava em Projeto')).toBeInTheDocument()
  })
})
