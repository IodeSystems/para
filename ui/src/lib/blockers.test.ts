import { describe, expect, it } from 'vitest'
import { blockerLink, blockerTone } from './blockers'

describe('blockers', () => {
  it('links each blocker to its page', () => {
    const b = { id: 'x', state: 'unapproved', title: 'T' }
    expect(blockerLink({ ...b, type: 'output' }).to).toBe('/io/$id')
    expect(blockerLink({ ...b, type: 'Ask' }).to).toBe('/rfx/$id')
    expect(blockerLink({ ...b, type: 'task' }).to).toBe('/docs/$id')
  })
  it('marks a blocker that waits on the owner', () => {
    expect(blockerTone('unapproved')).toBe('attn')
    expect(blockerTone('waiting for reply')).toBe('wait')
  })
})
