import { describe, expect, it } from 'vitest'
import { cardKeyFor, stackKeyFor } from '@/lib/keys'
import {
  canStackDecide,
  nextItem,
  rowActions,
  rowKey,
  STACK_STATES,
  inView,
  laterChip,
  laterDates,
  topicLabel,
  needsYou,
  stackLines,
  VAULT_QUESTIONS,
} from '@/lib/stack'

const row = (
  proposal: string,
  item: string,
  group?: string,
  kind = 'message',
  stack = 'proposed',
) => ({
  proposal,
  title: `Dossier ${proposal}`,
  item,
  kind,
  stack,
  group,
})

describe('review stack', () => {
  it('has the stack states in the order para gives them', () => {
    expect(STACK_STATES).toEqual([
      'proposed',
      'later',
      'decided',
      'executing',
      'done',
      'failed',
      'closed',
    ])
  })
  it('groups rows by dossier, then group, loose items first', () => {
    const rows = [row('p1', 'a', 'g1'), row('p2', 'x'), row('p1', 'b'), row('p1', 'c', 'g1')]
    const lines = stackLines(rows, [
      { proposal: 'p1', group: 'g1', done: 1, total: 3, proposed: 2 },
    ])
    expect(lines.map((l) => l.key)).toEqual([
      'd:p1',
      'p1/b',
      'g:p1/g1',
      'p1/a',
      'p1/c',
      'd:p2',
      'p2/x',
    ])
    const g = lines.find((l) => l.type === 'group')
    expect(g).toMatchObject({ done: 1, total: 3, proposed: 2 })
    expect(lines[0]).toMatchObject({ type: 'dossier', count: 3 })
  })
  it('counts a group from the rows when the server has no progress for it', () => {
    const lines = stackLines([row('p1', 'a', 'g'), row('p1', 'b', 'g', 'message', 'done')], [])
    expect(lines.find((l) => l.type === 'group')).toMatchObject({ total: 2, proposed: 1, done: 0 })
  })
  it('moves the cursor over items only', () => {
    const lines = stackLines([row('p1', 'a'), row('p2', 'b')], [])
    expect(nextItem(lines, null, 1)).toBe('p1/a')
    expect(nextItem(lines, 'p1/a', 1)).toBe('p2/b')
    expect(nextItem(lines, 'p2/b', 1)).toBe('p2/b')
    expect(nextItem(lines, 'p2/b', -1)).toBe('p1/a')
    expect(nextItem(lines, null, -1)).toBe('p2/b')
    expect(nextItem([], null, 1)).toBeNull()
  })
  it('decides only open items that are not questions', () => {
    expect(canStackDecide({ kind: 'message', stack: 'proposed' })).toBe(true)
    expect(canStackDecide({ kind: 'question', stack: 'proposed' })).toBe(false)
    expect(canStackDecide({ kind: 'para', stack: 'done' })).toBe(false)
    expect(rowKey({ proposal: 'p', item: 'i' })).toBe('p/i')
  })
})

describe('stack keys', () => {
  const press = (key: string, extra: Partial<Parameters<typeof stackKeyFor>[0]> = {}) =>
    stackKeyFor({ key, ctrlKey: false, metaKey: false, altKey: false, target: null, ...extra })
  it('maps the letters', () => {
    expect(press('j')).toBe('down')
    expect(press('k')).toBe('up')
    expect(press('a')).toBe('approve')
    expect(press('A')).toBe('approveGroup')
    expect(press('r')).toBe('decline')
    expect(press('d')).toBe('decline')
    expect(press('s')).toBe('skip')
    expect(press('g')).toBe('ask')
    expect(press('m')).toBe('done')
    expect(press('e')).toBe('edit')
    expect(press('c')).toBe('comment')
    expect(press('x')).toBe('select')
    expect(press('o')).toBe('open')
    expect(press('z')).toBeNull()
  })
  it('does nothing in a field or with a modifier', () => {
    expect(press('a', { target: { tagName: 'TEXTAREA' } })).toBeNull()
    expect(press('a', { ctrlKey: true })).toBeNull()
  })
  it('a dossier page maps d, r, s, g, m to the current card', () => {
    const card = (key: string, target: { tagName?: string } | null = null) =>
      cardKeyFor({ key, ctrlKey: false, metaKey: false, altKey: false, target })
    expect(card('d')).toBe('decline')
    expect(card('r')).toBe('decline')
    expect(card('s')).toBe('skip')
    expect(card('g')).toBe('ask')
    expect(card('m')).toBe('done')
    expect(card('a')).toBeNull()
    expect(card('s', { tagName: 'INPUT' })).toBeNull()
  })
})

