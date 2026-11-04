import { describe, expect, it } from 'vitest'
import { ioParty, ioTarget } from '@/lib/io'

describe('io parties', () => {
  it('reads a link as an id and text as text', () => {
    expect(ioTarget('[[person-ada-example]]')).toBe('person-ada-example')
    expect(ioTarget('ada@acme.example')).toBe('')
    expect(ioParty('[[person-ada-example]]')).toBe('ada example')
    expect(ioParty('ada@acme.example')).toBe('ada@acme.example')
    expect(ioParty(null)).toBe('')
  })
})
