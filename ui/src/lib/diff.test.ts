import { describe, expect, it } from 'vitest'
import { newDoc, splitDiff } from '@/lib/diff'

describe('diff lines', () => {
  it('gives each text line its own diff line and keeps the mark', () => {
    expect(splitDiff(['+ ---\n+ type: io', '  same', '- a\nb'])).toEqual([
      '+ ---',
      '+ type: io',
      '  same',
      '- a',
      '- b',
    ])
    expect(splitDiff(['+ x'])).toEqual(['+ x'])
  })

  it('reads a new document as its fields and body', () => {
    const lines = [
      '+ ---',
      '+ type: io',
      '+ medium: slack',
      '+ at: "2026-11-05T11:01:52-08:00"',
      '+ ---',
      '+ Trigger an incident.',
    ]
    expect(newDoc(lines)).toEqual({
      fields: [
        { key: 'type', value: 'io' },
        { key: 'medium', value: 'slack' },
        { key: 'at', value: '2026-11-05T11:01:52-08:00' },
      ],
      body: 'Trigger an incident.',
    })
    expect(newDoc(['  ---', '+ x'])).toBeNull()
  })
})
