import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import Settings from './Settings'
import { ChannelLine } from './Channel'
import { I18nProvider } from './i18n'
import type { ChannelMessage, ResolvedSetting, SettingScope, SettingsView } from './api'

// F-0493: a seção "Data e hora" do grupo Interface. É preferência de quem
// olha, guardada no navegador: não passa pelo registro do servidor.

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

vi.mock('./live', () => ({
  useProjects: () => ({ projects: [{ pid: 'p1', name: 'Projeto A' }], activePid: 'p1' }),
  useLiveVersion: () => 0,
}))

const NOW = new Date('2026-11-21T14:43:37Z')
const AT = '2026-11-21T14:50:00Z'
const FORMATS = ['pt-BR', 'pt-PT', 'en-US', 'en-GB', 'es-ES', 'es-MX', 'fr-FR', 'de-DE', 'it-IT', 'ja-JP', 'zh-CN']

const view = (): SettingsView => ({
  files: { project: '/repo/.cernerc', host: '/home/u/.config/cerne/settings.toml' },
  settings: [
    {
      id: 'graph.enabled',
      title: 'Graph',
      type: 'bool',
      default: true,
      scopes: ['project', 'host'],
      description: 'Shows the Graph tab in the UI. Off hides it.',
      group: 'interface',
      value: true,
      origin: 'default',
    } satisfies ResolvedSetting,
  ],
})

const message: ChannelMessage = {
  seq: 7,
  type: 'message',
  nick: 'W-0291',
  body: 'oi',
  time: AT,
  state: 'delivered',
}

// O computador diz pt-BR e America/Sao_Paulo, salvo quando o teste muda.
function computer(languages: string[] = ['pt-BR', 'en'], timeZone: string | undefined = 'America/Sao_Paulo') {
  vi.spyOn(navigator, 'languages', 'get').mockReturnValue(languages)
  vi.spyOn(navigator, 'language', 'get').mockReturnValue(languages[0] ?? '')
  const real = Intl.DateTimeFormat.prototype.resolvedOptions
  vi.spyOn(Intl.DateTimeFormat.prototype, 'resolvedOptions').mockImplementation(function (this: Intl.DateTimeFormat) {
    return { ...real.call(this), timeZone } as Intl.ResolvedDateTimeFormatOptions
  })
}

function open(extra?: React.ReactNode, group: 'interface' | 'general' = 'interface') {
  return render(
    <I18nProvider>
      <Settings group={group} />
      {extra}
    </I18nProvider>,
  )
}

const section = async () => screen.findByRole('region', { name: 'Data e hora' })
const field = (name: string) => screen.getByRole('combobox', { name }) as HTMLSelectElement
const chosen = (select: HTMLSelectElement) => select.selectedOptions[0].textContent

beforeEach(() => {
  cleanup()
  localStorage.clear()
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(NOW)
  fetchSettings.mockReset().mockResolvedValue(view())
  putSetting.mockReset()
  unsetSetting.mockReset()
  renameProject.mockReset()
})

afterEach(() => {
  vi.useRealTimers()
  vi.restoreAllMocks()
  localStorage.clear()
})