describe('stack row actions', () => {
  const r = (kind: string, state = 'pending', stack = 'proposed', question?: string) => ({
    kind,
    state,
    stack,
    question,
  })
  it('a dossier item: approve, decline, skip, ask; done by me when outward', () => {
    expect(rowActions(r('message'))).toEqual({
      approve: true,
      decline: true,
      skip: true,
      ask: true,
      done: true,
      bring: false,
    })
    expect(rowActions(r('para')).done).toBe(false)
    expect(rowActions(r('message', 'approved', 'decided'))).toEqual({
      approve: false,
      decline: false,
      skip: false,
      ask: false,
      done: true,
      bring: false,
    })
    expect(rowActions(r('message', 'done', 'done')).done).toBe(false)
  })
  it('a dossier question is skipped or asked from the row, answered on its card', () => {
    expect(rowActions(r('question'))).toEqual({
      approve: false,
      decline: false,
      skip: true,
      ask: true,
      done: false,
      bring: false,
    })
  })
  it('a vault question is declined, skipped, or asked; answered on its card', () => {
    expect(rowActions(r('question', 'skipped', 'proposed', 'question-a'))).toEqual({
      approve: false,
      decline: true,
      skip: true,
      ask: true,
      done: false,
      bring: false,
    })
  })
  it('vault questions group under one heading', () => {
    const lines = stackLines(
      [
        {
          proposal: '',
          title: 'Which region?',
          item: 'q-a',
          kind: 'question',
          stack: 'proposed',
          question: 'q-a',
        },
        {
          proposal: '',
          title: 'Which day?',
          item: 'q-b',
          kind: 'question',
          stack: 'proposed',
          question: 'q-b',
        },
      ],
      [],
    )
    expect(lines[0]).toMatchObject({
      type: 'dossier',
      proposal: '',
      title: VAULT_QUESTIONS,
      count: 2,
    })
    expect(lines).toHaveLength(3)
  })
})

describe('needs you', () => {
  it('keeps only what waits on the owner', () => {
    expect(needsYou({ stack: 'proposed', state: 'pending' })).toBe(true)
    expect(needsYou({ stack: 'proposed', state: 'skipped' })).toBe(true)
    expect(needsYou({ stack: 'proposed', state: 'sent_back' })).toBe(false)
    expect(needsYou({ stack: 'later', state: 'pending' })).toBe(false)
    expect(needsYou({ stack: 'decided', state: 'approved' })).toBe(false)
    expect(needsYou({ stack: 'done', state: 'answered' })).toBe(false)
  })
})

describe('later dates', () => {
  it('offers tomorrow and the next Monday', () => {
    // Thursday 2026-11-05
    expect(laterDates(new Date(2026, 10, 5, 9))).toEqual({
      tomorrow: '2026-11-06',
      nextWeek: '2026-11-09',
    })
    // A Monday goes to the Monday after.
    expect(laterDates(new Date(2026, 10, 9, 9))).toEqual({
      tomorrow: '2026-11-10',
      nextWeek: '2026-11-16',
    })
    // Month end.
    expect(laterDates(new Date(2026, 10, 30, 9)).tomorrow).toBe('2026-12-01')
  })
})

describe('skip is navigation in one visit', () => {
  const rows = [{ item: 'a' }, { item: 'b' }, { item: 'c' }]
  const key = (r: { item: string }) => r.item
  it('the to-decide view hides what this visit skipped', () => {
    expect(inView(rows, new Set(['b']), key, true).map((r) => r.item)).toEqual(['a', 'c'])
    expect(inView(rows, new Set(), key, true)).toHaveLength(3)
  })
  it('another view shows every row', () => {
    expect(inView(rows, new Set(['b']), key, false)).toHaveLength(3)
  })
})

describe('later items', () => {
  it('a later row may be brought back, and cannot be decided', () => {
    const a = rowActions({ kind: 'message', state: 'pending', stack: 'later' })
    expect(a).toEqual({
      approve: false,
      decline: false,
      skip: false,
      ask: false,
      done: false,
      bring: true,
    })
  })
  it('says the date on the chip', () => {
    expect(laterChip('2026-11-09')).toBe('later until 2026-11-09')
    expect(laterChip('someday')).toBe('later: someday')
  })
  it('a topic chip counts later items and shows their number apart', () => {
    expect(topicLabel({ tag: 'socials', stack: 2, later: 3 })).toBe('#socials 5 (3 later)')
    expect(topicLabel({ tag: 'ops', stack: 4, later: 0 })).toBe('#ops 4')
  })
})
