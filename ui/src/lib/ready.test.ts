import { describe, expect, it } from 'vitest'
import { readyTarget, readyWords } from '@/lib/ready'

describe('ready', () => {
  it('names what was prepared', () => {
    expect(readyWords('draft')).toBe('Ready: draft reply')
    expect(readyWords('answer')).toBe('Ready: proposed answer')
    expect(readyWords('plan')).toBe('Ready: plan')
    expect(readyWords(null)).toBe('Ready')
  })

  it('opens the draft, else the item', () => {
    expect(readyTarget({ id: 'io-in', readyRef: 'io-reply' })).toBe('io-reply')
    expect(readyTarget({ id: 'question-a', readyRef: null })).toBe('question-a')
  })
})
