import { describe, expect, it } from 'vitest'
import { hasHow, openableUrl, stepLines } from '@/lib/doit'

describe('doit', () => {
  it('knows when a task says how', () => {
    expect(hasHow({})).toBe(false)
    expect(hasHow({ doUrl: ' ', doCommand: '', steps: null })).toBe(false)
    expect(hasHow({ doCommand: 'acli auth login --token -' })).toBe(true)
  })
  it('opens only http(s) addresses', () => {
    expect(openableUrl('https://id.example.test/tokens')).toBe('https://id.example.test/tokens')
    expect(openableUrl('ftp://x.test/a')).toBeUndefined()
    expect(openableUrl('https://a b')).toBeUndefined()
    expect(openableUrl(null)).toBeUndefined()
  })
  it('splits a markdown list into steps', () => {
    expect(
      stepLines('1. Create a token.\n2) Run the command.\n- Paste it when asked.\n\nDone.'),
    ).toEqual(['Create a token.', 'Run the command.', 'Paste it when asked.', 'Done.'])
  })
})
