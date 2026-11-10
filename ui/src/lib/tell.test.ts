import { describe, expect, it } from 'vitest'
import { TELL_QUEUED_MS, tellText, tellToast } from '@/lib/tell'
import { TURN_UNDO_MS } from '@/lib/threadEdit'
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
    expect(t.dwell).toBe(TURN_UNDO_MS)
    expect(t.target).toEqual({ id: 'io-1', kind: 'io' })
  })
  it('offers Edit and Undo for about ten seconds when the turn is known', () => {
    const undo = async () => {}
    const edit = () => {}
    const t = tellToast('io-1', { request: 'request-1', status: 'queued' }, { undo, edit })
    expect(t.undo).toBe(undo)
    expect(t.edit).toBe(edit)
    expect(t.dwell).toBe(10000)
    // With no request there is nothing to edit or withdraw.
    const u = tellToast('io-1', { status: 'queued' }, { undo, edit })
    expect(u.undo).toBeUndefined()
    expect(u.edit).toBeUndefined()
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
