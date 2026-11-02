import { describe, expect, it } from 'vitest'
import { joinNames, nameList, nameOr } from '@/lib/names'

describe('names', () => {
  it('prefers the name', () => {
    expect(nameOr('Priya Raman', 'person-priya-raman')).toBe('Priya Raman')
  })
  it('falls back to the id in words', () => {
    expect(nameOr('', 'project-launch-plan')).toBe('launch plan')
    expect(nameOr(null, 'root')).toBe('the whole vault')
    expect(nameOr(undefined, undefined)).toBe('')
  })
  it('pairs ids with parallel names', () => {
    expect(nameList(['task-a', 'task-b'], ['Alpha', ''])).toEqual(['Alpha', 'b'])
    expect(nameList(['task-a'], null)).toEqual(['a'])
  })
  it('joins in words', () => {
    expect(joinNames(['A'])).toBe('A')
    expect(joinNames(['A', 'B', 'C'])).toBe('A, B and C')
  })
})

describe('ids in text', () => {
  it('swaps ids for names, and drops the rest', async () => {
    const { stripIds, withNames } = await import('@/lib/names')
    const id = 'proposal-1m6dgwszb9wm356hn0eaw0'
    expect(withNames(`Revise proposal ${id}`, [id], ['Quota follow-ups'])).toBe(
      'Revise proposal Quota follow-ups',
    )
    expect(stripIds(`Revise proposal ${id}`)).toBe('Revise proposal')
    expect(stripIds('Revise proposal planning')).toBe('Revise proposal planning')
  })
})