describe('Settings — Data e hora (F-0493 crit. 6)', () => {
  // F-0493 crit. 6
  it('F-0493 crit. 6: dois selects nativos, com "Automático (do computador)" e o valor detectado escolhidos', async () => {
    computer()
    open()
    const region = await section()
    const selects = within(region).getAllByRole('combobox')
    expect(selects).toHaveLength(2)
    expect(selects.every((s) => s.tagName === 'SELECT')).toBe(true)
    expect(field('Formato').value).toBe('')
    expect(chosen(field('Formato'))).toBe('Automático (do computador) — português (Brasil)')
    expect(field('Fuso horário').value).toBe('')
    expect(chosen(field('Fuso horário'))).toBe('Automático (do computador) — America/Sao_Paulo')
    // A primeira opção de cada um é o automático.
    expect(field('Formato').options[0].value).toBe('')
    expect(field('Fuso horário').options[0].value).toBe('')
  })

  // F-0493 crit. 6
  it('F-0493 crit. 6: a seção só aparece no grupo Interface', async () => {
    computer()
    open(undefined, 'general')
    await screen.findByRole('heading', { name: 'Geral' })
    expect(screen.queryByRole('region', { name: 'Data e hora' })).toBeNull()
  })

  // F-0493 crit. 6
  it('F-0493 crit. 6: a linha "Efetivo" repete o valor em uso e diz de onde vem', async () => {
    computer()
    open()
    const region = await section()
    expect(within(region).getByText('Efetivo: português (Brasil), do computador')).toBeInTheDocument()
    expect(within(region).getByText('Efetivo: America/Sao_Paulo, do computador')).toBeInTheDocument()
    fireEvent.change(field('Formato'), { target: { value: 'en-GB' } })
    expect(within(region).getByText('Efetivo: inglês (Reino Unido), neste navegador')).toBeInTheDocument()
    // A chave do localStorage vai em mono, onde as outras linhas põem o id.
    expect(within(region).getByText('cerne-date-locale').tagName).toBe('CODE')
    expect(within(region).getByText('cerne-time-zone').tagName).toBe('CODE')
  })
})

describe('Settings — listas (F-0493 crit. 7)', () => {
  // F-0493 crit. 7
  it('F-0493 crit. 7: onze formatos, cada um com o nome no idioma da interface e uma amostra do instante', async () => {
    computer()
    localStorage.setItem('cerne-time-zone', 'UTC')
    open()
    await section()
    const options = [...field('Formato').options].slice(1)
    expect(options.map((o) => o.value)).toEqual(FORMATS)
    const names = new Intl.DisplayNames(['pt-BR'], { type: 'language' })
    for (const o of options) expect(o.textContent, o.value).toContain(names.of(o.value)!)
    expect(options[0].textContent).toContain('português (Brasil)')
    // A amostra mostra a diferença que o nome não mostra.
    const sample = (code: string) => options.find((o) => o.value === code)!.textContent
    expect(sample('en-US')).toContain('11/21/26, 2:43 PM')
    expect(sample('en-GB')).toContain('21/11/2026, 14:43')
    expect(sample('pt-BR')).toContain('21/11/2026, 14:43')
    expect(sample('ja-JP')).toContain('2026/11/21 14:43')
  })

  // F-0493 crit. 7
  it('F-0493 crit. 7: o nome das opções segue o idioma da interface', async () => {
    computer()
    localStorage.setItem('cerne-lang', 'en')
    open()
    await screen.findByRole('region', { name: 'Date and time' })
    const names = new Intl.DisplayNames(['en'], { type: 'language' })
    const gb = [...screen.getByRole('combobox', { name: 'Format' }).querySelectorAll('option')].find((o) => o.value === 'en-GB')!
    expect(gb.textContent).toContain(names.of('en-GB')!)
    expect(chosen(screen.getByRole('combobox', { name: 'Format' }) as HTMLSelectElement)).toBe(
      'Automatic (from this computer) — Brazilian Portuguese',
    )
  })

  // F-0493 crit. 7
  it('F-0493 crit. 7: os fusos que o navegador conhece, agrupados por região em optgroup', async () => {
    computer()
    open()
    await section()
    const zones = field('Fuso horário')
    const groups = [...zones.querySelectorAll('optgroup')]
    const labels = groups.map((g) => g.label)
    for (const region of ['Africa', 'America', 'Asia', 'Atlantic', 'Australia', 'Europe', 'Indian', 'Pacific']) {
      expect(labels, region).toContain(region)
    }
    const america = groups.find((g) => g.label === 'America')!
    expect([...america.querySelectorAll('option')].map((o) => o.value)).toContain('America/Sao_Paulo')
    const all = [...zones.querySelectorAll('option')].map((o) => o.value).filter((v) => v !== '')
    for (const z of Intl.supportedValuesOf('timeZone')) expect(all, z).toContain(z)
    expect(all).toContain('UTC')
  })

  // F-0493 crit. 7
  it('F-0493 crit. 7: sem Intl.supportedValuesOf, só o fuso detectado e UTC', async () => {
    computer()
    const real = Intl.supportedValuesOf
    Object.defineProperty(Intl, 'supportedValuesOf', { value: undefined, configurable: true, writable: true })
    try {
      open()
      await section()
      const zones = field('Fuso horário')
      expect([...zones.options].map((o) => o.value)).toEqual(['', 'America/Sao_Paulo', 'UTC'])
      expect(zones.querySelectorAll('optgroup')).toHaveLength(0)
    } finally {
      Object.defineProperty(Intl, 'supportedValuesOf', { value: real, configurable: true, writable: true })
    }
  })

  // F-0493 crit. 7
  it('F-0493 crit. 7: um valor guardado que a lista não traz continua escolhível', async () => {
    computer()
    localStorage.setItem('cerne-date-locale', 'pt-AO')
    open()
    await section()
    expect(field('Formato').value).toBe('pt-AO')
  })
})

