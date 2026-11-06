import { describe, expect, it } from 'vitest'
import { artifactState, recurringBody, templateId, type ProduceInfo } from '@/lib/recurring'

const base = {
  title: ' Weekly status ',
  agent: '',
  kind: 'report' as const,
  repeat: 'weekly',
  ongoing: false,
  due: '2026-11-13',
  template: '',
  to: [],
  medium: '' as const,
}

describe('recurringBody', () => {
  it('makes a series commitment any agent owes', () => {
    expect(recurringBody(base)).toEqual({
      title: 'Weekly status',
      kind: 'commitment',
      owedBy: ['agent'],
      recur: 'weekly',
      due: '2026-11-13',
      produce: { kind: 'report', template: undefined },
    })
  })
  it('names the agent once, and takes to and medium only for a message', () => {
    const b = recurringBody({
      ...base,
      agent: 'agent:writer',
      kind: 'message',
      to: ['group-club'],
      medium: 'email',
    })
    expect(b.owedBy).toEqual(['agent:writer'])
    expect(b.produce).toEqual({
      kind: 'message',
      template: undefined,
      to: ['group-club'],
      medium: 'email',
    })
    expect(recurringBody({ ...base, to: ['group-club'] }).produce).toEqual({
      kind: 'report',
      template: undefined,
    })
  })
  it('makes an ongoing one with a cadence and no date', () => {
    const b = recurringBody({ ...base, ongoing: true, repeat: 'monthly' })
    expect(b).toMatchObject({ cadence: 'monthly' })
    expect(b).not.toHaveProperty('recur')
    expect(b).not.toHaveProperty('due')
  })
})

describe('templateId and artifactState', () => {
  it('reads a document template, not text', () => {
    expect(templateId('[[note-status]]')).toBe('note-status')
    expect(templateId('Done, next, blocked.')).toBeNull()
  })
  it('says where the artifact is', () => {
    const p: ProduceInfo = { agent: 'agent:writer', kind: 'message', ready: false }
    expect(artifactState(p)).toBe('nothing produced yet')
    expect(artifactState({ ...p, request: 'r1', requestStatus: 'claimed' })).toBe(
      'the agent has it (claimed)',
    )
    const draft = {
      id: 'io-1',
      type: 'io',
      title: 'Update',
      state: 'draft',
      occurrence: 'o1',
      at: '',
    }
    expect(artifactState({ ...p, artifact: draft })).toBe('draft: waits for your approval')
    expect(artifactState({ ...p, artifact: { ...draft, state: 'ready' } })).toBe(
      'approved: not sent yet',
    )
  })
})
