import { describe, expect, it } from 'vitest'
import { LONG_CHARS, parseChange } from '@/lib/change'

describe('parseChange', () => {
  it('keeps short fields as label and value', () => {
    expect(parseChange(['Direction: outgoing', 'Subject: Hello'])).toEqual([
      { label: 'Direction', value: 'outgoing', long: false },
      { label: 'Subject', value: 'Hello', long: false },
    ])
  })
  it('marks long text and multi-line text as long, to draw as markdown', () => {
    const body = '## Plan\n\n- one\n- **two**'
    const [a, b] = parseChange(['Text: ' + body, 'Body: ' + 'word '.repeat(LONG_CHARS)])
    expect(a).toEqual({ label: 'Text', value: body, long: true })
    expect(b?.long).toBe(true)
  })
  it('treats a line with no label as all value', () => {
    expect(parseChange(['just words'])).toEqual([{ label: '', value: 'just words', long: false }])
  })
})
