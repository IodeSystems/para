import { describe, expect, it } from 'vitest'
import { failText, toastText } from '@/lib/stackToast'

describe('toastText', () => {
  it('says what was done in plain words', () => {
    expect(toastText('done', ['Pay vendor'])).toBe('Marked done: Pay vendor')
    expect(toastText('later', ['A', 'B', 'C'])).toBe('Put off 3 items')
  })
  it('says a failure', () => {
    expect(failText('return', ['A'], 'refused')).toBe('Could not bring back A: refused')
    expect(failText('later', ['A', 'B'], 'gone')).toBe('Could not put off 2 items: gone')
  })
})