describe('Settings — gravação e aplicação (F-0493 crit. 8)', () => {
  // F-0493 crit. 8
  it('F-0493 crit. 8: escolher grava nas chaves ao lado do cerne-lang e aplica ao canal sem recarregar', async () => {
    computer(['pt-BR'], 'UTC')
    const { container } = open(<ChannelLine m={message} />)
    await section()
    const stamp = () => container.querySelector(`time[datetime="${AT}"]`)!
    expect(stamp()).toHaveTextContent('21/11, 14:50')

    fireEvent.change(field('Formato'), { target: { value: 'en-US' } })
    expect(localStorage.getItem('cerne-date-locale')).toBe('en-US')
    expect(stamp()).toHaveTextContent('11/21, 02:50 PM')
    // O idioma da interface não mudou: o texto segue em português.
    expect(document.documentElement.lang).toBe('pt-BR')

    fireEvent.change(field('Fuso horário'), { target: { value: 'Asia/Tokyo' } })
    expect(localStorage.getItem('cerne-time-zone')).toBe('Asia/Tokyo')
    expect(stamp()).toHaveTextContent('11/21, 11:50 PM')
    expect(stamp()).toHaveAttribute('title', expect.stringContaining('GMT+9'))
  })

  // F-0493 crit. 8
  it('F-0493 crit. 8: voltar a "Automático" apaga a chave e devolve o valor do computador', async () => {
    computer(['pt-BR'], 'UTC')
    localStorage.setItem('cerne-date-locale', 'en-US')
    localStorage.setItem('cerne-time-zone', 'Asia/Tokyo')
    const { container } = open(<ChannelLine m={message} />)
    await section()
    expect(field('Formato').value).toBe('en-US')
    expect(field('Fuso horário').value).toBe('Asia/Tokyo')

    fireEvent.change(field('Formato'), { target: { value: '' } })
    fireEvent.change(field('Fuso horário'), { target: { value: '' } })
    expect(localStorage.getItem('cerne-date-locale')).toBeNull()
    expect(localStorage.getItem('cerne-time-zone')).toBeNull()
    expect(container.querySelector(`time[datetime="${AT}"]`)).toHaveTextContent('21/11, 14:50')
    expect(chosen(field('Formato'))).toContain('Automático (do computador)')
  })
})

