import { describe, expect, it } from 'vitest'
import { entityHref, isFeatureFamily } from './entityRoute'

// B-0022: one prefix → route table for every linkifier. A kind with no page of
// its own is plain text (null), never a link to the wrong page.
describe('entityHref', () => {
  it.each([
    ['F-0001', '#/f/F-0001'],
    ['E-0012', '#/f/E-0012'],
    ['T-0003', '#/f/T-0003'],
    ['B-0019', '#/f/B-0019'],
    ['D-0004', '#/d/D-0004'],
    ['R-0005', '#/r/R-0005'],
    ['RB-0006', '#/rb/RB-0006'],
    ['N-0007', '#/n/N-0007'],
    ['HO-0008', '#/handoffs/HO-0008'],
  ])('%s links to %s', (id, href) => {
    expect(entityHref(id)).toBe(href)
  })

  it.each(['W-0007', 'C-0003', 'P-0001', 'I-0002', 'H-0001', 'X-0001', 'F-', 'F0001', 'wiki:a', '', 'f-0001'])(
    '%s has no page: plain text',
    (id) => {
      expect(entityHref(id)).toBeNull()
    },
  )

  it('does not take RB- for B- or R-', () => {
    expect(entityHref('RB-0001')).toBe('#/rb/RB-0001')
    expect(entityHref('R-0001')).toBe('#/r/R-0001')
  })

  it('encodes the id', () => {
    expect(entityHref('F-0001/x')).toBe('#/f/F-0001%2Fx')
  })
})

describe('isFeatureFamily', () => {
  it('is true for F-, E-, T-, B- only', () => {
    for (const id of ['F-0001', 'E-0001', 'T-0001', 'B-0001']) expect(isFeatureFamily(id)).toBe(true)
    for (const id of ['RB-0001', 'D-0001', 'N-0001', 'W-0001', '']) expect(isFeatureFamily(id)).toBe(false)
  })
})
