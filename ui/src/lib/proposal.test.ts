import { describe, expect, it } from 'vitest'
import { diffKind, parseInputObject } from '@/lib/proposal'

describe('proposal helpers', () => {
  it('reads diff lines', () => {
    expect(diffKind('+ new')).toBe('add')
    expect(diffKind('- old')).toBe('del')
    expect(diffKind('  same')).toBe('same')
  })
  it('accepts only a JSON object as input', () => {
    expect(parseInputObject('{"a":1}')).toEqual({ ok: true, value: { a: 1 } })
    expect(parseInputObject('[1]').ok).toBe(false)
    expect(parseInputObject('{bad').ok).toBe(false)
    expect(parseInputObject('null').ok).toBe(false)
  })
})
