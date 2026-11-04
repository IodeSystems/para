import { describe, expect, it } from 'vitest'
import { isUrl, proposedText, questionState } from './answer'

describe('isUrl', () => {
  it('knows web addresses from document ids', () => {
    expect(isUrl('https://example.com/a')).toBe(true)
    expect(isUrl('HTTP://example.com')).toBe(true)
    expect(isUrl('note-abc123')).toBe(false)
    expect(isUrl('javascript:alert(1)')).toBe(false)
  })
})

describe('questionState', () => {
  it('shows a proposal before anything else', () => {
    expect(questionState({ status: 'open', hasProposal: true, waitingOn: ['q'] })).toEqual({
      state: 'proposed',
      label: 'proposed answer',
    })
  })
  it('shows waiting for an open question with unresolved dependencies', () => {
    expect(questionState({ status: 'open', hasProposal: false, waitingOn: ['q'] }).label).toBe(
      'waiting',
    )
  })
  it('keeps the plain status otherwise', () => {
    expect(questionState({ status: 'answered', hasProposal: false, waitingOn: [] }).label).toBe(
      'answered',
    )
    expect(questionState({ status: 'open', hasProposal: false, waitingOn: [] }).state).toBe('open')
  })
})

describe('proposedText', () => {
  it('prefers text, then the choice', () => {
    expect(proposedText({ proposedAnswer: 'a', proposedChoice: 'b' })).toBe('a')
    expect(proposedText({ proposedChoice: 'b' })).toBe('b')
    expect(proposedText({})).toBe('')
  })
})
