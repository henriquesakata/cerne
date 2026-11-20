import { readFileSync } from 'node:fs'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import Mirror from './Mirror'
import type { MirrorCheckOutcome, MirrorCommits, MirrorOverview } from './api'
import { I18nProvider } from './i18n'

const api = vi.hoisted(() => ({
  fetchMirror: vi.fn<() => Promise<MirrorOverview>>(),
  postMirrorCheck: vi.fn<() => Promise<MirrorCheckOutcome>>(),
  fetchMirrorCommits: vi.fn<(id: string) => Promise<MirrorCommits>>(),
}))
vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  fetchMirror: api.fetchMirror,
  postMirrorCheck: api.postMirrorCheck,
  fetchMirrorCommits: api.fetchMirrorCommits,
}))
vi.mock('./live', () => ({ useLiveVersion: () => 0 }))

const overview: MirrorOverview = {
  enabled: true,
  trackers: [
    { name: 'alpha', kind: 'github', url: 'https://github.com/o/alpha' },
    { name: 'beta', kind: 'jira', url: 'https://beta.atlassian.net/jira/o' },
  ],
  cards: [
    { id: 'F-0001', title: 'Card one', state: 'candidate', tracker: 'alpha', ref: { id: '12', url: 'https://github.com/o/alpha/issues/12' } },
    { id: 'F-0003', title: 'Card three', state: 'in_development', tracker: 'alpha', ref: { id: '14', url: 'https://github.com/o/alpha/issues/14' } },
    { id: 'F-0002', title: 'Card two', state: 'done', tracker: 'beta', ref: { id: 'PROJ-7', url: 'https://beta.atlassian.net/browse/PROJ-7' } },
  ],
  ideas: [
    { id: 'I-0001', title: 'Crash on start', state: 'active', tracker: 'alpha', issueId: '13', url: 'https://github.com/o/alpha/issues/13/?x=1' },
    { id: 'I-0002', title: 'Old request', state: 'dropped', tracker: 'beta', issueId: 'PROJ-3', url: 'https://beta.atlassian.net/browse/PROJ-3' },
  ],
  lastSync: {
    at: new Date(Date.now() - 3 * 3600_000).toISOString(),
    trackers: [
      { name: 'alpha', synced: 2, imported: 1, errors: [] },
      { name: 'beta', synced: 1, imported: 0, errors: ['F-0002: boom', 'list: down'] },
    ],
  },
}

function renderMirror(lang: 'en' | 'pt-BR' = 'en') {
  localStorage.setItem('cerne-lang', lang)
  return render(
    <I18nProvider>
      <Mirror />
    </I18nProvider>,
  )
}

const section = (name: string) => screen.getByRole('region', { name })
const toggle = (id: string) => screen.getByRole('button', { name: new RegExp(`^${id}`) })

// A promise the test settles by hand, to see the pending state.
function deferred<T>() {
  let resolve!: (v: T) => void
  const promise = new Promise<T>((r) => (resolve = r))
  return { promise, resolve }
}

const checked: MirrorCheckOutcome = {
  kind: 'ok',
  results: [
    { id: 'F-0001', status: 'ok' },
    { id: 'F-0003', status: 'drift', reason: 'closed-but-card-open' },
    { id: 'F-0002', status: 'unchecked', reason: 'tracker-failed', detail: 'boom: 503' },
  ],
}

beforeEach(() => {
  cleanup()
  localStorage.clear()
  api.fetchMirror.mockReset().mockResolvedValue(overview)
  api.postMirrorCheck.mockReset().mockResolvedValue(checked)
  api.fetchMirrorCommits.mockReset().mockResolvedValue({ repo: true, commits: [] })
})
afterEach(() => localStorage.clear())

