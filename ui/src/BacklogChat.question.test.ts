import { describe, expect, it } from 'vitest'
import { parseQuestions, stripProposalFence } from './BacklogChat'

const block = (lines: string[], title = 'Qual canal?') =>
  '````cerne-pergunta\n# ' + title + '\n' + lines.join('\n') + '\n````'

describe('parseQuestions (F-0437)', () => {
  it('reads the question, the options and the recommended one', () => {
    const reply = 'Antes de seguir:\n\n' + block(['- Email — chega a todos', '- Push — só quem instalou (recomendada)'])
    expect(parseQuestions(reply)).toEqual([
      {
        question: 'Qual canal?',
        options: [
          { label: 'Email', consequence: 'chega a todos', recommended: false },
          { label: 'Push', consequence: 'só quem instalou', recommended: true },
        ],
      },
    ])
  })

  it('accepts the English recommended marker', () => {
    const q = parseQuestions(block(['- A — x', '- B — y (recommended)']))
    expect(q[0].options.map((o) => o.recommended)).toEqual([false, true])
  })

  it('reads up to several blocks in one reply', () => {
    const reply = block(['- A — x (recomendada)', '- B — y'], 'P1') + '\n\nmeio\n\n' + block(['- C — z', '- D — w (recomendada)'], 'P2')
    expect(parseQuestions(reply).map((q) => q.question)).toEqual(['P1', 'P2'])
  })

  it.each([
    ['no options', []],
    ['one option only', ['- A — x (recomendada)']],
    ['no recommended', ['- A — x', '- B — y']],
    ['two recommended', ['- A — x (recomendada)', '- B — y (recomendada)']],
  ])('malformed block (%s) yields no card and stays as text', (_n, lines) => {
    const reply = 'Texto\n' + block(lines as string[])
    expect(parseQuestions(reply)).toEqual([])
    expect(stripProposalFence(reply)).toBe(reply)
  })

  it('stripProposalFence removes a valid question block from the prose', () => {
    const reply = 'Antes\n' + block(['- A — x (recomendada)', '- B — y']) + '\nDepois'
    expect(stripProposalFence(reply)).toBe('Antes\nDepois')
  })

  it('an unclosed block (mid-stream) is text and hides nothing', () => {
    const reply = 'Antes\n````cerne-pergunta\n# Qual?\n- A — x (recomendada)\n- B — y\nresto da resposta'
    expect(parseQuestions(reply)).toEqual([])
    expect(stripProposalFence(reply)).toBe(reply)
  })

  it('reads a CRLF block', () => {
    const reply = block(['- A — x (recomendada)', '- B — y']).replace(/\n/g, '\r\n')
    expect(parseQuestions(reply)).toHaveLength(1)
  })

  it('ignores a question block that lies inside a proposal', () => {
    const reply = '````cerne-proposta\n# T\n\n## Problema\n\n' + '```cerne-pergunta\n# Dentro?\n- A — x (recomendada)\n- B — y\n```\n' + '\n````\n'
    expect(parseQuestions(reply)).toEqual([])
  })
})
