// How the owner does a task: the page, the exact command, and a few steps. Pure rules for the
// "Do it" block. No secret goes through here: a step says where to paste one.

export type How = { doUrl?: string | null; doCommand?: string | null; steps?: string | null }

// hasHow says the task tells the owner how to do it.
export const hasHow = (h: How): boolean =>
  !!(h.doUrl?.trim() || h.doCommand?.trim() || h.steps?.trim())

// openableUrl returns the address only when it is http(s): the block never opens another scheme.
export function openableUrl(u?: string | null): string | undefined {
  const s = (u ?? '').trim()
  return /^https?:\/\/\S+$/i.test(s) ? s : undefined
}

// stepLines splits a markdown list into its lines without the bullet or number. A line that is
// not a list item stays a line.
export function stepLines(steps?: string | null): string[] {
  return (steps ?? '')
    .split('\n')
    .map((l) => l.replace(/^\s*(?:[-*+]|\d+[.)])\s+/, '').trim())
    .filter(Boolean)
}
