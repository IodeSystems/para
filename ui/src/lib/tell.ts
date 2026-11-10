// Tell agent: the owner says what happened, from any page. The words are saved as an io item
// first (capture-first); then, if asked, the agent gets a turn on that item to file it.

import type { ToastTarget } from '@/lib/toasts'
import { TURN_UNDO_MS } from '@/lib/threadEdit'

// TELL_OPEN is the event the t key and the app bar button send to open the dialog.
export const TELL_OPEN = 'para:tell-open'

const FILE_IT =
  'File this: obligations, tasks, dates, people, and the specification as written; ask me in the thread if anything is unclear.'

// tellText is the turn sent to the agent: a short instruction, then the owner's words as written.
export function tellText(summary: string): string {
  return `${FILE_IT}\n\n${summary.trim()}`
}

export type TellResult = {
  // request is the turn the post made: Edit and Undo (withdraw) act on it.
  request?: string
  status: string
  note?: string | null
  // waitingNoAgent: from the thread read after the post; null when it could not be read.
  waitingNoAgent?: boolean | null
}

export const TELL_QUEUED_MS = 15000

// tellToast picks the toast after a save with "Tell my agent". With no agent connected (the thread
// says waiting_no_agent, or the post says the work is only proposed) the owner is told it is
// queued, and the toast stays longer.
export function tellToast(
  io: string,
  r: TellResult,
  acts: { undo?: () => Promise<unknown>; edit?: () => void } = {},
): {
  text: string
  link: { label: string; io: string }
  target: ToastTarget
  undo?: () => Promise<unknown>
  edit?: () => void
  dwell: number
} {
  const link = { label: 'Open the item', io }
  const target: ToastTarget = { id: io, kind: 'io' }
  // Edit and Undo take the turn back while it is queued, so the toast stays about ten seconds.
  const { undo, edit } = r.request ? acts : {}
  if (r.waitingNoAgent || r.status === 'proposed' || r.note) {
    return {
      text: 'Saved. Queued: no agent is connected on this machine; start /para loop',
      link,
      target,
      undo,
      edit,
      dwell: TELL_QUEUED_MS,
    }
  }
  return { text: 'Saved and sent to your agent', link, target, undo, edit, dwell: TURN_UNDO_MS }
}
