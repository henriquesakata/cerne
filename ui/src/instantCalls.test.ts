import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

// F-0493 crit. 3: the screens that show an instant with a time of day go
// through useInstant (datetime.ts), none builds the format itself. The files
// are the ones the spec's Problem lists (History.tsx joined it by amendment). A date with no time stays as it is
// when it is pinned to UTC (crit. 4).
const FILES = ['Channel.tsx', 'WarRoom.tsx', 'FeatureAgents.tsx', 'FeaturePage.tsx', 'Mirror.tsx', 'History.tsx']

const source = (f: string) => readFileSync(resolve(__dirname, f), 'utf8')

describe('instants with a time of day (F-0493 crit. 3)', () => {
  it.each(FILES)('F-0493 crit. 3: %s calls useInstant and never formats a time itself', (f) => {
    const text = source(f)
    expect(text).toMatch(/useInstant\(\)/)
    expect(text).not.toMatch(/\.toLocaleString\(/)
    expect(text).not.toMatch(/\.toLocaleTimeString\(/)
    expect(text).not.toMatch(/Intl\.DateTimeFormat\(/)
  })

  // F-0493 crit. 4: what was UTC stays UTC.
  it.each(FILES)('F-0493 crit. 4: %s keeps its dates with no time pinned to UTC', (f) => {
    for (const line of source(f).split('\n')) {
      if (/\.toLocaleDateString\(/.test(line)) expect(line, line).toMatch(/timeZone: 'UTC'/)
    }
  })
})
