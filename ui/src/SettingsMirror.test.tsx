import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import Settings from './Settings'
import { HTTPError, type ResolvedSetting, type SettingScope, type SettingsView } from './api'

// F-0449, the parts of T10 the acceptance batch does not name: the human-only
// toggle asks when it leaves its default (here: turning the mirror on), the
// registry's max_len caps a text field, a records refusal shows the server's
// reason, and the empty records state.

const fetchSettings = vi.fn<() => Promise<SettingsView>>()
const putSetting = vi.fn<(id: string, scope: SettingScope, value: unknown) => Promise<SettingsView>>()
vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  fetchSettings: () => fetchSettings(),
  putSetting: (id: string, scope: SettingScope, value: unknown) => putSetting(id, scope, value),
  unsetSetting: () => fetchSettings(),
}))
vi.mock('./live', () => ({ useProjects: () => ({ projects: [{ pid: 'p1', name: 'A' }], activePid: 'p1' }), useLiveVersion: () => 0 }))

const key = (over: Partial<ResolvedSetting> & Pick<ResolvedSetting, 'id'>): ResolvedSetting => ({
  title: over.id,
  type: 'bool',
  default: false,
  scopes: ['project'],
  description: '',
  value: false,
  origin: 'default',
  group: 'integrations',
  ...over,
})
const field = (id: string, title: string) => ({ id, title, type: 'string', default: '', scopes: ['project'] as SettingScope[], description: '' })
const view = (settings: ResolvedSetting[]): SettingsView => ({ files: { project: '/repo/.cernerc' }, settings })
const row = (title: string) => screen.getByRole('listitem', { name: title })

beforeEach(() => {
  cleanup()
  localStorage.clear()
  fetchSettings.mockReset()
  putSetting.mockReset()
})

