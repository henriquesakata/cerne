import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

// F-0496: secondary text (--faint, the colour of .muted) passes WCAG AA 1.4.3
// on every surface it sits on, in both theme blocks.
const css = readFileSync('src/app.css', 'utf8')

function block(selector: string): string {
  // The theme blocks open at the start of a line; a later rule that starts
  // with the same selector (`[data-theme='dark'] .x {`) never matches.
  const start = css.indexOf(`\n${selector} {`)
  if (start < 0) throw new Error(`theme block ${selector} not found`)
  return css.slice(start, css.indexOf('\n}', start))
}

function token(src: string, name: string): string {
  const m = new RegExp(`--${name}:\\s*(#[0-9a-fA-F]{3,6})\\s*;`).exec(src)
  if (!m) throw new Error(`--${name} not found`)
  return m[1].length === 4 ? `#${[...m[1].slice(1)].map((c) => c + c).join('')}` : m[1]
}

function luminance(hex: string): number {
  const [r, g, b] = [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16) / 255)
  const lin = (c: number) => (c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4)
  return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b)
}

function ratio(a: string, b: string): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x)
  return (hi + 0.05) / (lo + 0.05)
}

describe('secondary text contrast (F-0496)', () => {
  // F-0496 crit. 1
  it.each([
    ['light', ':root'],
    ['dark', "[data-theme='dark']"],
  ])('%s: --faint reaches 4.5:1 on bg, surface and surface-2, and stays lighter than --muted', (_, selector) => {
    const b = block(selector)
    const faint = token(b, 'faint')
    for (const bg of ['bg', 'surface', 'surface-2']) {
      expect(ratio(faint, token(b, bg)), `--faint on --${bg}`).toBeGreaterThanOrEqual(4.5)
    }
    expect(ratio(token(b, 'muted'), token(b, 'bg'))).toBeGreaterThan(ratio(faint, token(b, 'bg')))
  })
})
