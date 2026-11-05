import { describe, expect, it } from 'vitest'
import { sentStatus } from '@/lib/sent'

describe('sent to agent status', () => {
  it('says where the work stands', () => {
    expect(sentStatus({ status: 'queued' }).label).toBe('queued')
    expect(sentStatus({ status: 'claimed', claimedBy: 'claude' }).label).toBe('claude has it')
    expect(sentStatus({ status: 'done', resolution: 'done' })).toEqual({
      label: 'done',
      tone: 'good',
    })
    expect(sentStatus({ status: 'done', resolution: 'needs_clarification' })).toEqual({
      label: 'needs clarification',
      tone: 'attn',
    })
    expect(sentStatus({ status: 'done', resolution: 'partial' }).tone).toBe('wait')
    expect(sentStatus({ status: 'cancelled' }).label).toBe('taken back')
  })
})
