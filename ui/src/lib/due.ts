// Due dates are YYYY-MM-DD in the person's zone. Dates compare as strings.

export type DueState = 'overdue' | 'today' | 'soon' | 'later'

// dueState says how near a due date is. `soonDays` days ahead count as soon.
export function dueState(due: string, today: string, soonDays = 3): DueState {
  if (due < today) return 'overdue'
  if (due === today) return 'today'
  const end = new Date(`${today}T00:00:00Z`)
  end.setUTCDate(end.getUTCDate() + soonDays)
  return due <= end.toISOString().slice(0, 10) ? 'soon' : 'later'
}

import { dateWords } from '@/lib/when'

// dueLabel is the text of a due chip.
export function dueLabel(due: string, today: string): string {
  const s = dueState(due, today)
  return s === 'overdue'
    ? `overdue ${dateWords(due, today)}`
    : s === 'today'
      ? 'due today'
      : `due ${dateWords(due, today)}`
}
