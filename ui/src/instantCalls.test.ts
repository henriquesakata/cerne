import { readdirSync, readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

// F-0493 crit. 3: no screen formats a time of day by itself — one formatter,
// datetime.ts, reached through useInstant. The sweep reads every source file
// under ui/src (tests and datetime.ts aside), so a file added tomorrow is
// checked too. A date with no time stays as it is when it is pinned to UTC
// (crit. 4).

// The screens the spec's Problem lists, History.tsx joined by amendment: each
// must reach the formatter through the hook.
const LISTED = ['Channel.tsx', 'WarRoom.tsx', 'FeatureAgents.tsx', 'FeaturePage.tsx', 'Mirror.tsx', 'History.tsx']

// Files whose toLocaleString calls are numbers (a count, a percentage) or a
// window pinned to UTC; a call there that carries a time-of-day option and is
// not pinned still fails.
const NUMBERS = ['Costs.tsx', 'Dashboard.tsx', 'Stats.tsx']

// Date-only calls (no hour) in the local zone that the spec leaves out: P-0068
// holds them (limit accepted by the review); the cleanup drops this list.
const LOCAL_DATES = ['Dashboard.tsx', 'IdeasPanel.tsx', 'Pendings.tsx']

const source = (f: string) => readFileSync(resolve(__dirname, f), 'utf8')

const swept = readdirSync(__dirname, { recursive: true, encoding: 'utf8' }).filter(
  (f) => /\.tsx?$/.test(f) && !/\.test\.tsx?$/.test(f) && !/\.d\.ts$/.test(f) && f !== 'datetime.ts',
)

type Call = { name: string; args: string; line: number }

// callsOf lists each locale-formatting call with the text of its arguments,
// up to the matching parenthesis (a call may span lines).
function callsOf(text: string): Call[] {
  const calls: Call[] = []
  for (const m of text.matchAll(/(\.toLocaleString|\.toLocaleTimeString|\.toLocaleDateString|Intl\.DateTimeFormat)\(/g)) {
    const from = m.index + m[0].length
    let depth = 1
    let i = from
    for (; i < text.length && depth > 0; i++) {
      if (text[i] === '(') depth++
      else if (text[i] === ')') depth--
    }
    calls.push({ name: m[1], args: text.slice(from, i - 1), line: text.slice(0, m.index).split('\n').length })
  }
  return calls
}

const pinnedUTC = (c: Call) => /timeZone:\s*'UTC'/.test(c.args)

// problem says why a call breaks the rule, or '' when it is allowed.
function problem(file: string, c: Call): string {
  if (c.name === 'Intl.DateTimeFormat') return 'builds its own formatter'
  if (c.name === '.toLocaleTimeString') return 'formats a time of day by itself'
  if (c.name === '.toLocaleString') {
    if (pinnedUTC(c)) return ''
    if (!NUMBERS.includes(file)) return 'formats a date and time (or a number) outside the numbers files'
    return /\b(dateStyle|timeStyle|hour|minute|second|timeZoneName)\b/.test(c.args) ? 'formats a time of day by itself' : ''
  }
  // .toLocaleDateString: a day with no time of day.
  return pinnedUTC(c) || LOCAL_DATES.includes(file) ? '' : 'formats a local date outside the accepted limit'
}

describe('instants with a time of day (F-0493 crit. 3)', () => {
  // F-0493 crit. 3
  it.each(swept)('F-0493 crit. 3: %s formats no time of day by itself', (f) => {
    const bad = callsOf(source(f))
      .map((c) => ({ line: c.line, call: c.name, why: problem(f, c) }))
      .filter((b) => b.why !== '')
    expect(bad).toEqual([])
  })

  // F-0493 crit. 3
  it.each(LISTED)('F-0493 crit. 3: %s reaches the formatter through useInstant', (f) => {
    expect(source(f)).toMatch(/useInstant\(\)/)
  })

  // F-0493 crit. 3: the guard is not vacuous — it flags what it exists to catch.
  it('F-0493 crit. 3: the sweep flags a date formatted with the language, a clock and a home-made formatter', () => {
    const flagged = (code: string) => callsOf(code).map((c) => problem('New.tsx', c))
    expect(flagged('new Date(at).toLocaleString(lang)')).toEqual(['formats a date and time (or a number) outside the numbers files'])
    expect(flagged("d.toLocaleTimeString(lang, { hour: '2-digit' })")).toEqual(['formats a time of day by itself'])
    expect(flagged("new Intl.DateTimeFormat(lang, { hour: 'numeric' })")).toEqual(['builds its own formatter'])
    expect(flagged('d.toLocaleDateString(lang)')).toEqual(['formats a local date outside the accepted limit'])
    // A call that spans lines is read to its closing parenthesis.
    expect(callsOf("x.toLocaleString(lang, {\n  hour: 'numeric',\n})")[0].args).toContain("hour: 'numeric'")
    // What stays: a date pinned to UTC, and a number in a numbers file.
    expect(flagged("d.toLocaleDateString(lang, { timeZone: 'UTC' })")).toEqual([''])
    expect(problem('Stats.tsx', callsOf('n.toLocaleString(lang, { maximumFractionDigits: 1 })')[0])).toBe('')
    expect(problem('Stats.tsx', callsOf("d.toLocaleString(lang, { hour: '2-digit' })")[0])).not.toBe('')
  })
})

// F-0493 crit. 4: what was pinned to UTC is still pinned to UTC.
describe('dates pinned to UTC (F-0493 crit. 4)', () => {
  it.each([
    ['Costs.tsx', /toLocaleDateString\(lang, \{ timeZone: 'UTC' \}\)/],
    ['FeaturePage.tsx', /toLocaleDateString\(lang, \{ timeZone: 'UTC' \}\)/],
    ['Dashboard.tsx', /toLocaleString\(lang, \{ timeZone: 'UTC', dateStyle: 'short', timeStyle: 'short' \}\)\} UTC/],
  ])('F-0493 crit. 4: %s keeps its UTC dates and windows', (f, pinned) => {
    expect(source(f)).toMatch(pinned)
  })
})
