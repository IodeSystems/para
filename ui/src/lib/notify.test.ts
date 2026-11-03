import { describe, expect, it } from 'vitest'
import { noticeText, rises, workNeedsYou, zeroCounts } from '@/lib/notify'

describe('notify', () => {
  it('reports only growth, by kind', () => {
    const a = { ...zeroCounts(), answer: 1, work: 2 }
    const b = { ...zeroCounts(), answer: 3, work: 1, incident: 1 }
    expect(rises(a, b)).toEqual([
      { kind: 'answer', count: 2 },
      { kind: 'incident', count: 1 },
    ])
    expect(rises(b, a)).toEqual([{ kind: 'work', count: 1 }])
  })
  it('says kind and count only', () => {
    expect(noticeText({ kind: 'answer', count: 1 })).toBe('1 answer to check')
    expect(noticeText({ kind: 'answer', count: 2 })).toBe('2 answers to check')
  })
  it('knows which agent work waits for the owner', () => {
    expect(workNeedsYou('stuck')).toBe(true)
    expect(workNeedsYou('needs_clarification')).toBe(true)
    expect(workNeedsYou('done')).toBe(false)
    expect(workNeedsYou('')).toBe(false)
  })
})
