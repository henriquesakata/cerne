import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'
import { checkProjectName } from './projectName'

// O lado TS da trava de paridade Go↔TS do nome do projeto (F-0331): o MESMO
// corpus é lido por internal/app/project_test.go. Uma regra mudada num lado só
// quebra a rodada do outro.
const corpus = JSON.parse(
  readFileSync(resolve(__dirname, '../../internal/app/testdata/project_names.json'), 'utf-8'),
) as { cases: { name: string; in: string; rule: string; stored: string }[] }

describe('checkProjectName — paridade com app.RenameProject (F-0331)', () => {
  it('o corpus compartilhado tem casos', () => {
    expect(corpus.cases.length).toBeGreaterThan(0)
  })

  it.each(corpus.cases.map((c) => [c.name, c] as const))('%s', (_, c) => {
    const got = checkProjectName(c.in)
    expect(got.rule ?? '').toBe(c.rule)
    if (c.rule === '') expect(got.name).toBe(c.stored)
  })
})
