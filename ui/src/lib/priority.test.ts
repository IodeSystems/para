import { describe, expect, it } from 'vitest'
import { digestRows, priorityHelp, splitInbox } from '@/lib/priority'

describe('inbox order', () => {
  const rows = [
    { id: 'a', needsYou: false, digest: true },
    { id: 'b', needsYou: true, digest: false },
    { id: 'c', needsYou: false, digest: null },
    { id: 'd', needsYou: true },
  ]
  it('puts what needs you first, in order', () => {
    const s = splitInbox(rows)
    expect(s.needsYou.map((r) => r.id)).toEqual(['b', 'd'])
    expect(s.rest.map((r) => r.id)).toEqual(['a', 'c'])
  })
  it('lists as digest only rows the server marks digest', () => {
    expect(digestRows(rows).map((r) => r.id)).toEqual(['a'])
  })
})

describe('priority words', () => {
  it('says what a priority does', () => {
    expect(priorityHelp('high')).toContain('needs you')
    expect(priorityHelp('unset', 'high')).toBe('Not set: high from a group.')
    expect(priorityHelp('unset')).toContain('normal')
  })
})
