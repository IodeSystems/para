import { describe, expect, it } from 'vitest'
import {
  requestEditTurn,
  takePendingEdit,
  turnEditable,
  turnWithdrawable,
  TURN_UNDO_MS,
} from './threadEdit'

describe('turn edit rules', () => {
  it('edits only a turn no agent has claimed', () => {
    expect(turnEditable('queued')).toBe(true)
    expect(turnEditable('proposed')).toBe(true)
    expect(turnEditable('claimed')).toBe(false)
    expect(turnEditable('done')).toBe(false)
    expect(turnEditable(undefined)).toBe(false)
  })
  it('withdraws a turn until it is done', () => {
    expect(turnWithdrawable('queued')).toBe(true)
    expect(turnWithdrawable('claimed')).toBe(true)
    expect(turnWithdrawable('done')).toBe(false)
    expect(turnWithdrawable('cancelled')).toBe(false)
  })
  it('hands a waiting edit to its turn once', () => {
    requestEditTurn('request-a')
    expect(takePendingEdit('request-b')).toBe(false)
    expect(takePendingEdit('request-a')).toBe(true)
    expect(takePendingEdit('request-a')).toBe(false)
  })
  it('keeps the send toast about ten seconds', () => {
    expect(TURN_UNDO_MS).toBe(10000)
  })
})
