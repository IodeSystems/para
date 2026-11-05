// The toast store: what the owner just did, one line each, with Undo when the action can be taken
// back. A toast dwells, pauses while hovered or focused, fades, collapses so the toasts below slide
// up, and is removed. The store holds no React and no DOM, so the lifecycle is testable with fake
// timers.

export const DWELL_MS = 6000
export const FADE_MS = 250
export const COLLAPSE_MS = 200

// prefersReducedMotion says the person asked for no motion: no slide, no fade, just show and remove.
export const prefersReducedMotion = (): boolean =>
  typeof window !== 'undefined' && !!window.matchMedia?.('(prefers-reduced-motion: reduce)').matches

export type ToastPhase = 'show' | 'fade' | 'collapse'

export type Toast = {
  id: number
  text: string
  tone: 'info' | 'error'
  phase: ToastPhase
  // undo takes the action back. A toast with no undo has no Undo button.
  undo?: () => Promise<unknown>
  // note is a short line under the text, such as why an action cannot be undone.
  note?: string
  undoing: boolean
}

export type ToastInput = {
  text: string
  tone?: 'info' | 'error'
  undo?: () => Promise<unknown>
  note?: string
}

type Timing = { timer?: ReturnType<typeof setTimeout>; left: number; since: number; holds: number }

export type ToastOptions = {
  // reduced says whether the person asked for no motion: then a toast shows and is removed, with no
  // fade and no slide.
  reduced?: () => boolean
  // describe turns an error into words for the "Undo failed" toast.
  describe?: (e: unknown) => string
}

export class ToastStore {
  private list: Toast[] = []
  private timing = new Map<number, Timing>()
  private subs = new Set<() => void>()
  private next = 1

  constructor(private readonly opts: ToastOptions = {}) {}

  subscribe = (fn: () => void): (() => void) => {
    this.subs.add(fn)
    return () => this.subs.delete(fn)
  }

  getSnapshot = (): readonly Toast[] => this.list

  private emit() {
    for (const fn of this.subs) fn()
  }

  private set(id: number, patch: Partial<Toast>) {
    this.list = this.list.map((t) => (t.id === id ? { ...t, ...patch } : t))
    this.emit()
  }

  private get(id: number): Toast | undefined {
    return this.list.find((t) => t.id === id)
  }

  private reduced = () => this.opts.reduced?.() ?? false

  push(t: ToastInput): number {
    const id = this.next++
    this.list = [
      ...this.list,
      {
        id,
        text: t.text,
        tone: t.tone ?? 'info',
        phase: 'show',
        undo: t.undo,
        note: t.note,
        undoing: false,
      },
    ]
    this.timing.set(id, { left: DWELL_MS, since: Date.now(), holds: 0 })
    this.emit()
    this.arm(id)
    return id
  }

  // arm starts the dwell timer with the time left.
  private arm(id: number) {
    const tm = this.timing.get(id)
    if (!tm) return
    clearTimeout(tm.timer)
    tm.since = Date.now()
    tm.timer = setTimeout(() => this.leave(id), tm.left)
  }

  // hold pauses the dwell, while the pointer is over the toast or focus is in it. Holds nest.
  hold(id: number) {
    const tm = this.timing.get(id)
    const t = this.get(id)
    if (!tm || !t || t.phase !== 'show') return
    tm.holds++
    if (tm.holds === 1) {
      clearTimeout(tm.timer)
      tm.left = Math.max(0, tm.left - (Date.now() - tm.since))
    }
  }

  release(id: number) {
    const tm = this.timing.get(id)
    const t = this.get(id)
    if (!tm || !t || t.phase !== 'show' || tm.holds === 0) return
    tm.holds--
    // A toast with an undo running stays until it is done.
    if (tm.holds === 0 && !t.undoing) this.arm(id)
  }

  // dismiss ends the dwell now: the toast fades out.
  dismiss(id: number) {
    const t = this.get(id)
    if (!t || t.phase !== 'show') return
    const tm = this.timing.get(id)
    if (tm) clearTimeout(tm.timer)
    this.leave(id)
  }

  private leave(id: number) {
    if (this.reduced()) return this.remove(id)
    const tm = this.timing.get(id)
    if (!tm) return
    this.set(id, { phase: 'fade' })
    tm.timer = setTimeout(() => {
      this.set(id, { phase: 'collapse' })
      tm.timer = setTimeout(() => this.remove(id), COLLAPSE_MS)
    }, FADE_MS)
  }

  private remove(id: number) {
    const tm = this.timing.get(id)
    if (tm) clearTimeout(tm.timer)
    this.timing.delete(id)
    this.list = this.list.filter((t) => t.id !== id)
    this.emit()
  }

  // runUndo takes the action back. On success the toast goes and "Undone" shows; on failure the toast
  // stays, with an error toast beside it.
  async runUndo(id: number): Promise<boolean> {
    const t = this.get(id)
    if (!t?.undo || t.undoing || t.phase !== 'show') return false
    const tm = this.timing.get(id)
    if (tm) clearTimeout(tm.timer)
    this.set(id, { undoing: true })
    try {
      await t.undo()
    } catch (e) {
      this.set(id, { undoing: false })
      const why = this.opts.describe
        ? this.opts.describe(e)
        : e instanceof Error
          ? e.message
          : String(e)
      this.push({ text: `Undo failed: ${why}`, tone: 'error' })
      if (tm && tm.holds === 0) {
        tm.left = DWELL_MS
        this.arm(id)
      }
      return false
    }
    this.dismiss(id)
    this.push({ text: 'Undone' })
    return true
  }
}
