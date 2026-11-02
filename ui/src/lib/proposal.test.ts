import { describe, expect, it } from 'vitest'
import { diffKind, parseInputObject, quoteAnchors } from '@/lib/proposal'

const item = {
  op: 'task_add',
  target: null,
  body: 'Hi Sam, could you raise the quota?',
  input: { title: 'Write the plan', project: 'project-x' },
  evidence: { quote: 'Sam will raise it', locator: 'minute 3' },
}

describe('proposal helpers', () => {
  it('anchors a quote inside one part of an item', () => {
    expect(quoteAnchors(item, 'raise the quota')).toBe(true)
    expect(quoteAnchors(item, 'Write the plan')).toBe(true)
    expect(quoteAnchors(item, 'Sam will raise it')).toBe(true)
    expect(quoteAnchors(item, 'not in the item')).toBe(false)
    expect(quoteAnchors(item, '   ')).toBe(false)
  })
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
