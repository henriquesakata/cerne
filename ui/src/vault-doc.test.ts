import { beforeEach, describe, expect, it, vi } from 'vitest'

const api = vi.hoisted(() => ({
  fetchFeature: vi.fn(),
  fetchDecision: vi.fn(),
  fetchNote: vi.fn(),
  fetchRunbook: vi.fn(),
  fetchReference: vi.fn(),
  fetchWikiPage: vi.fn(),
}))
vi.mock('./api', () => api)

import { loadVaultDoc } from './vault-doc'

const feature = (state: string) => ({
  feature: { id: 'F-1', title: 'T', state },
  docs: { spec: 'SPEC', plan: 'PLAN', result: 'RESULT' },
})

beforeEach(() => vi.clearAllMocks())

describe('loadVaultDoc (F-0463)', () => {
  it.each([
    ['candidate', 'spec', 'SPEC'],
    ['approved', 'spec', 'SPEC'],
    ['in_development', 'plan', 'PLAN'],
    ['implemented', 'result', 'RESULT'],
    ['done', 'result', 'RESULT'],
    ['rejected', 'spec', 'SPEC'],
    ['superseded', 'spec', 'SPEC'],
  ])('stage: feature %s opens %s', async (state, doc, content) => {
    api.fetchFeature.mockResolvedValue(feature(state))
    const d = await loadVaultDoc('F-1')
    expect(d).toMatchObject({ id: 'F-1', kind: 'feature', doc, content, state, title: 'T' })
  })

  it('stage: an explicit doc wins and historical follows the state', async () => {
    api.fetchFeature.mockResolvedValue(feature('done'))
    const d = await loadVaultDoc('F-1', 'spec')
    expect(d).toMatchObject({ doc: 'spec', content: 'SPEC', historical: true })
    api.fetchFeature.mockResolvedValue(feature('approved'))
    expect((await loadVaultDoc('F-1')).historical).toBe(false)
  })

  it('routes epics/tasks/bugs to the feature endpoint', async () => {
    api.fetchFeature.mockResolvedValue(feature('approved'))
    for (const id of ['E-1', 'T-1', 'B-1']) await loadVaultDoc(id)
    expect(api.fetchFeature.mock.calls.map((c) => c[0])).toEqual(['E-1', 'T-1', 'B-1'])
  })

  it.each([
    ['D-1', 'fetchDecision', 'decision', 'decision'],
    ['N-1', 'fetchNote', 'note', 'note'],
    ['RB-1', 'fetchRunbook', 'runbook', 'runbook'],
    ['R-1', 'fetchReference', 'reference', 'reference'],
  ])('hash: %s reads its own route', async (id, fn, kind, key) => {
    api[fn as keyof typeof api].mockResolvedValue({ [key]: { id, title: 'X', state: 'superseded' }, doc: 'BODY' })
    const d = await loadVaultDoc(id)
    expect(d).toMatchObject({ id, kind, content: 'BODY', title: 'X', state: 'superseded', historical: true })
    expect(d.doc).toBeUndefined()
  })

  it('wiki ids read the wiki page', async () => {
    api.fetchWikiPage.mockResolvedValue({ rel: 'modules/x.md', body: 'W' })
    const d = await loadVaultDoc('wiki:modules/x.md')
    expect(api.fetchWikiPage).toHaveBeenCalledWith('modules/x.md')
    expect(d).toMatchObject({ kind: 'wiki', content: 'W', historical: false, title: 'x.md' })
  })

  it('rejects an id with no vault route', async () => {
    await expect(loadVaultDoc('W-0001')).rejects.toThrow(/W-0001/)
  })
})
