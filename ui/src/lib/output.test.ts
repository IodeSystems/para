import { describe, expect, it } from 'vitest'
import { resultChips, sinceFrom, variantWords } from '@/lib/output'

describe('resultChips', () => {
  it('has none for a message with no result', () => {
    expect(resultChips({})).toEqual([])
    expect(resultChips({ lastAttempt: { result: 'sent' } })).toEqual([])
  })
  it('links the reply', () => {
    expect(resultChips({ reply: { id: 'io-r' } })).toEqual([
      { key: 'reply', label: 'Replied', tone: 'good', io: 'io-r' },
    ])
  })
  it('shows the Ask state', () => {
    const [c] = resultChips({ ask: { id: 'rfx-1', status: 'no_reply' } })
    expect(c?.label).toBe('Ask: no reply')
  })
  it('shows a failed or held attempt with its error', () => {
    const [f] = resultChips({ lastAttempt: { result: 'failed', error: 'smtp 550' } })
    expect(f).toMatchObject({ tone: 'bad', label: 'Failed: smtp 550', title: 'smtp 550' })
    const [h] = resultChips({ lastAttempt: { result: 'held' } })
    expect(h).toMatchObject({ tone: 'attn', label: 'Held' })
  })
})

describe('sinceFrom', () => {
  it('counts back days', () => {
    expect(sinceFrom(7, new Date('2026-11-10T12:00:00Z'))).toBe('2026-11-03')
    expect(sinceFrom(0)).toBeUndefined()
  })
})

describe('variantWords', () => {
  it('names the place in a set', () => {
    expect(variantWords(2, 3)).toBe('variation 2 of 3')
    expect(variantWords(null, null)).toBe('')
  })
})
