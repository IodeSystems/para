import { describe, expect, it } from 'vitest'
import { needsRenew, parseTokenFragment } from '@/client/session'

describe('parseTokenFragment', () => {
  it('reads the token', () => {
    expect(parseTokenFragment('#token=abc123')).toBe('abc123')
    expect(parseTokenFragment('#x=1&token=a%2Fb')).toBe('a/b')
  })
  it('ignores other fragments and empty tokens', () => {
    expect(parseTokenFragment('')).toBeNull()
    expect(parseTokenFragment('#section')).toBeNull()
    expect(parseTokenFragment('#token=')).toBeNull()
    expect(parseTokenFragment('#mytoken=abc')).toBeNull()
  })
  it('refuses a malformed escape', () => {
    expect(parseTokenFragment('#token=%E0%A4%A')).toBeNull()
  })
})

describe('needsRenew', () => {
  const now = Date.parse('2026-11-01T00:00:00Z')
  const day = 24 * 60 * 60 * 1000
  const at = (ms: number) => new Date(now + ms).toISOString()
  it('renews a login key within 7 days of its end', () => {
    expect(
      needsRenew({ vault: 'a', key: 'k', surface: 'pwa', login: true, expires: at(3 * day) }, now),
    ).toBe(true)
  })
  it('leaves a key far from its end, an ended key, and a key that is not a login key', () => {
    expect(
      needsRenew({ vault: 'a', key: 'k', surface: 'pwa', login: true, expires: at(20 * day) }, now),
    ).toBe(false)
    expect(
      needsRenew({ vault: 'a', key: 'k', surface: 'pwa', login: true, expires: at(-day) }, now),
    ).toBe(false)
    expect(needsRenew({ vault: 'a', key: 'k', surface: 'pwa', expires: at(day) }, now)).toBe(false)
    expect(needsRenew({ vault: 'a', key: 'k', surface: 'pwa', login: true }, now)).toBe(false)
    expect(needsRenew(null, now)).toBe(false)
  })
})
