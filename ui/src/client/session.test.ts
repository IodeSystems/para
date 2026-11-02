import { describe, expect, it } from 'vitest'
import { parseTokenFragment } from '@/client/session'

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