describe('Mirror — the list (F-0465 crit. 1)', () => {
  it('groups the cards and the imported ideas by tracker', async () => {
    renderMirror()
    expect(await screen.findByRole('heading', { level: 1, name: 'Mirror' })).toBeInTheDocument()
    const alpha = within(await screen.findByRole('region', { name: 'alpha' }))
    expect(alpha.getByText('github')).toBeInTheDocument()
    expect(alpha.getByRole('link', { name: 'github.com/o/alpha' })).toHaveAttribute('href', 'https://github.com/o/alpha')
    expect(alpha.getByText('Card one')).toBeInTheDocument()
    expect(alpha.getByText('Card three')).toBeInTheDocument()
    expect(alpha.queryByText('Card two')).toBeNull()
    expect(alpha.getByText('2 mirrored cards')).toBeInTheDocument()
    expect(alpha.getByText('Crash on start')).toBeInTheDocument()
    expect(alpha.getByRole('link', { name: 'from #13' })).toHaveAttribute('href', 'https://github.com/o/alpha/issues/13/?x=1')
    expect(within(section('beta')).getByRole('link', { name: 'from PROJ-3' })).toBeInTheDocument()
    // an active idea carries no chip; a closed one does
    const beta = within(section('beta'))
    expect(beta.getByText('Card two')).toBeInTheDocument()
    expect(beta.getByText('1 mirrored card')).toBeInTheDocument()
    expect(beta.getByText('Dropped')).toBeInTheDocument()
    expect(alpha.queryByText('Dropped')).toBeNull()
    expect(beta.getByText('Old request')).toBeInTheDocument()
  })

  it('shows the issue as #n or as its key, with an accessible name', async () => {
    renderMirror()
    await screen.findByRole('region', { name: 'alpha' })
    expect(screen.getByRole('link', { name: 'Open issue #12 on alpha' })).toHaveAttribute('href', 'https://github.com/o/alpha/issues/12')
    expect(screen.getByRole('link', { name: 'Open issue PROJ-7 on beta' })).toHaveTextContent('PROJ-7')
    expect(screen.getByText('Candidate')).toHaveClass('chip', 'chip-candidate')
  })

  it('a card of a tracker no longer configured goes to its own group', async () => {
    api.fetchMirror.mockResolvedValue({
      ...overview,
      cards: [...overview.cards, { id: 'F-0009', title: 'Orphan', state: 'approved', tracker: 'gone', ref: { id: '1', url: 'https://x/1' } }],
    })
    renderMirror()
    const g = within(await screen.findByRole('region', { name: 'Trackers no longer configured' }))
    expect(g.getByText('Orphan')).toBeInTheDocument()
    expect(g.getByText('gone')).toBeInTheDocument()
  })

  it('mirror off: the empty state points to Integrations', async () => {
    api.fetchMirror.mockResolvedValue({ enabled: false, trackers: [], cards: [], ideas: [], lastSync: null })
    renderMirror()
    expect(await screen.findByText('The mirror is off.')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Open Integrations' })).toHaveAttribute('href', '#/settings/integrations')
    expect(screen.queryByRole('button', { name: 'Check now' })).toBeNull()
  })

  it('mirror on with nothing published: the other empty state, commands in code', async () => {
    api.fetchMirror.mockResolvedValue({ enabled: true, trackers: overview.trackers, cards: [], ideas: [], lastSync: null })
    renderMirror()
    expect(await screen.findByText('No card published yet.')).toBeInTheDocument()
    expect(screen.getByText('cerne feature mirror F-…').tagName).toBe('CODE')
    // once in the never-synced line, once in the empty state
    const syncs = screen.getAllByText('cerne mirror sync')
    expect(syncs).toHaveLength(2)
    syncs.forEach((c) => expect(c.tagName).toBe('CODE'))
    expect(screen.getByRole('link', { name: 'Open Integrations' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Check now' })).toBeNull()
  })

  it('a failed load with nothing on screen shows the error with a retry', async () => {
    api.fetchMirror.mockRejectedValue(new Error('down'))
    renderMirror()
    expect(await screen.findByText('Could not load the mirror.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Try again' })).toBeInTheDocument()
  })
})

describe('Mirror — links (F-0465)', () => {
  it('links only http(s) URLs: anything else shows the same text without a link', async () => {
    api.fetchMirror.mockResolvedValue({
      ...overview,
      trackers: [{ name: 'alpha', kind: 'github', url: 'javascript:alert(0)' }],
      cards: [{ ...overview.cards[0], ref: { id: '12', url: 'javascript:alert(1)' } }],
      ideas: [{ ...overview.ideas[0], url: 'data:text/html,x' }],
    })
    api.fetchMirrorCommits.mockResolvedValue({ repo: true, commits: [{ sha: 'abc1234', subject: 's', url: 'javascript:alert(2)' }] })
    renderMirror()
    const alpha = within(await screen.findByRole('region', { name: 'alpha' }))
    fireEvent.click(alpha.getByRole('button', { name: /^F-0001/ }))
    await screen.findByText('abc1234')
    for (const a of Array.from(document.querySelectorAll('a'))) expect(a.getAttribute('href')).not.toMatch(/^(javascript|data):/i)
    expect(alpha.queryByRole('link', { name: 'Open issue #12 on alpha' })).toBeNull()
    expect(alpha.getByText('#12')).toBeInTheDocument()
    expect(alpha.getByText('javascript:alert(0)')).toBeInTheDocument()
    expect(screen.getByText('abc1234').closest('a')).toBeNull()
  })

  it('accepts HTTPS in any case', async () => {
    api.fetchMirror.mockResolvedValue({
      ...overview,
      cards: [{ ...overview.cards[0], ref: { id: '12', url: 'HTTPS://github.com/o/alpha/issues/12' } }],
    })
    renderMirror()
    expect(await screen.findByRole('link', { name: 'Open issue #12 on alpha' })).toBeInTheDocument()
  })
})

describe('Mirror — last sync (F-0465 crit. 4)', () => {
  it('shows when, the counts and the failures of the last sync on this computer', async () => {
    renderMirror()
    const line = await screen.findByText(/Last sync on this computer:/)
    expect(line).toHaveTextContent('3 cards synced, 1 idea imported')
    expect(line).toHaveTextContent('beta: 2 failures')
    expect(line).not.toHaveTextContent('alpha:')
    expect(line).toHaveTextContent('UTC')
  })

  it('shows the record per tracker, each error on its own line', async () => {
    renderMirror()
    const alpha = within(await screen.findByRole('region', { name: 'alpha' }))
    expect(alpha.getByText("This tracker's last sync: 2 cards synced, 1 idea imported.")).toBeInTheDocument()
    expect(alpha.queryByText(/boom|down/)).toBeNull()
    const beta = within(section('beta'))
    expect(beta.getByText("This tracker's last sync: 1 card synced, 0 ideas imported.")).toBeInTheDocument()
    expect(beta.getByText('F-0002: boom').tagName).toBe('LI')
    expect(beta.getByText('list: down').tagName).toBe('LI')
    expect(alpha.queryByText('F-0002: boom')).toBeNull()
  })

  it('a tracker with no entry in the record, or no record at all, shows no per-tracker line', async () => {
    api.fetchMirror.mockResolvedValue({ ...overview, lastSync: { ...overview.lastSync!, trackers: [overview.lastSync!.trackers[0]] } })
    renderMirror()
    await screen.findByRole('region', { name: 'alpha' })
    expect(within(section('alpha')).getByText(/This tracker's last sync/)).toBeInTheDocument()
    expect(within(section('beta')).queryByText(/This tracker's last sync/)).toBeNull()
    cleanup()
    api.fetchMirror.mockResolvedValue({ ...overview, lastSync: null })
    renderMirror()
    await screen.findByRole('region', { name: 'alpha' })
    expect(screen.queryByText(/This tracker's last sync/)).toBeNull()
  })

  it('never synced', async () => {
    api.fetchMirror.mockResolvedValue({ ...overview, lastSync: null })
    renderMirror()
    const line = await screen.findByText(/Sync has never run on this computer/)
    expect(within(line).getByText('cerne mirror sync').tagName).toBe('CODE')
  })
})

describe('Mirror — Check now (F-0465 crit. 2)', () => {
  it('is not called on mount', async () => {
    renderMirror()
    await screen.findByRole('region', { name: 'alpha' })
    expect(api.postMirrorCheck).not.toHaveBeenCalled()
    expect(api.fetchMirrorCommits).not.toHaveBeenCalled()
  })

  it('is busy and disabled while it runs, then shows the three verdicts and the status', async () => {
    const d = deferred<MirrorCheckOutcome>()
    api.postMirrorCheck.mockReturnValue(d.promise)
    renderMirror()
    const button = await screen.findByRole('button', { name: 'Check now' })
    fireEvent.click(button)
    const busy = await screen.findByRole('button', { name: 'Checking…' })
    expect(busy).toBeDisabled()
    expect(busy).toHaveAttribute('aria-busy', 'true')
    expect(screen.queryByText('up to date')).toBeNull()

    d.resolve(checked)
    const done = await screen.findByRole('button', { name: 'Check now' })
    expect(done).toBeEnabled()
    expect(done).not.toHaveAttribute('aria-busy', 'true')
    await waitFor(() =>
      expect(screen.getByRole('status')).toHaveTextContent(/^Checked at \d\d:\d\d: 1 up to date, 1 drifted, 1 not checked\.$/),
    )

    const ok = within(toggle('F-0001').closest('li')!)
    expect(ok.getByText('up to date')).not.toHaveClass('chip')
    const drift = within(toggle('F-0003').closest('li')!)
    expect(drift.getByText('drifted')).toHaveClass('chip', 'chip-lint')
    expect(drift.getByText('issue not among the open ones, card open')).toBeInTheDocument()
    const unchecked = within(toggle('F-0002').closest('li')!)
    expect(unchecked.getByText(/not checked/)).toHaveTextContent('not checked — the tracker did not answer')
    expect(unchecked.getByText(/not checked/)).not.toHaveClass('chip')
    // the tracker's own text is in the open row, not in the meta line
    expect(screen.queryByText('boom: 503')).toBeNull()
    fireEvent.click(toggle('F-0002'))
    expect(screen.getByText('Reason given by the tracker:')).toBeInTheDocument()
    expect(screen.getByText('boom: 503').tagName).toBe('CODE')
  })

  it('409 says the mirror is off', async () => {
    api.postMirrorCheck.mockResolvedValue({ kind: 'off' })
    renderMirror()
    fireEvent.click(await screen.findByRole('button', { name: 'Check now' }))
    await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('The mirror is off: there is nothing to check.'))
  })

  it('a failed check keeps the previous results', async () => {
    renderMirror()
    fireEvent.click(await screen.findByRole('button', { name: 'Check now' }))
    await screen.findAllByText('drifted')
    api.postMirrorCheck.mockResolvedValue({ kind: 'error', message: 'HTTP 500' })
    fireEvent.click(screen.getByRole('button', { name: 'Check now' }))
    await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('The check failed: HTTP 500'))
    expect(screen.getByText('drifted')).toBeInTheDocument()
    expect(screen.getByText('up to date')).toBeInTheDocument()
  })
})

describe('Mirror — list cap in Check now (F-0469 crits. 3 and 4)', () => {
  const capped: MirrorCheckOutcome = {
    kind: 'ok',
    results: [
      { id: 'F-0001', status: 'unchecked', reason: 'list-capped' },
      { id: 'F-0003', status: 'unchecked', reason: 'list-capped' },
      { id: 'F-0002', status: 'ok' },
    ],
  }

  it('names the tracker, the 100 cap and the count, in English', async () => {
    api.postMirrorCheck.mockResolvedValue(capped)
    renderMirror()
    fireEvent.click(await screen.findByRole('button', { name: 'Check now' }))
    const line = await screen.findByText(/alpha: .*100.* 2 cards/)
    expect(line).toBeInTheDocument()
    expect(screen.queryByText(/beta: .*100/)).toBeNull()
  })

  it('names the tracker, the 100 cap and the count, in pt-BR', async () => {
    api.postMirrorCheck.mockResolvedValue(capped)
    renderMirror('pt-BR')
    fireEvent.click(await screen.findByRole('button', { name: 'Conferir agora' }))
    expect(await screen.findByText(/alpha: .*100.* 2 cards/)).toBeInTheDocument()
    expect(screen.queryByText(/beta: .*100/)).toBeNull()
  })

  it('shows no line when no result is list-capped', async () => {
    renderMirror()
    fireEvent.click(await screen.findByRole('button', { name: 'Check now' }))
    await screen.findAllByText('drifted')
    expect(screen.queryByText(/100/)).toBeNull()
  })
})

describe('Mirror — commits of a card (F-0465 crit. 3)', () => {
  it('opens by a native button: one fetch on first open, none when it closes and opens again', async () => {
    renderMirror()
    const b = await screen.findByRole('button', { name: /^F-0001/ })
    // A native button: Enter and Space activate it with no key handler.
    expect(b.tagName).toBe('BUTTON')
    expect(b).toHaveAttribute('type', 'button')
    expect(b).toHaveAttribute('aria-expanded', 'false')
    b.focus()
    expect(b).toHaveFocus()
    fireEvent.click(b)
    expect(b).toHaveAttribute('aria-expanded', 'true')
    expect(b.getAttribute('aria-controls')).toBeTruthy()
    expect(document.getElementById(b.getAttribute('aria-controls')!)).toBeInTheDocument()
    await waitFor(() => expect(api.fetchMirrorCommits).toHaveBeenCalledTimes(1))
    expect(api.fetchMirrorCommits).toHaveBeenCalledWith('F-0001')
    fireEvent.click(b)
    fireEvent.click(b)
    expect(api.fetchMirrorCommits).toHaveBeenCalledTimes(1)
  })

  it('shows the card link and, while loading, the loading line', async () => {
    const d = deferred<MirrorCommits>()
    api.fetchMirrorCommits.mockReturnValue(d.promise)
    renderMirror()
    fireEvent.click(await screen.findByRole('button', { name: /^F-0001/ }))
    expect(screen.getByRole('link', { name: 'Open F-0001' })).toHaveAttribute('href', '#/f/F-0001')
    expect(screen.getByRole('heading', { name: 'Commits citing F-0001' })).toBeInTheDocument()
    expect(screen.getByText('Loading commits…')).toBeInTheDocument()
    d.resolve({ repo: true, commits: [] })
    expect(await screen.findByText(/No commit in this repository cites F-0001/)).toBeInTheDocument()
  })

  it('lists the commits, linked when the server gave a url, with the clone hint', async () => {
    api.fetchMirrorCommits.mockResolvedValue({
      repo: true,
      commits: [
        { sha: 'abc1234', subject: 'feat: one', url: 'https://github.com/o/r/commit/abc1234ffff' },
        { sha: 'def5678', subject: 'fix: two' },
      ],
    })
    renderMirror()
    fireEvent.click(await screen.findByRole('button', { name: /^F-0001/ }))
    const link = await screen.findByRole('link', { name: 'abc1234' })
    expect(link).toHaveAttribute('href', 'https://github.com/o/r/commit/abc1234ffff')
    expect(link).toHaveAttribute('rel', 'noreferrer')
    expect(screen.getByText('feat: one')).toBeInTheDocument()
    expect(screen.getByText('def5678').closest('a')).toBeNull()
    expect(screen.getByText('fix: two')).toBeInTheDocument()
    expect(screen.queryByText('Showing the 20 most recent.')).toBeNull()
    expect(screen.getByText(/Only this clone's commits/)).toBeInTheDocument()
  })

  it('not a git repository', async () => {
    api.fetchMirrorCommits.mockResolvedValue({ repo: false, commits: [] })
    renderMirror()
    fireEvent.click(await screen.findByRole('button', { name: /^F-0001/ }))
    expect(await screen.findByText('The project is not a git repository: there are no commits to list.')).toBeInTheDocument()
  })

  it('twenty commits carry the cap note', async () => {
    api.fetchMirrorCommits.mockResolvedValue({
      repo: true,
      commits: Array.from({ length: 20 }, (_, i) => ({ sha: `sha${i}`, subject: `step ${i}` })),
    })
    renderMirror()
    fireEvent.click(await screen.findByRole('button', { name: /^F-0001/ }))
    expect(await screen.findByText('Showing the 20 most recent.')).toBeInTheDocument()
  })

  it('an error is shown as a form error', async () => {
    api.fetchMirrorCommits.mockRejectedValue(new Error('git failed'))
    renderMirror()
    fireEvent.click(await screen.findByRole('button', { name: /^F-0001/ }))
    const e = await screen.findByText('Could not read the commits: git failed')
    expect(e).toHaveClass('form-error')
  })

  it('a failed commit list is fetched again on the next open', async () => {
    api.fetchMirrorCommits.mockRejectedValueOnce(new Error('git failed')).mockResolvedValue({
      repo: true,
      commits: [{ sha: 'abc1234', subject: 'feat: one' }],
    })
    renderMirror()
    const b = await screen.findByRole('button', { name: /^F-0001/ })
    fireEvent.click(b)
    expect(await screen.findByText('Could not read the commits: git failed')).toBeInTheDocument()
    fireEvent.click(b)
    fireEvent.click(b)
    expect(await screen.findByText('feat: one')).toBeInTheDocument()
    expect(api.fetchMirrorCommits).toHaveBeenCalledTimes(2)
  })

  it('Expand all opens every row of that tracker and fetches each once', async () => {
    renderMirror()
    await screen.findByRole('region', { name: 'alpha' })
    fireEvent.click(within(section('alpha')).getByRole('button', { name: 'Expand all' }))
    await waitFor(() => expect(api.fetchMirrorCommits).toHaveBeenCalledTimes(2))
    expect(api.fetchMirrorCommits.mock.calls.map((c) => c[0]).sort()).toEqual(['F-0001', 'F-0003'])
  })
})

describe('Mirror — both languages (F-0465 crit. 6)', () => {
  it('renders in pt-BR', async () => {
    renderMirror('pt-BR')
    expect(await screen.findByRole('heading', { level: 1, name: 'Espelho' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Conferir agora' })).toBeInTheDocument()
    expect(screen.getByText(/Último sync neste computador:/)).toBeInTheDocument()
    expect(screen.getByText('2 cards espelhados')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Abrir a issue #12 em alpha' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /^F-0001/ }))
    expect(await screen.findByRole('heading', { name: 'Commits que citam F-0001' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Conferir agora' }))
    await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent(/^Conferido às \d\d:\d\d: 1 em dia, 1 divergentes, 1 não conferidos\.$/))
    expect(screen.getByText('divergente')).toHaveClass('chip-lint')
  })

  it('renders in English', async () => {
    renderMirror('en')
    expect(await screen.findByRole('heading', { level: 1, name: 'Mirror' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Check now' })).toBeInTheDocument()
  })

  it('pt-BR empty states', async () => {
    api.fetchMirror.mockResolvedValue({ enabled: false, trackers: [], cards: [], ideas: [], lastSync: null })
    renderMirror('pt-BR')
    expect(await screen.findByText('O espelho está desligado.')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Abrir Integrações' })).toBeInTheDocument()
  })
})

// The generic .queue-btn hover (0,3,0) would repaint the primary button's
// background while its text stays the page colour: the label vanishes. The
// primary keeps its full ink on hover.
describe('Mirror — the primary queue button on hover', () => {
  it('has its own hover rule that keeps the ink', () => {
    const css = readFileSync('src/app.css', 'utf8')
    const m = /(^|\n)\.queue-btn-primary:hover:not\(:disabled\) \{([^}]*)\}/.exec(css)
    expect(m).not.toBeNull()
    expect(m![2]).toContain('background: var(--fg)')
    expect(m![2]).toContain('border-color: var(--fg)')
  })
})
