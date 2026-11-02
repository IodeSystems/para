import { describe, expect, it } from 'vitest'
import { plainState, toneOf, toneOfSeverity } from '@/lib/status'

describe('status', () => {
  it('gives each state one tone', () => {
    expect(toneOf('done')).toBe('good')
    expect(toneOf('sent')).toBe('wait')
    expect(toneOf('no_reply')).toBe('attn')
    expect(toneOf('blocked')).toBe('bad')
    expect(toneOf('todo')).toBe('quiet')
    expect(toneOf('anything else')).toBe('quiet')
  })
  it('writes states in words', () => {
    expect(plainState('no_reply')).toBe('no reply')
  })
  it('maps severity', () => {
    expect(toneOfSeverity('sev1')).toBe('bad')
    expect(toneOfSeverity('sev2')).toBe('attn')
    expect(toneOfSeverity('sev3')).toBe('quiet')
  })
})
