import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import AISettings from './AISettings'
import { I18nProvider } from './i18n'
import type { LLMConfig, LLMKeyStatus, ResolvedSetting, SettingScope, SettingsView } from './api'

// F-0414: the AI group's panel. It knows no endpoint shape beyond the api
// module; every write goes through putSetting/unsetSetting (llm.* keys) or the
// key endpoints, in the scope the page selector shows.

const fetchLLMConfig = vi.fn<() => Promise<LLMConfig>>()
type TestBody = { mode?: string; provider?: string; model?: string; base_url?: string; key?: string }
const testLLM = vi.fn<(b: TestBody) => Promise<{ ok: boolean; error?: string }>>()
const putLLMKey = vi.fn<(provider: string, value: string) => Promise<LLMKeyStatus>>()
const deleteLLMKey = vi.fn<(provider: string) => Promise<LLMKeyStatus>>()
const putSetting = vi.fn<(id: string, scope: SettingScope, value: unknown) => Promise<SettingsView>>()
const unsetSetting = vi.fn<(id: string, scope: SettingScope) => Promise<SettingsView>>()

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  fetchLLMConfig: () => fetchLLMConfig(),
  testLLM: (b: TestBody) => testLLM(b),
  putLLMKey: (p: string, v: string) => putLLMKey(p, v),
  deleteLLMKey: (p: string) => deleteLLMKey(p),
  putSetting: (id: string, scope: SettingScope, value: unknown) => putSetting(id, scope, value),
  unsetSetting: (id: string, scope: SettingScope) => unsetSetting(id, scope),
}))

const view: SettingsView = { settings: [], files: {} }
const reg = (id: string, over: Partial<ResolvedSetting> = {}): ResolvedSetting => ({
  id,
  title: id,
  type: 'string',
  default: '',
  scopes: ['project', 'host'],
  description: '',
  value: '',
  origin: 'default',
  group: 'ai',
  ...over,
})
const noStored = [reg('llm.mode'), reg('llm.provider'), reg('llm.model'), reg('llm.base_url')]

const cfg = (over: Partial<LLMConfig> = {}): LLMConfig => ({
  mode: 'claude-cli',
  source: 'detected',
  clis: [
    { name: 'claude', mode: 'claude-cli', found: true },
    { name: 'codex', mode: 'codex-cli', found: false },
    { name: 'gemini', mode: 'gemini-cli', found: true },
  ],
  provider: 'anthropic',
  model: '',
  base_url: '',
  key: { env: 'ANTHROPIC_API_KEY', set: false },
  models: [
    { id: 'claude-opus-4-8', label: 'Opus 4.8', provider: 'anthropic', visibility: 'current' },
    { id: 'claude-old', label: 'Old', provider: 'anthropic', visibility: 'delisted' },
  ],
  default_model: 'claude-opus-4-8',
  ...over,
})

const onScope = vi.fn()
function mount(settings = noStored, scope: SettingScope = 'project') {
  const onWritten = vi.fn()
  render(
    <I18nProvider>
      <AISettings settings={settings} scope={scope} onWritten={onWritten} onScope={onScope} />
    </I18nProvider>,
  )
  return onWritten
}

beforeEach(() => {
  localStorage.setItem('cerne-lang', 'pt-BR')
  fetchLLMConfig.mockResolvedValue(cfg())
  putSetting.mockResolvedValue(view)
  unsetSetting.mockResolvedValue(view)
  testLLM.mockResolvedValue({ ok: true })
})
afterEach(() => {
  cleanup()
  vi.clearAllMocks()
  localStorage.clear()
})

const pick = (name: string) => fireEvent.click(screen.getByRole('radio', { name }))

