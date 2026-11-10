import { describe, expect, it } from 'vitest'
import { TELL_QUEUED_MS, tellText, tellToast } from '@/lib/tell'
import { captureCall, captureReady, type Capture } from '@/lib/context'

describe('tellText', () => {
  it('puts the instruction first and keeps the owner words', () => {
    const t = tellText('  I promised X a spec by Friday.\nSecond line.  ')
    expect(t.startsWith('File this:')).toBe(true)
    expect(t.endsWith('I promised X a spec by Friday.\nSecond line.')).toBe(true)
  })
})

describe('tellToast', () => {
  it('sent', () => {
    const t = tellToast('io-1', { status: 'queued', waitingNoAgent: false })
    expect(t.text).toBe('Saved and sent to your agent')
    expect(t.link.io).toBe('io-1')
    expect(t.dwell).toBeUndefined()
  })
  it('no agent: says queued and stays longer', () => {
    for (const r of [
      { status: 'queued', waitingNoAgent: true },
      { status: 'proposed' },
      { status: 'queued', note: 'Proposed: ...' },
    ]) {
      const t = tellToast('io-1', r)
      expect(t.text).toContain('Queued: no agent is connected')
      expect(t.dwell).toBe(TELL_QUEUED_MS)
    }
  })
})

describe('capture with tell', () => {
  const c: Capture = {
    kind: 'verbal',
    with: [{ id: 'p1', name: 'P' }],
    from: '',
    date: '2026-11-10',
    summary: 'x',
    url: '',
    tell: true,
  }
  it('is an io item, not a logged contact', () => {
    expect(captureCall(c).op).toBe('io_add')
    expect(captureCall({ ...c, tell: false }).op).toBe('interaction_add')
    expect(captureReady(c)).toBe(true)
  })
})