describe('Settings — mirror pieces of T10 (F-0449)', () => {
  it('turning a human-only key on, off its default, asks first; Ligar writes, focus starts on Cancel, Escape cancels', async () => {
    const mirror = key({ id: 'mirror.enabled', title: 'Mirror (unit)', human_only: true })
    fetchSettings.mockResolvedValue(view([mirror]))
    putSetting.mockResolvedValue(view([mirror]))
    render(<Settings group="integrations" />)
    const r = await screen.findByRole('listitem', { name: 'Mirror (unit)' })
    fireEvent.click(within(r).getByRole('checkbox', { name: 'Mirror (unit)' }))
    const ask = within(r).getByRole('group')
    expect(ask).toHaveTextContent('Ligar Mirror (unit)? A partir de agora, um card que um humano marcar como público é publicado nos trackers abaixo')
    expect(ask).toHaveTextContent('Isto grava em .cernerc.')
    expect(within(ask).getByRole('button', { name: 'Cancelar' })).toHaveFocus()
    fireEvent.keyDown(within(ask).getByRole('button', { name: 'Cancelar' }), { key: 'Escape' })
    expect(within(r).queryByRole('group')).toBeNull()
    expect(putSetting).not.toHaveBeenCalled()

    fireEvent.click(within(r).getByRole('checkbox', { name: 'Mirror (unit)' }))
    fireEvent.click(within(within(r).getByRole('group')).getByRole('button', { name: 'Ligar' }))
    await waitFor(() => expect(putSetting).toHaveBeenCalledWith('mirror.enabled', 'project', true))
  })

  it('back to the default writes at once', async () => {
    const mirror = key({ id: 'mirror.enabled', title: 'Mirror (unit)', human_only: true, value: true, origin: 'project', stored: { project: true } })
    fetchSettings.mockResolvedValue(view([mirror]))
    putSetting.mockResolvedValue(view([mirror]))
    render(<Settings group="integrations" />)
    const r = await screen.findByRole('listitem', { name: 'Mirror (unit)' })
    fireEvent.click(within(r).getByRole('checkbox', { name: 'Mirror (unit)' }))
    expect(within(r).queryByRole('group')).toBeNull()
    await waitFor(() => expect(putSetting).toHaveBeenCalledWith('mirror.enabled', 'project', false))
  })

  it('a text field takes the key max_len, 64 without one', async () => {
    fetchSettings.mockResolvedValue(
      view([
        key({ id: 'delivery.commit_url', title: 'Commit URL (unit)', type: 'string', default: '', value: '', max_len: 256 }),
        key({ id: 'x.name', title: 'Name (unit)', type: 'string', default: '', value: '' }),
      ]),
    )
    render(<Settings group="integrations" />)
    await screen.findByRole('listitem', { name: 'Commit URL (unit)' })
    expect(within(row('Commit URL (unit)')).getByRole('textbox')).toHaveAttribute('maxLength', '256')
    expect(within(row('Name (unit)')).getByRole('textbox')).toHaveAttribute('maxLength', '64')
  })

  it('records: the effective line counts and names the trackers; an empty list reads nenhum', async () => {
    const fields = [field('name', 'Tracker name'), { ...field('kind', 'Tracker kind'), enum: ['github'] }, field('url', 'Issues URL')]
    const rows = [
      { name: 'pub', kind: 'github', url: 'https://github.com/o/pub' },
      { name: 'internal', kind: 'github', url: 'https://github.com/o/internal' },
    ]
    fetchSettings.mockResolvedValue(
      view([
        key({ id: 'mirror.trackers', title: 'Two (unit)', type: 'records', default: [], fields, value: rows, origin: 'project', stored: { project: rows } }),
        key({ id: 'x.recs', title: 'One (unit)', type: 'records', default: [], fields, value: rows.slice(0, 1), origin: 'project', stored: { project: rows.slice(0, 1) } }),
        key({ id: 'y.recs', title: 'None (unit)', type: 'records', default: [], fields, value: [] }),
      ]),
    )
    render(<Settings group="integrations" />)
    await screen.findByRole('listitem', { name: 'Two (unit)' })
    expect(row('Two (unit)')).toHaveTextContent('Efetivo: 2 trackers: pub, internal, de Projeto')
    expect(row('One (unit)')).toHaveTextContent('Efetivo: 1 tracker: pub, de Projeto')
    expect(row('None (unit)')).toHaveTextContent('Efetivo: nenhum (padrão)')
    expect(row('None (unit)')).toHaveTextContent('Padrão: nenhum')
    expect(row('Two (unit)')).not.toHaveTextContent('[{')
  })

  it('close_on: the select and the effective line read the option labels, not accept and merge', async () => {
    fetchSettings.mockResolvedValue(
      view([key({ id: 'mirror.close_on', title: 'Close on (unit)', type: 'string', default: 'accept', enum: ['accept', 'merge'], value: 'accept' })]),
    )
    render(<Settings group="integrations" />)
    const r = await screen.findByRole('listitem', { name: 'Close on (unit)' })
    const select = within(r).getByRole('combobox') as HTMLSelectElement
    expect([...select.options].map((o) => o.textContent)).toEqual(['no aceite humano', 'no merge do PR'])
    expect(r).toHaveTextContent('Efetivo: no aceite humano (padrão)')
  })

  it('records: empty list keeps the header and says nothing is published; Add focuses the new first field; a refusal shows the server reason', async () => {
    const trackers = key({
      id: 'mirror.trackers',
      title: 'Trackers (unit)',
      type: 'records',
      default: [],
      value: [],
      human_only: true,
      fields: [field('name', 'Tracker name'), { ...field('kind', 'Tracker kind'), enum: ['github'] }, field('url', 'Issues URL'), field('label', 'Import label')],
    })
    const reason = 'mirror.trackers: invalid setting value: record 1: name is required'
    fetchSettings.mockResolvedValue(view([trackers]))
    putSetting.mockRejectedValue(new HTTPError(reason, 400))
    render(<Settings group="integrations" />)
    const r = await screen.findByRole('listitem', { name: 'Trackers (unit)' })
    const table = within(r).getByRole('table')
    expect(within(table).getAllByRole('columnheader').map((h) => h.textContent)).toEqual(['Tracker name', 'Tracker kind', 'Issues URL', 'Import label', 'Ações'])
    expect(within(table).getByText('Nenhum tracker. Nada é publicado até haver um.')).toBeInTheDocument()

    fireEvent.click(within(r).getByRole('button', { name: 'Incluir tracker' }))
    await waitFor(() => expect(within(r).getByRole('textbox', { name: 'Tracker name, linha 1' })).toHaveFocus())
    expect((within(r).getByRole('combobox', { name: 'Tracker kind, linha 1' }) as HTMLSelectElement).value).toBe('github')
    expect(within(r).getByRole('button', { name: 'Remover linha 1' })).toBeInTheDocument()

    fireEvent.click(within(r).getByRole('button', { name: 'Salvar' }))
    await waitFor(() => expect(putSetting).toHaveBeenCalledWith('mirror.trackers', 'project', [{ kind: 'github' }]))
    expect(await within(r).findByText(reason)).toBeInTheDocument()
    expect(within(r).getByText(reason).tagName).toBe('CODE')
  })
})
