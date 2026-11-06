import { describe, expect, it } from 'vitest'
import { briefOpensItself, lineSecondary, personGroups } from '@/lib/brief'

const line = (id: string) => ({
  id,
  title: id,
  type: 'ask',
  status: 'open',
  reason: 'they asked you',
})

describe('personGroups', () => {
  it('keeps only the groups that hold lines, in brief order', () => {
    const g = personGroups({
      youAsked: [],
      theyAsked: [line('rfx-a')],
      tasks: [],
      questions: [line('q-a')],
    })
    expect(g.map((x) => x.title)).toEqual(['They asked you', 'Questions'])
  })
  it('is empty when nothing is open with the person', () => {
    expect(personGroups({ youAsked: [], theyAsked: [], tasks: [], questions: [] })).toEqual([])
  })
})

describe('lineSecondary', () => {
  it('says why, then type and status', () => {
    expect(lineSecondary(line('rfx-a'))).toBe('they asked you - ask, open')
  })
  it('leaves out a missing status', () => {
    expect(lineSecondary({ ...line('x'), status: null })).toBe('they asked you - ask')
  })
})

describe('briefOpensItself', () => {
  it('opens for a meeting that needs you only', () => {
    expect(briefOpensItself('meeting', true)).toBe(true)
    expect(briefOpensItself('meeting', false)).toBe(false)
    expect(briefOpensItself('task', true)).toBe(false)
  })
})