describe('AISettings (F-0414)', () => {
  it('critério 1: sem valor gravado, diz que a CLI foi detectada no PATH', async () => {
    mount()
    const line = await screen.findByText(/Em uso:/)
    expect(line.closest('div')).toHaveTextContent('Assinatura — Claude Code')
    expect(line.closest('div')).toHaveTextContent('detectado no PATH')
  })

  it('critério 1: valor gravado diz em que escopo foi configurado', async () => {
    fetchLLMConfig.mockResolvedValue(cfg({ source: 'configured', mode: 'gemini-cli' }))
    mount([reg('llm.mode', { stored: { host: 'gemini-cli' } }), ...noStored.slice(1)])
    const line = await screen.findByText(/Em uso:/)
    expect(line.closest('div')).toHaveTextContent('Gemini CLI')
    expect(line.closest('div')).toHaveTextContent('modo definido no Host')
  })

  it('critério 2: os quatro caminhos são um grupo de rádio rotulado e a Assinatura lista as três CLIs', async () => {
    mount()
    const group = await screen.findByRole('radiogroup', { name: /Como o cerne chega a um modelo/ })
    expect(within(group).getAllByRole('radio').map((r) => (r as HTMLInputElement).value)).toEqual([
      'subscription',
      'api',
      'gateway',
      'delegation',
    ])
    expect(screen.getByRole('radio', { name: /Assinatura/ })).toBeChecked()
    expect(screen.getByText('Claude Code').closest('li')).toHaveTextContent('instalado')
    expect(screen.getByText('Codex CLI').closest('li')).toHaveTextContent('não encontrado')
  })

  it('critério 2: Usar numa CLI grava llm.mode no escopo selecionado', async () => {
    mount(noStored, 'host')
    fireEvent.click(await screen.findByRole('button', { name: 'Usar Gemini CLI' }))
    await waitFor(() => expect(putSetting).toHaveBeenCalledWith('llm.mode', 'host', 'gemini-cli'))
  })

  it('critério 2: Usar numa CLI não instalada não grava nada', async () => {
    mount()
    fireEvent.click(await screen.findByRole('button', { name: 'Usar Codex CLI' }))
    expect(putSetting).not.toHaveBeenCalled()
  })

  it('critério 2: Delegação explica o caminho e Usar grava llm.mode=delegation', async () => {
    mount()
    await screen.findByText(/Em uso:/)
    pick('Delegação')
    expect(screen.getAllByText(/O agente da sessão em curso responde/).length).toBeGreaterThan(0)
    fireEvent.click(screen.getByRole('button', { name: 'Usar delegação' }))
    await waitFor(() => expect(putSetting).toHaveBeenCalledWith('llm.mode', 'project', 'delegation'))
  })

  it('critério 4: Testar mostra OK numa região viva educada', async () => {
    mount()
    fireEvent.click(await screen.findByRole('button', { name: 'Testar Claude Code' }))
    const live = await screen.findByText('Claude Code: conexão OK.')
    expect(live.closest('[aria-live="polite"]')).not.toBeNull()
    expect(testLLM).toHaveBeenCalledWith({ mode: 'claude-cli' })
  })

  it('critério 4: Testar com erro mostra o erro com a correção que o servidor nomeou', async () => {
    testLLM.mockResolvedValue({ ok: false, error: 'not signed in; run the login' })
    mount()
    fireEvent.click(await screen.findByRole('button', { name: 'Testar Gemini CLI' }))
    const msg = (await screen.findAllByText(/not signed in; run the login/))[0]
    expect(msg.closest('[aria-live="polite"]')).not.toBeNull()
  })

  it('critério 7: Chave de API grava mode, provider e modelo; o modelo da Anthropic é um select do catálogo sem os deslistados', async () => {
    fetchLLMConfig.mockResolvedValue(cfg({ key: { env: 'ANTHROPIC_API_KEY', set: true, source: 'env' } }))
    mount()
    await screen.findByText(/Em uso:/)
    pick('Chave de API')
    const model = screen.getByRole('combobox', { name: 'Modelo' })
    expect(within(model).getAllByRole('option').map((o) => o.textContent)).toEqual(['Padrão do catálogo', 'Opus 4.8'])
    fireEvent.change(model, { target: { value: 'claude-opus-4-8' } })
    fireEvent.click(screen.getByRole('button', { name: 'Usar chave de API' }))
    await waitFor(() => expect(putSetting).toHaveBeenCalledWith('llm.model', 'project', 'claude-opus-4-8'))
    expect(putSetting).toHaveBeenCalledWith('llm.mode', 'project', 'byo-key')
    expect(putSetting).toHaveBeenCalledWith('llm.provider', 'project', 'anthropic')
    expect(unsetSetting).toHaveBeenCalledWith('llm.base_url', 'project')
  })

  it('critério 7: fora da Anthropic o modelo é texto livre', async () => {
    mount()
    await screen.findByText(/Em uso:/)
    pick('Chave de API')
    fireEvent.change(screen.getByRole('combobox', { name: 'Provedor' }), { target: { value: 'openai' } })
    fireEvent.change(screen.getByLabelText('Chave', { selector: 'input' }), { target: { value: 'sk-typed-0000' } })
    const model = screen.getByRole('textbox', { name: 'Modelo' })
    fireEvent.change(model, { target: { value: 'gpt-5' } })
    fireEvent.click(screen.getByRole('button', { name: 'Usar chave de API' }))
    await waitFor(() => expect(putSetting).toHaveBeenCalledWith('llm.model', 'project', 'gpt-5'))
    expect(putSetting).toHaveBeenCalledWith('llm.provider', 'project', 'openai')
    // item 7: Usar also saves the typed key.
    expect(putLLMKey).toHaveBeenCalledWith('openai', 'sk-typed-0000')
  })

  it('critério 8: compatível pede a URL base e a grava; modelo vazio remove a chave do escopo', async () => {
    mount(noStored, 'host')
    await screen.findByText(/Em uso:/)
    pick('Gateway ou compatível')
    fireEvent.change(screen.getByRole('combobox', { name: 'Gateway' }), { target: { value: 'compatible' } })
    expect(screen.getByRole('textbox', { name: 'URL base' })).toHaveValue('http://localhost:11434/v1')
    fireEvent.click(screen.getByRole('button', { name: 'Usar gateway ou compatível' }))
    await waitFor(() => expect(putSetting).toHaveBeenCalledWith('llm.base_url', 'host', 'http://localhost:11434/v1'))
    expect(putSetting).toHaveBeenCalledWith('llm.provider', 'host', 'compatible')
    expect(putSetting).toHaveBeenCalledWith('llm.mode', 'host', 'byo-key')
    expect(unsetSetting).toHaveBeenCalledWith('llm.model', 'host')
  })

  it('critério 8: a chave salva nunca volta ao DOM, só os 4 últimos, e o campo limpa', async () => {
    putLLMKey.mockResolvedValue({ env: 'ANTHROPIC_API_KEY', set: true, source: 'file', last4: 'wxyz' })
    const { container } = { container: document.body }
    mount()
    await screen.findByText(/Em uso:/)
    pick('Chave de API')
    const field = screen.getByLabelText('Chave', { selector: 'input' }) as HTMLInputElement
    expect(field.type).toBe('password')
    fireEvent.change(field, { target: { value: 'sk-secret-1234567890wxyz' } })
    fireEvent.click(screen.getByRole('button', { name: 'Salvar chave' }))
    await screen.findByText('•••• wxyz')
    expect(putLLMKey).toHaveBeenCalledWith('anthropic', 'sk-secret-1234567890wxyz')
    expect(field.value).toBe('')
    expect(container.innerHTML).not.toContain('sk-secret')
  })

  it('critério 8: chave vinda do ambiente diz a variável e não oferece Remover', async () => {
    fetchLLMConfig.mockResolvedValue(cfg({ key: { env: 'ANTHROPIC_API_KEY', set: true, source: 'env' } }))
    mount()
    await screen.findByText(/Em uso:/)
    pick('Chave de API')
    expect(screen.getByText('Chave definida pela variável ANTHROPIC_API_KEY.')).toBeInTheDocument()
    expect(screen.getByText(/só neste computador/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Remover chave' })).toBeNull()
  })

  it('critério 8: Remover chave chama o DELETE e volta a "nenhuma chave"', async () => {
    fetchLLMConfig.mockResolvedValue(cfg({ key: { env: 'ANTHROPIC_API_KEY', set: true, source: 'file', last4: 'wxyz' } }))
    deleteLLMKey.mockResolvedValue({ env: 'ANTHROPIC_API_KEY', set: false })
    mount()
    await screen.findByText(/Em uso:/)
    pick('Chave de API')
    expect(screen.getByText('•••• wxyz')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Remover chave' }))
    await waitFor(() => expect(deleteLLMKey).toHaveBeenCalledWith('anthropic'))
    await screen.findByText('Nenhuma chave definida.')
  })

  it('item 4: a CLI em uso mostra "em uso" em vez de Usar; as outras mantêm o botão', async () => {
    mount()
    await screen.findByText(/Em uso:/)
    const row = screen.getByText('Claude Code').closest('li')!
    expect(row).toHaveTextContent('em uso')
    expect(within(row).queryByRole('button', { name: 'Usar Claude Code' })).toBeNull()
    expect(screen.getByRole('button', { name: 'Usar Gemini CLI' })).toBeInTheDocument()
  })

  it('item 4: caminho escolhido e fora de uso diz "Não está em uso" com o botão Usar', async () => {
    mount()
    await screen.findByText(/Em uso:/)
    pick('Delegação')
    expect(screen.getByText(/Não está em uso/)).toBeInTheDocument()
  })

  it('item 4: com modo gravado no escopo, Voltar à detecção automática remove llm.mode', async () => {
    fetchLLMConfig.mockResolvedValue(cfg({ source: 'configured', mode: 'claude-cli' }))
    mount([reg('llm.mode', { stored: { project: 'claude-cli' } }), ...noStored.slice(1)])
    fireEvent.click(await screen.findByRole('button', { name: 'Voltar à detecção automática' }))
    await waitFor(() => expect(unsetSetting).toHaveBeenCalledWith('llm.mode', 'project'))
  })

  it('item 4: sem modo gravado não oferece voltar à detecção', async () => {
    mount()
    await screen.findByText(/Em uso:/)
    expect(screen.queryByRole('button', { name: 'Voltar à detecção automática' })).toBeNull()
  })

  it('item 5: o resultado do teste aparece dentro da linha testada, com a correção em mono e os detalhes', async () => {
    testLLM.mockResolvedValue({
      ok: false,
      error: `gemini-cli invocation failed: failed to call Gemini CLI: "gemini" exited with exit status 1: boom — run 'gemini auth' and sign in`,
    })
    mount()
    fireEvent.click(await screen.findByRole('button', { name: 'Testar Gemini CLI' }))
    const row = screen.getByText('Gemini CLI').closest('li')!
    expect(await within(row).findByText('gemini auth')).toBeInTheDocument()
    expect(within(row).getByText('Detalhes')).toBeInTheDocument()
    expect(within(screen.getByText('Claude Code').closest('li')!).queryByText('gemini auth')).toBeNull()
  })

  it('item 5: Testar na chave de API testa o formulário não salvo, com a chave digitada', async () => {
    mount()
    await screen.findByText(/Em uso:/)
    pick('Chave de API')
    fireEvent.change(screen.getByLabelText('Chave', { selector: 'input' }), { target: { value: 'sk-typed-0000' } })
    fireEvent.click(screen.getByRole('button', { name: 'Testar Chave de API' }))
    await waitFor(() =>
      expect(testLLM).toHaveBeenCalledWith({ provider: 'anthropic', model: '', base_url: '', key: 'sk-typed-0000' }),
    )
    expect(screen.queryByText(/use “Usar” antes/)).toBeNull()
  })

  it('item 7: Usar fica aria-disabled com o motivo quando não há chave', async () => {
    mount()
    await screen.findByText(/Em uso:/)
    pick('Chave de API')
    const use = screen.getByRole('button', { name: 'Usar chave de API' })
    expect(use).toHaveAttribute('aria-disabled', 'true')
    expect(use).toHaveAccessibleDescription(/Digite uma chave, ou defina ANTHROPIC_API_KEY/)
    fireEvent.click(use)
    expect(putSetting).not.toHaveBeenCalled()
  })

  it('item 7: compatível sem URL base não pode ser usado', async () => {
    mount()
    await screen.findByText(/Em uso:/)
    pick('Gateway ou compatível')
    fireEvent.change(screen.getByRole('combobox', { name: 'Gateway' }), { target: { value: 'compatible' } })
    fireEvent.change(screen.getByRole('textbox', { name: 'URL base' }), { target: { value: '' } })
    const use = screen.getByRole('button', { name: 'Usar gateway ou compatível' })
    expect(use).toHaveAttribute('aria-disabled', 'true')
    expect(use).toHaveAccessibleDescription(/URL base/)
  })

  it('item 8: o Host explica por que a assinatura é pessoal; não há nota duplicada do escopo', async () => {
    mount(noStored, 'host')
    await screen.findByText(/Em uso:/)
    expect(screen.getByText(/assinatura é pessoal/i)).toBeInTheDocument()
    expect(screen.queryByText(/As escolhas gravam/)).toBeNull()
  })

  it('item 10: cada CLI diz o plano que usa e a não encontrada mostra o comando de instalação', async () => {
    mount()
    await screen.findByText(/Em uso:/)
    expect(screen.getByText('Claude Code').closest('li')).toHaveTextContent('Claude Pro/Max')
    const codex = screen.getByText('Codex CLI').closest('li')!
    expect(codex).toHaveTextContent('ChatGPT Plus/Pro')
    expect(within(codex).getByText('npm i -g @openai/codex')).toBeInTheDocument()
  })

  it('F-0441: o Cursor CLI aparece com o plano, o limite do isolamento, o login e a instalação', async () => {
    fetchLLMConfig.mockResolvedValue(cfg({ clis: [...cfg().clis!, { name: 'cursor-agent', mode: 'cursor-cli', found: false }] }))
    mount()
    await screen.findByText(/Em uso:/)
    const cursor = screen.getByText('Cursor CLI').closest('li')!
    expect(cursor).toHaveTextContent('assinatura do Cursor')
    expect(cursor).toHaveTextContent('regras, hooks e plugins da sua conta Cursor')
    expect(cursor).toHaveTextContent('cursor-agent login')
    expect(within(cursor).getByText('curl https://cursor.com/install -fsS | bash')).toBeInTheDocument()
  })

  it('F-0445: o Copilot CLI aparece com o plano, o que o cerne nega, o login e a instalação', async () => {
    fetchLLMConfig.mockResolvedValue(cfg({ clis: [...cfg().clis!, { name: 'copilot', mode: 'copilot-cli', found: false }] }))
    mount()
    await screen.findByText(/Em uso:/)
    const copilot = screen.getByText('GitHub Copilot CLI').closest('li')!
    expect(copilot).toHaveTextContent('assinatura do GitHub Copilot')
    expect(copilot).toHaveTextContent('nega todas as ferramentas')
    expect(copilot).toHaveTextContent('copilot login')
    expect(within(copilot).getByText('npm install -g @github/copilot')).toBeInTheDocument()
  })

  it('item 12: o campo da chave é new-password e uma chave recusada entra na descrição', async () => {
    fetchLLMConfig.mockResolvedValue(cfg({ key: { env: 'ANTHROPIC_API_KEY', set: false, problem: 'mode 0644' } }))
    mount()
    await screen.findByText(/Em uso:/)
    pick('Chave de API')
    const field = screen.getByLabelText('Chave', { selector: 'input' })
    expect(field).toHaveAttribute('autocomplete', 'new-password')
    expect(field).toHaveAccessibleDescription(/mode 0644/)
  })

  it('critério 11: uma escrita que falha avisa o motivo em um alerta', async () => {
    putSetting.mockRejectedValue(new Error('the lock is busy'))
    mount()
    fireEvent.click(await screen.findByRole('button', { name: 'Usar Gemini CLI' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Não gravou: the lock is busy')
  })

  it('uma escrita que passa entrega a view nova a quem hospeda o painel', async () => {
    const onWritten = mount()
    fireEvent.click(await screen.findByRole('button', { name: 'Usar Gemini CLI' }))
    await waitFor(() => expect(onWritten).toHaveBeenCalledWith(view))
  })

  // ---- round 3 ----
  const projectStored = [reg('llm.mode', { stored: { project: 'claude-cli' } }), ...noStored.slice(1)]

  it('P1.1: no Host, o .cernerc que define llm.mode aparece com o valor e leva ao Projeto num clique', async () => {
    mount(projectStored, 'host')
    const note = await screen.findByText('O .cernerc deste projeto define llm.mode = claude-cli, que vence o Host.')
    expect(note.compareDocumentPosition(screen.getByText(/assinatura é pessoal/i)) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Ir para Projeto' }))
    expect(onScope).toHaveBeenCalledWith('project')
  })

  it('P1.1: sem valor no Projeto, não há aviso de sobreposição', async () => {
    mount(noStored, 'host')
    await screen.findByText(/Em uso:/)
    expect(screen.queryByRole('button', { name: 'Ir para Projeto' })).toBeNull()
  })

  it('P1.1: escrita no Host sobreposta diz "sem efeito aqui" na região viva da linha', async () => {
    putSetting.mockResolvedValue({ settings: projectStored, files: {} })
    mount(projectStored, 'host')
    fireEvent.click(await screen.findByRole('button', { name: 'Usar Gemini CLI' }))
    const msg = await screen.findByText('Salvo no Host — sem efeito aqui: o Projeto define llm.mode.')
    expect(msg.closest('li')).toHaveTextContent('Gemini CLI')
    expect(msg.closest('[aria-live="polite"]')).not.toBeNull()
  })

  it('P1.1/P2.3: escrita no Host anuncia o que ficou em uso e leva o foco ao Testar da linha', async () => {
    fetchLLMConfig.mockResolvedValueOnce(cfg()).mockResolvedValue(cfg({ mode: 'gemini-cli', source: 'configured' }))
    mount(noStored, 'host')
    fireEvent.click(await screen.findByRole('button', { name: 'Usar Gemini CLI' }))
    expect(await screen.findByText('Agora em uso: Assinatura — Gemini CLI · padrão do CLI (salvo no Host).')).toBeInTheDocument()
    await waitFor(() => expect(screen.getByRole('button', { name: 'Testar Gemini CLI' })).toHaveFocus())
  })

  it('P1.1: escrita no Projeto lembra de commitar o .cernerc', async () => {
    mount()
    fireEvent.click(await screen.findByRole('button', { name: 'Usar Gemini CLI' }))
    expect(await screen.findByText(/lembre de commitar/)).toBeInTheDocument()
  })

  it('P2.3: voltar à detecção automática anuncia e põe o foco no título, nunca no body', async () => {
    mount([reg('llm.mode', { stored: { project: 'gemini-cli' } }), ...noStored.slice(1)])
    fireEvent.click(await screen.findByRole('button', { name: 'Voltar à detecção automática' }))
    await waitFor(() => expect(document.activeElement).not.toBe(document.body))
    expect(screen.getByText(/Agora em uso:.*detecção automática/)).toBeInTheDocument()
  })

  it('P1.2b: a CLI em uso diz "Ainda não testado" até um teste rodar nesta página', async () => {
    mount()
    await screen.findByText(/Em uso:/)
    const row = screen.getByText('Claude Code').closest('li')!
    expect(row).toHaveTextContent('Ainda não testado')
    fireEvent.click(within(row).getByRole('button', { name: 'Testar Claude Code' }))
    await within(row).findByText('Claude Code: conexão OK.')
    expect(row).not.toHaveTextContent('Ainda não testado')
  })

  it('P1.2c / P2.7: CLI não encontrada traz instalação, login e o motivo do Usar desabilitado; cópia com nome próprio', async () => {
    mount()
    await screen.findByText(/Em uso:/)
    const codex = screen.getByText('Codex CLI').closest('li')!
    expect(codex).toHaveTextContent('codex login')
    expect(within(codex).getByRole('button', { name: 'Copiar comando de instalação do Codex CLI' })).toBeInTheDocument()
    expect(within(codex).getByRole('button', { name: 'Usar Codex CLI' })).toHaveAccessibleDescription(/não encontrado no PATH.*npm i -g @openai\/codex/)
  })

  it('P2.5: o selo "em uso" não entra no nome do rádio, só na descrição', async () => {
    mount()
    await screen.findByText(/Em uso:/)
    const radio = screen.getByRole('radio', { name: 'Assinatura' })
    expect(radio).toHaveAccessibleDescription(/em uso/)
  })

  it('P2.5: salvar a chave diz como trocar', async () => {
    putLLMKey.mockResolvedValue({ env: 'X', set: true, source: 'file', last4: 'wxyz' })
    mount()
    await screen.findByText(/Em uso:/)
    pick('Chave de API')
    fireEvent.change(screen.getByLabelText('Chave', { selector: 'input' }), { target: { value: 'sk-typed-0000' } })
    fireEvent.click(screen.getByRole('button', { name: 'Salvar chave' }))
    expect(await screen.findByText('Chave salva. Clique em Usar chave de API para trocar.')).toBeInTheDocument()
    await waitFor(() => expect(screen.getByRole('button', { name: 'Testar Chave de API' })).toHaveFocus())
  })

  it('P2.6: ao voltar o foco para a janela, relê a config e a CLI instalada passa a "instalado"', async () => {
    mount()
    await screen.findByText(/Em uso:/)
    expect(screen.getByText('Codex CLI').closest('li')).toHaveTextContent('não encontrado')
    fetchLLMConfig.mockResolvedValue(
      cfg({ clis: [
        { name: 'claude', mode: 'claude-cli', found: true },
        { name: 'codex', mode: 'codex-cli', found: true },
        { name: 'gemini', mode: 'gemini-cli', found: true },
      ] }),
    )
    fireEvent.focus(window)
    await waitFor(() => expect(screen.getByText('Codex CLI').closest('li')).toHaveTextContent('instalado'))
  })

  it('P3.8/9/10: origem em linha própria, "seria Claude Code" e o equivalente no terminal', async () => {
    mount([reg('llm.mode', { stored: { host: 'gemini-cli' } }), ...noStored.slice(1)], 'host')
    const head = (await screen.findByText(/Em uso:/)).closest('div')!
    expect(within(head).getByText('detectado no PATH').tagName).toBe('P')
    expect(screen.getByText(/seria Claude Code/)).toBeInTheDocument()
    expect(screen.getByText('cerne settings set llm.mode <valor> --scope host')).toBeInTheDocument()
  })

  // ---- Gauntlet round 1 ----
  const callOrder = (mock: { mock: { calls: unknown[][] } }, id: string) =>
    mock.mock.calls.findIndex((c) => c[0] === id)

  it('B3: Usar numa CLI limpa model, provider e base_url no escopo e grava llm.mode por último', async () => {
    mount(noStored, 'host')
    fireEvent.click(await screen.findByRole('button', { name: 'Usar Gemini CLI' }))
    await waitFor(() => expect(putSetting).toHaveBeenCalledWith('llm.mode', 'host', 'gemini-cli'))
    for (const id of ['llm.model', 'llm.provider', 'llm.base_url']) expect(unsetSetting).toHaveBeenCalledWith(id, 'host')
    const lastUnset = Math.max(...unsetSetting.mock.invocationCallOrder)
    expect(putSetting.mock.invocationCallOrder[0]).toBeGreaterThan(lastUnset)
  })

  it('B3: voltar à detecção automática limpa o resto e remove llm.mode por último', async () => {
    mount([reg('llm.mode', { stored: { project: 'gemini-cli' } }), ...noStored.slice(1)])
    fireEvent.click(await screen.findByRole('button', { name: 'Voltar à detecção automática' }))
    await waitFor(() => expect(unsetSetting).toHaveBeenCalledWith('llm.mode', 'project'))
    const calls = unsetSetting.mock.calls.map((c) => c[0])
    expect(calls).toEqual(['llm.model', 'llm.provider', 'llm.base_url', 'llm.mode'])
  })

  it('D2: ao usar uma chave de API, llm.mode é a última escrita', async () => {
    fetchLLMConfig.mockResolvedValue(cfg({ key: { env: 'ANTHROPIC_API_KEY', set: true, source: 'env' } }))
    mount()
    await screen.findByText(/Em uso:/)
    pick('Chave de API')
    fireEvent.click(screen.getByRole('button', { name: 'Usar chave de API' }))
    await waitFor(() => expect(putSetting).toHaveBeenCalledWith('llm.mode', 'project', 'byo-key'))
    const writes = putSetting.mock.calls.map((c) => c[0])
    expect(writes[writes.length - 1]).toBe('llm.mode')
    expect(callOrder(putSetting, 'llm.provider')).toBeLessThan(callOrder(putSetting, 'llm.mode'))
  })

  it('D4: a chave de outro provedor, já na variável de ambiente, libera o Usar', async () => {
    fetchLLMConfig.mockResolvedValue(
      cfg({ keys: { openai: { env: 'OPENAI_API_KEY', set: true, source: 'env' }, anthropic: { env: 'ANTHROPIC_API_KEY', set: false } } }),
    )
    mount()
    await screen.findByText(/Em uso:/)
    pick('Chave de API')
    fireEvent.change(screen.getByRole('combobox', { name: 'Provedor' }), { target: { value: 'openai' } })
    expect(screen.getByRole('button', { name: 'Usar chave de API' })).not.toHaveAttribute('aria-disabled', 'true')
    expect(screen.getByText('Chave definida pela variável OPENAI_API_KEY.')).toBeInTheDocument()
  })

  it('M1: o anúncio de voltar à detecção automática continua montado depois que llm.mode some das configurações', async () => {
    fetchLLMConfig.mockResolvedValueOnce(cfg({ source: 'configured', mode: 'gemini-cli' })).mockResolvedValue(cfg())
    const stored = [reg('llm.mode', { stored: { project: 'gemini-cli' } }), ...noStored.slice(1)]
    const ui = (settings: ResolvedSetting[]) => (
      <I18nProvider>
        <AISettings settings={settings} scope="project" onWritten={() => {}} onScope={() => {}} />
      </I18nProvider>
    )
    const { rerender } = render(ui(stored))
    fireEvent.click(await screen.findByRole('button', { name: 'Voltar à detecção automática' }))
    await waitFor(() => expect(unsetSetting).toHaveBeenCalledWith('llm.mode', 'project'))
    rerender(ui(noStored)) // the page re-read the settings: llm.mode is gone
    expect(await screen.findByText(/Agora em uso:.*detecção automática/)).toBeInTheDocument()
  })

  it('M3: só o llm.model gravado no Projeto não avisa que o Projeto sobrepõe o Host', async () => {
    mount([reg('llm.model', { stored: { project: 'x' } }), reg('llm.mode'), reg('llm.provider'), reg('llm.base_url')], 'host')
    await screen.findByText(/Em uso:/)
    expect(screen.queryByRole('button', { name: 'Ir para Projeto' })).toBeNull()
  })

  it('D3: o teste mostra a nota quando uma variável de ambiente vence a chave digitada', async () => {
    testLLM.mockResolvedValue({ ok: true, note: 'OPENAI_API_KEY is set in the environment and wins over a saved key in real use.' } as never)
    mount()
    await screen.findByText(/Em uso:/)
    pick('Chave de API')
    fireEvent.click(screen.getByRole('button', { name: 'Testar Chave de API' }))
    expect(await screen.findByText(/OPENAI_API_KEY is set in the environment/)).toBeInTheDocument()
  })
})
