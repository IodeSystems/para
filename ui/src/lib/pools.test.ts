import { describe, expect, it } from 'vitest'
import { isPoolRow, nextFire, pickWords } from '@/lib/pools'

describe('pools', () => {
  it('knows a pool row', () => {
    expect(isPoolRow({ id: 'pool:build-day', kind: 'pool' })).toBe(true)
    expect(isPoolRow({ id: 'task-a', kind: 'task' })).toBe(false)
  })
  it('says what a pick did', () => {
    expect(pickWords(1, 1, 'Build day')).toBe('Picked 1 idea; 1 now prep for Build day')
    expect(pickWords(2, 0, '')).toBe('Picked 2 ideas; back in the order now')
  })
  it('says when a pool fires', () => {
    expect(nextFire({ fires: true })).toBe('fires now')
    expect(nextFire({ fires: false, next: '2027-01-01' })).toBe('next 2027-01-01')
    expect(nextFire({ fires: false, problem: 'no item task-x' })).toBe('no item task-x')
    expect(nextFire({ fires: false })).toBe('waits on its trigger')
  })
})
