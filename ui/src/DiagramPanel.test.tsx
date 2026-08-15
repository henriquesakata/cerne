import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import DiagramPanel from './DiagramPanel'
import { I18nProvider, useT } from './i18n'
import { fetchArtifactJSON } from './api'

vi.mock('@excalidraw/excalidraw', () => ({
  Excalidraw: () => <div data-testid="excalidraw" />,
  getSceneVersion: () => 1,
  serializeAsJSON: () => '{}',
}))

vi.mock('./api', () => ({
  fetchArtifactJSON: vi.fn(),
  putArtifact: vi.fn(),
}))

// Troca de idioma pelo fluxo real: mesmo I18nProvider que envolve o painel.
function LangSwitcher() {
  const { setLang } = useT()
  return <button onClick={() => setLang('en')}>trocar-idioma</button>
}

function renderPanel() {
  return render(
    <I18nProvider>
      <LangSwitcher />
      <DiagramPanel
        featureId="F-0001"
        name="fluxo.excalidraw"
        isNew={false}
        onClose={() => {}}
        onSaved={() => {}}
      />
    </I18nProvider>,
  )
}

describe('DiagramPanel', () => {
  beforeEach(() => {
    localStorage.setItem('cerne-lang', 'pt-BR')
  })

  afterEach(() => {
    cleanup()
    localStorage.clear()
    vi.clearAllMocks()
  })

  it('mostra o erro de carga no corpo, não preso em "Carregando"', async () => {
    vi.mocked(fetchArtifactJSON).mockRejectedValueOnce(new Error('boom'))
    renderPanel()
    const err = await screen.findByText('Erro ao carregar o diagrama: boom')
    // O erro de carga pertence ao corpo do painel, não à toolbar de save.
    expect(err.closest('.diagram-controls')).toBeNull()
    expect(screen.queryByText('Carregando diagrama…')).not.toBeInTheDocument()
  })

  it('não re-busca o artefato ao trocar o idioma', async () => {
    vi.mocked(fetchArtifactJSON).mockResolvedValue(
      JSON.stringify({ elements: [], appState: {} }),
    )
    renderPanel()
    await waitFor(() =>
      expect(screen.queryByText('Carregando diagrama…')).not.toBeInTheDocument(),
    )
    fireEvent.click(screen.getByText('trocar-idioma'))
    await waitFor(() => expect(screen.getByTestId('excalidraw')).toBeInTheDocument())
    expect(fetchArtifactJSON).toHaveBeenCalledTimes(1)
  })
})
