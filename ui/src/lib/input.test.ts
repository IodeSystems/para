import { describe, expect, it } from 'vitest'
import { inputTab, splitInput } from './input'

const row = (o: Partial<Parameters<typeof inputTab>[0]> = {}) => ({ needsYou: false, ...o })

describe('inputTab', () => {
  it('puts a reviewed row on Reviewed, even when it needs you or made something', () => {
    expect(inputTab(row({ reviewed: '2026-11-10T10:00:00Z', needsYou: true, made: 2 }))).toBe(
      'reviewed',
    )
  })
  it('puts a row that expects a response on Needs you, before Changed', () => {
    expect(inputTab(row({ needsYou: true, made: 3 }))).toBe('needs')
  })
  it('puts an unreviewed row an agent acted on on Changed para', () => {
    expect(inputTab(row({ made: 1 }))).toBe('changed')
  })
  it('puts the rest on The rest', () => {
    expect(inputTab(row({ digest: true }))).toBe('rest')
    expect(inputTab(row({ made: 0, reviewed: '' }))).toBe('rest')
    expect(inputTab(row({ made: null }))).toBe('rest')
  })
})

describe('splitInput', () => {
  it('groups in order and sorts Reviewed newest first', () => {
    const a = { id: 'a', needsYou: false, reviewed: '2026-11-01' }
    const b = { id: 'b', needsYou: false, reviewed: '2026-11-09' }
    const c = { id: 'c', needsYou: true }
    const d = { id: 'd', needsYou: false, made: 1 }
    const s = splitInput([a, b, c, d])
    expect(s.reviewed.map((x) => x.id)).toEqual(['b', 'a'])
    expect(s.needs).toEqual([c])
    expect(s.changed).toEqual([d])
    expect(s.rest).toEqual([])
  })
})
