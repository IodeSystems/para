import { describe, expect, it } from 'vitest'
import { activityKind, activityTarget, activityWhat, activityWho } from '@/lib/activity'

describe('activityKind', () => {
  it('says known kinds in words and spaces the rest', () => {
    expect(activityKind('closed')).toBe('Closed')
    expect(activityKind('turn')).toBe('Turn posted')
    expect(activityKind('external')).toBe('Edited by hand')
    expect(activityKind('some_new_kind')).toBe('some new kind')
  })
})

describe('activityWho', () => {
  it('names the actor, and the machine when it is another one', () => {
    expect(activityWho({ who: 'owner' })).toBe('You')
    expect(activityWho({ who: 'agent:claude', actor: 'claude' })).toBe('claude')
    expect(activityWho({ who: 'hand edit' })).toBe('Hand edit')
    expect(activityWho({ who: 'owner', remote: true, install: 'laptop' })).toBe('You on laptop')
    expect(activityWho({ who: 'owner', remote: true })).toBe('You on another machine')
  })
})

describe('activityWhat', () => {
  const base = { kind: 'edited', count: 1, who: 'owner', doc: 'task-a' }
  it('names the item by its title, or its id when it has none', () => {
    expect(activityWhat({ ...base, docName: 'Renew the cert' })).toBe('Edited: Renew the cert')
    expect(activityWhat(base)).toBe('Edited: task-a')
  })
  it('counts a burst', () => {
    expect(activityWhat({ ...base, count: 3, docName: 'Renew the cert' })).toBe(
      'Edited (3 changes): Renew the cert',
    )
  })
})

describe('activityTarget', () => {
  it('opens each type on its own page, and the rest on the document page', () => {
    expect(activityTarget({ doc: 'rfx-a', docType: 'rfx' })).toEqual({
      to: '/rfx/$id',
      id: 'rfx-a',
    })
    expect(activityTarget({ doc: 'io-a', docType: 'io' })).toEqual({ to: '/io/$id', id: 'io-a' })
    expect(activityTarget({ doc: 'project-a', docType: 'project' }).to).toBe('/projects/$id')
    expect(activityTarget({ doc: 'obligation-a', docType: 'obligation' }).to).toBe(
      '/obligations/$id',
    )
    expect(activityTarget({ doc: 'task-a', docType: 'task' }).to).toBe('/docs/$id')
    expect(activityTarget({ doc: 'gone-a' }).to).toBe('/docs/$id')
  })
})
