import { describe, expect, it } from 'vitest'
import { clientWords, lastCallWords, noListenerWords, pushChip } from '@/lib/agents'

describe('connected agents', () => {
  it('says whether para can wake a session', () => {
    expect(pushChip(['message', 'channel'])).toEqual({ label: 'can be woken', tone: 'good' })
    expect(pushChip(['message'])).toEqual({ label: 'message only', tone: 'quiet' })
    expect(pushChip([]).label).toBe('message only')
  })
  it('names the client', () => {
    expect(clientWords({ client: 'claude-code', version: '2.1.0' })).toBe('claude-code 2.1.0')
    expect(clientWords({ client: 'dun' })).toBe('dun')
    expect(clientWords({})).toBe('unknown client')
  })
  it('says when it last called a tool', () => {
    const now = Date.parse('2026-11-08T10:05:00Z')
    expect(lastCallWords('2026-11-08T10:03:00Z', now)).toBe('last call 2 minutes ago')
    expect(lastCallWords(null, now)).toBe('no tool call yet')
  })
  it('warns when queued work has no listener', () => {
    expect(noListenerWords(3)).toBe(
      '3 queued, and no agent is connected on this machine. Start one, or run /para loop.',
    )
  })
})
