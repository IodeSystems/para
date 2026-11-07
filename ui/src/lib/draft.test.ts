import { describe, expect, it } from 'vitest'
import { draftEditBody } from '@/lib/draft'

describe('draftEditBody', () => {
  it('saves a changed text alone, so an approved draft goes back to draft', () => {
    expect(draftEditBody('io-1', 'Hi', 'Hello', false)).toEqual({ id: 'io-1', text: 'Hello' })
  })
  it('saves and approves in one call', () => {
    expect(draftEditBody('io-1', 'Hi', 'Hello', true)).toEqual({
      id: 'io-1',
      text: 'Hello',
      state: 'ready',
    })
  })
  it('approves an unchanged text without resending it', () => {
    expect(draftEditBody('io-1', 'Hi', 'Hi', true)).toEqual({ id: 'io-1', state: 'ready' })
  })
  it('has nothing to save for an unchanged or empty text', () => {
    expect(draftEditBody('io-1', 'Hi', 'Hi', false)).toBeNull()
    expect(draftEditBody('io-1', 'Hi', '  ', false)).toBeNull()
  })
})
