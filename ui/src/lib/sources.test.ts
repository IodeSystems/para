import { describe, expect, it } from 'vitest'
import { cadenceWords, reachWords, viaWords } from './sources'

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
  it('says on demand with no cadence', () => {
    expect(cadenceWords({ cadence: '' })).toBe('on demand')
    expect(cadenceWords({ cadence: 'daily' })).toBe('daily')
  })
})
