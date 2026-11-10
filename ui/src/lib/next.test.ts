import { describe, expect, it } from 'vitest'
import {
  backCard,
  currentCard,
  deckOf,
  effortWords,
  groupWords,
  headerLine,
  homeFor,
  left,
  skipCard,
} from '@/lib/next'
import { nextKeyFor, NEXT_KEYS, TABS } from '@/lib/keys'

const row = (
  id: string,
  extra: Partial<{ needsYou: boolean; bucket: string; ready: boolean }> = {},
) => ({
  id,
  needsYou: true,
  bucket: 'now',
  ...extra,
})

describe('deck', () => {
  const rows = [
    row('a'),
    row('b', { ready: true }),
    row('c', { needsYou: false }),
    row('d'),
    row('e', { bucket: 'not_yet' }),
  ]
  it('keeps what needs you and can start now, in order', () => {
    expect(deckOf(rows).map((r) => r.id)).toEqual(['a', 'b', 'd'])
  })
  it('the card is the first not gone and not skipped', () => {
    const deck = deckOf(rows)
    expect(currentCard(deck, [], new Set())?.id).toBe('a')
    expect(currentCard(deck, ['a'], new Set())?.id).toBe('b')
    expect(currentCard(deck, ['a'], new Set(['b']))?.id).toBe('d')
    expect(currentCard(deck, ['a', 'd'], new Set(['b']))).toBeNull()
    expect(currentCard([], [], new Set())).toBeNull()
  })
  it('Undo of a done card brings it back in its place', () => {
    const deck = deckOf(rows)
    const gone = new Set(['a'])
    expect(currentCard(deck, [], gone)?.id).toBe('b')
    gone.delete('a')
    expect(currentCard(deck, [], gone)?.id).toBe('a')
  })
  it('left drops what is gone, not what is skipped', () => {
    expect(left(deckOf(rows), new Set(['b'])).map((r) => r.id)).toEqual(['a', 'd'])
  })
})

describe('skip set', () => {
  it('skips once, in order, and back takes the last skip back', () => {
    let s = skipCard([], 'a')
    s = skipCard(s, 'b')
    s = skipCard(s, 'a')
    expect(s).toEqual(['a', 'b'])
    expect(backCard(s)).toEqual(['a'])
    expect(backCard([])).toEqual([])
  })
  it('does not change the list it is given', () => {
    const s = ['a']
    skipCard(s, 'b')
    backCard(s)
    expect(s).toEqual(['a'])
  })
})

describe('words', () => {
  it('header counts need, ready, and the reply batch', () => {
    const rows = [row('a'), row('b', { ready: true })]
    expect(headerLine(rows, { reply: 2, replyMinutes: 40 })).toBe(
      '2 need you · 1 ready · reply batch 2, about 40 min',
    )
    expect(headerLine([row('a')], { reply: 0, replyMinutes: 0 })).toBe('1 needs you · 0 ready')
  })
  it('names groups and efforts, nothing for unknown ones', () => {
    expect(groupWords('reply')).toBe('Reply')
    expect(groupWords('not_yet')).toBe('')
    expect(groupWords(null)).toBe('')
    expect(effortWords('xs')).toBe('xs, under 15 min')
    expect(effortWords(undefined)).toBe('')
  })
  it('the owner lands on Next, a read-only key on the agenda', () => {
    const base = { vault: 'acme', key: 'k', surface: 'pwa' }
    expect(homeFor({ ...base, scopes: ['review:decide'] })).toBe('/next')
    expect(homeFor({ ...base, admin: true })).toBe('/next')
    expect(homeFor({ ...base, scopes: ['vault:read'] })).toBe('/agenda')
    expect(homeFor(null)).toBe('/agenda')
  })
})

describe('Next keys', () => {
  const press = (key: string, extra: Partial<Parameters<typeof nextKeyFor>[0]> = {}) =>
    nextKeyFor({ key, ctrlKey: false, metaKey: false, altKey: false, target: null, ...extra })
  it('maps the card keys', () => {
    expect(press('j')).toBe('next')
    expect(press('n')).toBe('next')
    expect(press('k')).toBe('back')
    expect(press('d')).toBe('done')
    expect(press('l')).toBe('later')
    expect(press('s')).toBe('send')
    expect(press('a')).toBe('approve')
    expect(press('o')).toBe('open')
    expect(press('x')).toBeNull()
  })
  it('does nothing while typing or with a modifier', () => {
    expect(press('d', { target: { tagName: 'TEXTAREA' } })).toBeNull()
    expect(press('d', { ctrlKey: true })).toBeNull()
  })
  it('each key is listed once and no key is a tab digit', () => {
    const keys = NEXT_KEYS.map((k) => k.key)
    expect(new Set(keys).size).toBe(keys.length)
    for (const t of TABS) expect(keys).not.toContain(t.key)
  })
  it('Next is a tab', () => {
    expect(TABS.find((t) => t.to === '/next')?.key).toBe('7')
  })
})
