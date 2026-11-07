import { describe, expect, it } from 'vitest'
import { cadenceWords, reachWords, statsWords, viaWords } from './sources'

describe('sources', () => {
  it('says how an agent reaches a source', () => {
    expect(viaWords({ via: 'mcp', server: 'gmail' })).toBe('MCP (gmail): agent only')
    expect(viaWords({ via: 'cli' })).toBe('command line on this machine')
    expect(viaWords({ via: 'none' })).toBe('read as described')
  })
  it('names who reaches it', () => {
    expect(reachWords({ reachedBy: [], declared: [] })).toBe('no agent declared it lately')
    expect(
      reachWords({
        reachedBy: ['claude', 'codex'],
        declared: [
          { actor: 'claude', fresh: true },
          { actor: 'old', fresh: false },
        ],
      }),
    ).toBe('reached by claude; listed: codex')
  })
  it('gives the numbers of a source', () => {
    expect(statsWords(undefined)).toBe('')
    const w = { days: 7, sweeps: 3, failed: 1, in: 5, out: 2, replyHours: 4.5, replied: 2 }
    expect(statsWords({ openReplies: 2, overdue: 1, held: 1, windows: [w] })).toBe(
      '7d: 3 sweeps (1 failed), 5 in, 2 out - median reply 4.5 h - 2 replies owed, 1 overdue - 1 held by the pre-send check',
    )
    expect(
      statsWords({
        openReplies: 0,
        overdue: 0,
        held: 0,
        windows: [{ ...w, failed: 0, replyHours: -1 }],
      }),
    ).toBe('7d: 3 sweeps, 5 in, 2 out')
  })
  it('says on demand with no cadence', () => {
    expect(cadenceWords({ cadence: '' })).toBe('on demand')
    expect(cadenceWords({ cadence: 'daily' })).toBe('daily')
  })
})
