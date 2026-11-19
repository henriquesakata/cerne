import { describe, expect, it } from 'vitest'
import '@testing-library/jest-dom/vitest'
import { render } from '@testing-library/react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { createElement } from 'react'
import { remarkWikiLink } from './remarkWikiLink'

const known = new Set(['F-0001', 'N-0002'])
const resolve = (id: string) => (/^(F|N|X)-/.test(id) ? `#/x/${id}` : null)

function html(md: string, withKnown = true) {
  const { container } = render(
    createElement(
      ReactMarkdown,
      { remarkPlugins: [remarkGfm, [remarkWikiLink, { resolve, known: withKnown ? known : undefined, brokenTitle: 'não achada' }]] },
      md,
    ),
  )
  return container
}

describe('remarkWikiLink (F-0463, paridade com internal/mdlink)', () => {
  it('link conhecido, caixa normalizada e alias', () => {
    const c = html('cite [[F-0001]], [[f-0001]] again, [[N-0002|a note]]')
    const links = [...c.querySelectorAll('a.wikilink')]
    expect(links.map((a) => [a.getAttribute('href'), a.textContent])).toEqual([
      ['#/x/F-0001', 'F-0001'],
      ['#/x/F-0001', 'F-0001'],
      ['#/x/N-0002', 'a note'],
    ])
  })

  it('id desconhecido vira span.wikilink-broken; sem known nada é quebrado', () => {
    const broken = html('[[X-9999]]').querySelector('span.wikilink-broken')
    expect(broken?.textContent).toBe('X-9999')
    expect(broken).toHaveAttribute('title', 'não achada')
    const c = html('[[X-9999]]', false)
    expect(c.querySelector('.wikilink-broken')).toBeNull()
    expect(c.querySelector('a.wikilink')).not.toBeNull()
  })

  it('título, [[historical]] e prefixo sem rota ficam como texto', () => {
    const c = html('[[algum título]] [[historical]] [[W-0001]]')
    expect(c.querySelector('a, .wikilink-broken')).toBeNull()
    expect(c.textContent).toContain('[[algum título]] [[historical]] [[W-0001]]')
  })

  it('nada muda em bloco de código nem em código inline', () => {
    const c = html('Top [[F-0001]].\n```toml\n[[F-0001]]\n```\nMid `[[F-0001]]` span.')
    expect(c.querySelectorAll('a.wikilink')).toHaveLength(1)
    expect(c.querySelector('pre code')?.textContent).toContain('[[F-0001]]')
    expect(c.querySelector('p code')?.textContent).toBe('[[F-0001]]')
  })
})
