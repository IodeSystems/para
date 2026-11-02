// Browser notifications (AR phase 1). Pure parts: what counts as a notice, and the words. A notice
// holds a kind and a count only, never document text. No outside service: the Notification API
// works while a tab or the installed app is open.

export type NoticeKind = 'proposal' | 'answer' | 'work' | 'incident'

export type Counts = Record<NoticeKind, number>

export const NOTICE_KINDS: NoticeKind[] = ['proposal', 'answer', 'work', 'incident']

export const zeroCounts = (): Counts => ({ proposal: 0, answer: 0, work: 0, incident: 0 })

// A request ended in a way that waits for the owner.
export function workNeedsYou(resolution: string | null | undefined): boolean {
  return resolution === 'stuck' || resolution === 'blocked' || resolution === 'needs_clarification'
}

export type Notice = { kind: NoticeKind; count: number }

// rises lists the kinds whose count grew since prev, with the number of new items.
export function rises(prev: Counts, next: Counts): Notice[] {
  const out: Notice[] = []
  for (const kind of NOTICE_KINDS) {
    const d = next[kind] - prev[kind]
    if (d > 0) out.push({ kind, count: d })
  }
  return out
}

const WORDS: Record<NoticeKind, [string, string]> = {
  proposal: ['proposal filed', 'proposals filed'],
  answer: ['answer to check', 'answers to check'],
  work: ['agent work needs you', 'agent work items need you'],
  incident: ['incident opened', 'incidents opened'],
}

// noticeText is the whole text of a notification: "2 proposals filed".
export function noticeText(n: Notice): string {
  const [one, many] = WORDS[n.kind]
  return `${n.count} ${n.count === 1 ? one : many}`
}

const key = (vault: string) => `para.notify.${vault}`

// Opt-in is per vault and per browser: a session is bound to one vault. Off unless set.
export function notifyEnabled(vault: string): boolean {
  try {
    return localStorage.getItem(key(vault)) === '1'
  } catch {
    return false
  }
}

export function setNotifyEnabled(vault: string, on: boolean): void {
  try {
    if (on) localStorage.setItem(key(vault), '1')
    else localStorage.removeItem(key(vault))
  } catch {
    // Storage can be blocked; the choice then lasts until the page closes.
  }
  window.dispatchEvent(new Event('para-notify'))
}

export function notifySupported(): boolean {
  return typeof Notification !== 'undefined'
}
