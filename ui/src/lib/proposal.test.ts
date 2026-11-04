import { describe, expect, it } from 'vitest'
import { diffKind } from '@/lib/diff'
import { isOutward, stateWords } from '@/lib/proposal'

describe('proposal helpers', () => {
  it('reads diff lines', () => {
    expect(diffKind('+ new')).toBe('add')
    expect(diffKind('- old')).toBe('del')
    expect(diffKind('  same')).toBe('same')
  })
  it('tells a vault change from an act outside', () => {
    expect(isOutward('message')).toBe(true)
    expect(isOutward('action')).toBe(true)
    expect(isOutward('external')).toBe(true)
    expect(isOutward('para')).toBe(false)
    expect(isOutward('op')).toBe(false)
  })
  it('says an approved outward item is not done', () => {
    expect(stateWords('approved', 'message')).toBe('approved, not done')
    expect(stateWords('approved', 'para')).toBe('approved')
    expect(stateWords('pending', 'para')).toBe('waiting for you in the chat')
    expect(stateWords('sent_back', 'para')).toBe('sent back')
  })
})
