import { describe, expect, it } from 'vitest'
import { closeLine, isHighClose, notItQuote } from './proposedClose'

describe('proposed close', () => {
  it('says the outcome in words, the confidence, and the reason', () => {
    expect(closeLine({ outcome: 'done', confidence: 90, reason: 'Renewed.' })).toBe(
      'Likely done, 90%: Renewed.',
    )
    expect(closeLine({ outcome: 'dropped', confidence: 40, reason: 'Moved.' })).toBe(
      'Likely not needed, 40%: Moved.',
    )
    expect(closeLine({ outcome: 'other', confidence: 5, reason: 'x' })).toBe('Likely other, 5%: x')
  })
  it('highlights from 80', () => {
    expect(isHighClose(80)).toBe(true)
    expect(isHighClose(79)).toBe(false)
  })
  it('quotes the proposal in a Not it turn', () => {
    expect(notItQuote({ outcome: 'done', reason: 'Renewed.' })).toBe(
      'Proposed close (done): Renewed.',
    )
  })
})
