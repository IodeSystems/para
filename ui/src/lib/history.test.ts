import { describe, expect, it } from 'vitest'
import { kindLabel, refLabel, refTarget, rowTime, whoLabel, whoTone } from '@/lib/history'

describe('whoLabel', () => {
  it('names each kind of actor', () => {
    expect(whoLabel({ who: 'owner' })).toBe('You')
    expect(whoLabel({ who: 'agent:claude', actor: 'claude' })).toBe('claude')
    expect(whoLabel({ who: 'tick' })).toBe('Tick')
    expect(whoLabel({ who: 'hand edit' })).toBe('Hand edit')
    expect(whoLabel({ who: 'person', actor: 'Sam Example' })).toBe('Sam Example')
    expect(whoLabel({ who: 'person', actor: null })).toBe('Someone')
  })
})

describe('whoTone', () => {
  it('marks a hand edit and quiets the owner', () => {
    expect(whoTone('hand edit')).toBe('attn')
    expect(whoTone('agent:claude')).toBe('wait')
    expect(whoTone('tick')).toBe('wait')
    expect(whoTone('owner')).toBe('quiet')
  })
})

describe('kindLabel', () => {
  it('says known kinds in words and spaces the rest', () => {
    expect(kindLabel('agent_result')).toBe('Agent result')
    expect(kindLabel('receipt')).toBe('Agent write')
    expect(kindLabel('some_new_kind')).toBe('some new kind')
  })
})

describe('rowTime', () => {
  it('keeps a time and turns a day-only row into its day', () => {
    expect(rowTime({ at: '2026-11-06T10:00:00Z' })).toBe('2026-11-06T10:00:00Z')
    const noon = new Date(2026, 10, 6, 12).toISOString()
    expect(rowTime({ at: noon, dateOnly: true })).toBe('2026-11-06')
  })
})

describe('refTarget', () => {
  it('opens messages on the io page and the rest on the doc page', () => {
    expect(refTarget({ kind: 'message_in', ref: 'io-1' })).toEqual({ to: '/io/$id', id: 'io-1' })
    expect(refTarget({ kind: 'receipt', ref: 'receipt-1' })).toEqual({
      to: '/docs/$id',
      id: 'receipt-1',
    })
    expect(refTarget({ kind: 'note', ref: null })).toBeNull()
    expect(refLabel('agent_result')).toBe('agent work')
    expect(refLabel('message_out')).toBe('message')
  })
})
