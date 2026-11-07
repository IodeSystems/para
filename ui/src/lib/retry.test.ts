import { describe, expect, it } from 'vitest'
import { lineDiff, parseInput, readRetryFix } from './retry'

const body = [
  'Op: note_add',
  'Actor: agent',
  'Input hash: 00',
  '',
  '## Findings',
  '',
  '```text',
  'note.md:10: error [raw-html] raw HTML "<heading>" is not allowed',
  '```',
  '',
  '## Refused input',
  '',
  '```json',
  '{',
  '  "title": "Plan"',
  '}',
  '```',
  '',
  '## Proposed fix',
  '',
  'Changes the meaning.',
  '',
  '```json',
  '{',
  '  "title": "Plan, plain"',
  '}',
  '```',
].join('\n')

describe('readRetryFix', () => {
  it('reads the op, findings, refused input, and the proposal', () => {
    const f = readRetryFix(body)
    expect(f?.op).toBe('note_add')
    expect(f?.findings).toContain('raw-html')
    expect(f?.refused).toContain('"Plan"')
    expect(f?.proposed).toContain('Plan, plain')
  })
  it('is null with no proposal or not a retry request', () => {
    expect(readRetryFix(body.split('## Proposed fix')[0])).toBeNull()
    expect(readRetryFix('## Result\n\ndone')).toBeNull()
  })
})

describe('lineDiff', () => {
  it('marks added, removed, and same lines', () => {
    expect(lineDiff('a\nb\nc', 'a\nx\nc')).toEqual(['  a', '- b', '+ x', '  c'])
  })
})

describe('parseInput', () => {
  it('accepts an object and refuses the rest', () => {
    expect(parseInput('{"a":1}').input).toEqual({ a: 1 })
    expect(parseInput('[1]').error).toBeTruthy()
    expect(parseInput('{').error).toBeTruthy()
  })
})
