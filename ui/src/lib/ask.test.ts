import { describe, expect, it } from 'vitest'
import {
  droppedNote,
  httpUrl,
  needsLink,
  openTargets,
  replyText,
  settlesAsk,
  targetKey,
} from './ask'

const t = (to: string, medium: string, state: string) => ({ to, medium, state })

describe('ask helpers', () => {
  const ts = [t('a', 'email', 'sent'), t('b', 'slack', 'replied'), t('c', 'phone', 'drafted')]
  it('lists the targets that have not replied', () => {
    expect(openTargets(ts).map((x) => x.to)).toEqual(['a', 'c'])
  })
  it('settles the Ask only when the last open target replies', () => {
    expect(settlesAsk(ts, targetKey(ts[0]))).toBe(false)
    expect(settlesAsk([ts[0], ts[1]], targetKey(ts[0]))).toBe(true)
  })
  it('puts where it was heard in front of the reply', () => {
    expect(replyText('  Yes, Friday ', 'in person')).toBe('Heard in person: Yes, Friday')
    expect(replyText('Yes', '')).toBe('Yes')
  })
  it('wants a link for a no only', () => {
    expect(needsLink('no')).toBe(true)
    expect(needsLink('answered')).toBe(false)
  })
  it('checks links and notes', () => {
    expect(httpUrl('https://x.example/m/1')).toBe(true)
    expect(httpUrl('in the hall')).toBe(false)
    expect(droppedNote(' no longer needed ')).toBe('Dropped: no longer needed')
  })
})
