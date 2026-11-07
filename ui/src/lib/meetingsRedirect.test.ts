import { describe, expect, it } from 'vitest'
import { Route as MeetingsIndex } from '@/routes/_authed.meetings.index'
import { Route as MeetingPage } from '@/routes/_authed.meetings.$id'

// thrown runs a route's beforeLoad and returns what it threw: a redirect.
function thrown(run: () => unknown): {
  options: { to?: string; search?: unknown; params?: unknown }
} {
  try {
    run()
  } catch (e) {
    return e as { options: { to?: string; search?: unknown; params?: unknown } }
  }
  throw new Error('no redirect')
}

describe('meetings redirect to obligations', () => {
  it('sends the list to the Meetings filter', () => {
    const r = thrown(() => MeetingsIndex.options.beforeLoad?.({} as never))
    expect(r.options.to).toBe('/obligations')
    expect(r.options.search).toEqual({ filter: 'meetings' })
  })
  it('sends a meeting to its obligation page', () => {
    const r = thrown(() =>
      MeetingPage.options.beforeLoad?.({ params: { id: 'obligation-x' } } as never),
    )
    expect(r.options.to).toBe('/obligations/$id')
    expect(r.options.params).toEqual({ id: 'obligation-x' })
  })
})
