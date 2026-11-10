import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  COLLAPSE_MS,
  DWELL_MS,
  FADE_MS,
  isToastControl,
  swipeDismisses,
  SWIPE_PX,
  ToastStore,
  toastRoute,
} from '@/lib/toasts'

beforeEach(() => vi.useFakeTimers())
afterEach(() => vi.useRealTimers())

const phases = (s: ToastStore) => s.getSnapshot().map((t) => `${t.text}:${t.phase}`)

describe('toast link to the item', () => {
  it('picks the page of the item', () => {
    expect(toastRoute({ id: 'task-a' })).toEqual({ to: '/docs/$id', params: { id: 'task-a' } })
    expect(toastRoute({ id: 'io-1', kind: 'io' })).toEqual({
      to: '/io/$id',
      params: { id: 'io-1' },
    })
    expect(toastRoute({ id: 'rfx-1', kind: 'rfx' }).to).toBe('/rfx/$id')
    expect(toastRoute({ id: 'inc-1', kind: 'incident' }).to).toBe('/incidents/$id')
  })
  it('keeps the target on the toast; a button or link click is not the toast click', () => {
    const s = new ToastStore()
    s.push({ text: 'Marked done: A', target: { id: 'task-a' } })
    expect(s.getSnapshot()[0].target).toEqual({ id: 'task-a' })
    // The test runs without a DOM: a stand-in for the clicked element answers closest().
    const inside = (tag: string) =>
      ({
        closest: (sel: string) =>
          sel
            .split(',')
            .map((x) => x.trim())
            .includes(tag)
            ? {}
            : null,
      }) as unknown as Element
    expect(isToastControl(inside('button'))).toBe(true)
    expect(isToastControl(inside('a'))).toBe(true)
    expect(isToastControl(inside('span'))).toBe(false)
    expect(isToastControl(null)).toBe(false)
  })
})

describe('toast lifecycle', () => {
  it('dwells, fades, collapses, then is removed', () => {
    const s = new ToastStore()
    s.push({ text: 'Declined: A' })
    expect(phases(s)).toEqual(['Declined: A:show'])
    vi.advanceTimersByTime(DWELL_MS - 1)
    expect(phases(s)).toEqual(['Declined: A:show'])
    vi.advanceTimersByTime(1)
    expect(phases(s)).toEqual(['Declined: A:fade'])
    vi.advanceTimersByTime(FADE_MS)
    expect(phases(s)).toEqual(['Declined: A:collapse'])
    vi.advanceTimersByTime(COLLAPSE_MS)
    expect(phases(s)).toEqual([])
  })

  it('pauses while held and resumes with the time left', () => {
    const s = new ToastStore()
    const id = s.push({ text: 'A' })
    vi.advanceTimersByTime(4000)
    s.hold(id)
    vi.advanceTimersByTime(60_000)
    expect(phases(s)).toEqual(['A:show'])
    s.release(id)
    vi.advanceTimersByTime(DWELL_MS - 4000 - 1)
    expect(phases(s)).toEqual(['A:show'])
    vi.advanceTimersByTime(1)
    expect(phases(s)).toEqual(['A:fade'])
  })

  it('keeps several toasts, each on its own clock', () => {
    const s = new ToastStore()
    s.push({ text: 'A' })
    vi.advanceTimersByTime(1000)
    s.push({ text: 'B' })
    expect(s.getSnapshot()).toHaveLength(2)
    vi.advanceTimersByTime(DWELL_MS - 1000 + FADE_MS + COLLAPSE_MS)
    expect(phases(s)).toEqual(['B:show'])
  })

  it('shows and removes with no fade when motion is reduced', () => {
    const s = new ToastStore({ reduced: () => true })
    s.push({ text: 'A' })
    vi.advanceTimersByTime(DWELL_MS)
    expect(phases(s)).toEqual([])
  })

  it('notifies subscribers with a new snapshot on each change', () => {
    const s = new ToastStore()
    const fn = vi.fn()
    s.subscribe(fn)
    const before = s.getSnapshot()
    s.push({ text: 'A' })
    expect(fn).toHaveBeenCalled()
    expect(s.getSnapshot()).not.toBe(before)
  })
})

describe('toast undo', () => {
  it('runs the undo, removes the toast, and confirms Undone', async () => {
    const s = new ToastStore({ reduced: () => true })
    const undo = vi.fn().mockResolvedValue(undefined)
    const id = s.push({ text: 'Approved: A', undo })
    expect(await s.runUndo(id)).toBe(true)
    expect(undo).toHaveBeenCalledOnce()
    expect(phases(s)).toEqual(['Undone:show'])
    expect(s.getSnapshot()[0]?.undo).toBeUndefined()
  })

  it('does not expire while the undo runs', async () => {
    const s = new ToastStore()
    let finish: () => void = () => {}
    const id = s.push({ text: 'A', undo: () => new Promise<void>((r) => (finish = r)) })
    const run = s.runUndo(id)
    vi.advanceTimersByTime(DWELL_MS * 2)
    expect(phases(s)).toEqual(['A:show'])
    finish()
    await run
    expect(phases(s)).toContain('Undone:show')
  })

  it('keeps the toast and shows an error when the undo fails', async () => {
    const s = new ToastStore({ reduced: () => true, describe: (e) => (e as Error).message })
    const id = s.push({ text: 'A', undo: () => Promise.reject(new Error('changed since')) })
    expect(await s.runUndo(id)).toBe(false)
    expect(phases(s)).toEqual(['A:show', 'Undo failed: changed since:show'])
    expect(s.getSnapshot()[0]?.undoing).toBe(false)
  })

  it('ignores Undo on a toast that has none', async () => {
    const s = new ToastStore()
    const id = s.push({ text: 'Sent: A', note: 'Cannot be undone.' })
    expect(await s.runUndo(id)).toBe(false)
  })
})

describe('toast swipe', () => {
  it('dismisses past SWIPE_PX either way, springs back short of it', () => {
    expect(swipeDismisses(SWIPE_PX)).toBe(true)
    expect(swipeDismisses(-SWIPE_PX - 1)).toBe(true)
    expect(swipeDismisses(SWIPE_PX - 1)).toBe(false)
    expect(swipeDismisses(0)).toBe(false)
  })
})