describe('Settings — valor inválido e storage que falha (F-0493 crit. 9)', () => {
  // F-0493 crit. 9
  it('F-0493 crit. 9: fuso inexistente e locale desconhecido caem no automático, sem erro', async () => {
    computer()
    const errors = vi.spyOn(console, 'error').mockImplementation(() => {})
    localStorage.setItem('cerne-time-zone', 'Lua/Base')
    localStorage.setItem('cerne-date-locale', 'xx-INVALID')
    open(<ChannelLine m={message} />)
    await section()
    expect(field('Formato').value).toBe('')
    expect(field('Fuso horário').value).toBe('')
    expect(chosen(field('Fuso horário'))).toContain('Automático (do computador)')
    expect(errors).not.toHaveBeenCalled()
  })

  // F-0493 crit. 9
  it('F-0493 crit. 9: localStorage que lança na leitura e na gravação cai no automático, sem erro', async () => {
    computer()
    const errors = vi.spyOn(console, 'error').mockImplementation(() => {})
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new DOMException('negado', 'SecurityError')
    })
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new DOMException('cheio', 'QuotaExceededError')
    })
    vi.spyOn(Storage.prototype, 'removeItem').mockImplementation(() => {
      throw new DOMException('negado', 'SecurityError')
    })
    open()
    await section()
    expect(field('Formato').value).toBe('')
    fireEvent.change(field('Formato'), { target: { value: 'en-GB' } })
    fireEvent.change(field('Fuso horário'), { target: { value: 'Asia/Tokyo' } })
    expect(field('Formato').value).toBe('')
    expect(field('Fuso horário').value).toBe('')
    expect(errors).not.toHaveBeenCalled()
  })
})

describe('Settings — prévia (F-0493 crit. 10)', () => {
  // F-0493 crit. 10
  it('F-0493 crit. 10: a prévia mostra o instante atual no formato do title, com o fuso, e muda com a escolha', async () => {
    computer(['pt-BR'], 'UTC')
    open()
    const region = await section()
    const preview = () => within(region).getByText(/\d{4}.*\d:\d{2}:\d{2}/, { selector: 'time' })
    expect(preview()).toHaveTextContent('21/11/2026, 14:43:37 UTC')
    expect(preview()).toHaveAttribute('datetime', NOW.toISOString())

    fireEvent.change(field('Formato'), { target: { value: 'en-US' } })
    expect(preview()).toHaveTextContent('11/21/2026, 2:43:37 PM UTC')

    fireEvent.change(field('Fuso horário'), { target: { value: 'Asia/Tokyo' } })
    expect(preview()).toHaveTextContent('11/21/2026, 11:43:37 PM GMT+9')

    fireEvent.change(field('Formato'), { target: { value: 'en-GB' } })
    fireEvent.change(field('Fuso horário'), { target: { value: 'Europe/Lisbon' } })
    expect(preview()).toHaveTextContent('21/11/2026, 14:43:37 WET')
  })
})

describe('Settings — só este navegador (F-0493 crit. 11)', () => {
  // F-0493 crit. 11
  it('F-0493 crit. 11: cada linha traz o selo "este navegador", no lugar do escopo', async () => {
    computer()
    open()
    const region = await section()
    for (const title of ['Formato', 'Fuso horário']) {
      const row = within(region).getByRole('listitem', { name: title })
      expect(within(row).getByText('este navegador')).toBeInTheDocument()
      expect(within(row).queryByText('Host')).toBeNull()
    }
    expect(region).toHaveTextContent(/só (vale )?(para )?este navegador/i)
  })

  // F-0493 crit. 11
  it('F-0493 crit. 11: a seção fica acima do seletor Projeto | Host, fora dele', async () => {
    computer()
    open()
    const region = await section()
    const chooser = await screen.findByRole('group', { name: /escopo/i })
    expect(region.compareDocumentPosition(chooser) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(chooser.contains(region)).toBe(false)
    expect(region.contains(chooser)).toBe(false)
  })

  // F-0493 crit. 11
  it('F-0493 crit. 11: nenhuma escolha chama a API de configurações do servidor', async () => {
    computer()
    open()
    await section()
    fireEvent.change(field('Formato'), { target: { value: 'en-GB' } })
    fireEvent.change(field('Fuso horário'), { target: { value: 'Europe/Lisbon' } })
    fireEvent.change(field('Formato'), { target: { value: '' } })
    expect(putSetting).not.toHaveBeenCalled()
    expect(unsetSetting).not.toHaveBeenCalled()
    expect(renameProject).not.toHaveBeenCalled()
  })
})
