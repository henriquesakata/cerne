import { describe, expect, it } from 'vitest'
import { proposalWithCode as fixture } from './__fixtures__/proposalWithCode'
import { parseNote, parseProposal, stripProposalFence } from './BacklogChat'

describe('parseFenced (F-0435)', () => {
  it('parseFenced keeps inner code blocks', () => {
    const p = parseProposal(fixture)
    expect(p).not.toBeNull()
    expect(p!.title).toBe('Exportar tarefas do taskflow em CSV')
    expect(p!.body).toContain('```go')
    expect(p!.body).toContain('```csv')
    expect(p!.body).toContain('## Fora de escopo')
    expect(p!.body.trim().endsWith('Exportar em XLSX.')).toBe(true)
    expect(stripProposalFence(fixture)).not.toContain('Fora de escopo')
  })

  it('parseFenced legacy three-backtick proposal', () => {
    const reply = 'Texto\n```cerne-proposta\n# Título\nfacet: code\ncomponents: a\n\n## Problema\n\nX.\n```\nDepois'
    const p = parseProposal(reply)
    expect(p?.title).toBe('Título')
    expect(p?.body).toBe('## Problema\n\nX.')
    expect(stripProposalFence(reply)).toBe('Texto\nDepois')
  })

  it('parseFenced serves the note block with the same rule', () => {
    const reply = '````cerne-nota\n# Nota\nfacet: process\ncomponents: x\n\nCorpo\n```js\nx\n```\nFim\n````'
    expect(parseNote(reply)?.body).toBe('Corpo\n```js\nx\n```\nFim')
  })
})
