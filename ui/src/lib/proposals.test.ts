import { describe, expect, it } from 'vitest'
import {
  assessmentRows,
  batchPairs,
  CHANGED_WORDS,
  orderGroups,
  refusalsFor,
  refusalText,
  selectedDraft,
} from '@/lib/proposals'

const d = (id: string, state = 'draft', needsYou = false) => ({
  id,
  state,
  approval: '',
  needsYou,
})

describe('assessmentRows', () => {
  it('shows a stated one with percent and basis, the rest as not stated', () => {
    const r = assessmentRows([{ name: 'confidence', percent: 80, basis: 'read the thread' }], [
      'recommend',
      'risk',
    ])
    expect(r.map((x) => x.text)).toEqual([
      'Confidence: 80%',
      'Recommend: not stated',
      'Risk: not stated',
    ])
    expect(r[0]?.basis).toBe('read the thread')
    expect(r[1]?.percent).toBeNull()
  })
  it('a missing standard name is not stated even when the server did not list it', () => {
    expect(assessmentRows([], []).every((x) => x.percent === null)).toBe(true)
  })
  it('clamps and keeps other stated names', () => {
    const r = assessmentRows([{ name: 'effort', percent: 140 }])
    expect(r.find((x) => x.name === 'effort')?.percent).toBe(100)
  })
})

describe('batchPairs', () => {
  const sets = [
    { root: 'a', drafts: [d('a1'), d('a2')] },
    { root: 'b', drafts: [d('b1')] },
    { root: 'c', drafts: [d('c1', 'ready')] },
  ]
  const hashes = { a1: 'h1', a2: 'h2', b1: 'h3', c1: 'h4' }
  it('takes one draft per set: the selected tab', () => {
    const r = batchPairs(sets, { a: 'a2' }, hashes)
    expect(r.pairs).toEqual([
      { id: 'a2', expectHash: 'h2' },
      { id: 'b1', expectHash: 'h3' },
    ])
  })
  it('defaults to the draft that needs the owner, never two of a set, never a ready one', () => {
    const s = [{ root: 'x', drafts: [d('x1'), d('x2', 'draft', true)] }]
    expect(batchPairs(s, {}, { x1: 'a', x2: 'b' }).pairs).toEqual([{ id: 'x2', expectHash: 'b' }])
    expect(batchPairs(sets, {}, hashes).pairs.map((p) => p.id)).not.toContain('c1')
  })
  it('only sets handed in (the rendered ones) count; a missing hash is skipped and counted', () => {
    const r = batchPairs([sets[1]], {}, {})
    expect(r).toEqual({ pairs: [], skipped: 1 })
  })
  it('ignores a chosen id that is not in the set', () => {
    expect(selectedDraft(sets[0], 'zzz')?.id).toBe('a1')
  })
})

describe('refusals', () => {
  it('a hash mismatch reads changed since you looked', () => {
    expect(refusalText('x changed after the owner saw it, so it is not approved')).toBe(
      CHANGED_WORDS,
    )
    expect(refusalText('no such draft')).toBe('no such draft')
  })
  it('a batch refusal goes to the items it names', () => {
    const r = refusalsFor(['io-aa', 'io-bb'], 'items[1]: io-bb changed after the owner saw it')
    expect(r.byId).toEqual({ 'io-bb': CHANGED_WORDS })
    expect(r.all).toBeUndefined()
    expect(refusalsFor(['io-aa'], 'nope').all).toBe('nope')
  })
})

describe('orderGroups', () => {
  it('needs-owner first, stable', () => {
    const g = [
      { g: 1, needsYou: false },
      { g: 2, needsYou: true },
      { g: 3, needsYou: false },
      { g: 4, needsYou: true },
    ]
    expect(orderGroups(g).map((x) => x.g)).toEqual([2, 4, 1, 3])
  })
})
