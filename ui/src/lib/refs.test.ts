import { describe, expect, it } from 'vitest'
import { dedupe, refKind, refLinks } from '@/lib/refs'

describe('refs', () => {
  it('uses the resolved list and drops repeats', () => {
    const l = refLinks({
      sources: ['mail:1', 'mail:1'],
      sourceLinks: [
        { text: 'mail:1', url: 'https://mail.example.test/#all/1' },
        { text: 'mail:1b', url: 'https://mail.example.test/#all/1' },
      ],
    })
    expect(l).toHaveLength(1)
  })
  it('falls back to the stored strings, linking web addresses', () => {
    const l = refLinks({ sources: ['https://a.test/x', 'https://a.test/x', 'note-1', 'note-1'] })
    expect(l).toEqual([{ text: 'https://a.test/x', url: 'https://a.test/x' }, { text: 'note-1' }])
  })
  it('says how a link opens', () => {
    expect(refKind({ text: 'a', url: '/docs/a', internal: true })).toBe('app')
    expect(refKind({ text: 'a', url: 'https://a.test' })).toBe('web')
    expect(refKind({ text: 'a', url: 'ftp://a.test' })).toBe('none')
    expect(refKind({ text: 'a' })).toBe('none')
    expect(refKind({ text: 'a', url: '//evil.test', internal: true })).toBe('none')
  })
  it('keeps order in dedupe', () => {
    expect(dedupe([{ text: 'b' }, { text: 'a' }, { text: 'b' }]).map((x) => x.text)).toEqual([
      'b',
      'a',
    ])
  })
})
