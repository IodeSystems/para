import { describe, expect, it } from 'vitest'
import { canWait, depActions } from './depends'

describe('depActions', () => {
  it('changes the edge on this item for Waits on', () => {
    const a = depActions('task-a', 'waits', { id: 'task-b', optional: false, via: 'depends_on' })
    expect(a).toEqual({
      remove: { id: 'task-a', on: 'task-b', remove: true },
      toggle: { id: 'task-a', on: 'task-b', optional: true },
      toggleLabel: 'Make optional',
    })
  })
  it('changes the edge on the waiter for Blocks', () => {
    const a = depActions('task-b', 'blocks', {
      id: 'task-a',
      optional: true,
      via: 'optional_after',
    })
    expect(a?.remove).toEqual({ id: 'task-a', on: 'task-b', remove: true })
    expect(a?.toggle).toEqual({ id: 'task-a', on: 'task-b', optional: false })
    expect(a?.toggleLabel).toBe('Make required')
  })
  it('removes a text reason, with no toggle', () => {
    const a = depActions('task-a', 'waits', {
      reason: 'vendor reply',
      optional: false,
      via: 'blocked_by',
    })
    expect(a).toEqual({ remove: { id: 'task-a', reason: 'vendor reply', remove: true } })
  })
  it("leaves an ask's blocks alone", () => {
    expect(depActions('task-a', 'waits', { id: 'rfx-1', optional: false, via: 'rfx_blocks' })).toBe(
      null,
    )
  })
})

describe('canWait', () => {
  it('is true for the types that hold depends_on', () => {
    expect(['task', 'obligation', 'rfx', 'question'].every(canWait)).toBe(true)
    expect(canWait('project')).toBe(false)
    expect(canWait(undefined)).toBe(false)
  })
})
