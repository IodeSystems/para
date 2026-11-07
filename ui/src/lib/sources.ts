// Words for a source row: how an agent reaches it, and who declared it.

type Row = {
  via: string
  server?: string | null
  cadence?: string | null
  lastScan?: string | null
  reachedBy: string[]
  declared: { actor: string; fresh: boolean }[]
}

export function viaWords(r: Pick<Row, 'via' | 'server'>): string {
  switch (r.via) {
    case 'mcp':
      return `MCP${r.server ? ` (${r.server})` : ''}: agent only`
    case 'cli':
      return 'command line on this machine'
    default:
      return 'read as described'
  }
}

// reachWords names the agents that declared the source lately, then the ones the source
// names, and says so when none did.
export function reachWords(r: Pick<Row, 'reachedBy' | 'declared'>): string {
  const fresh = r.declared.filter((d) => d.fresh).map((d) => d.actor)
  const named = r.reachedBy.filter((a) => !fresh.includes(a))
  const parts: string[] = []
  if (fresh.length > 0) parts.push(`reached by ${fresh.join(', ')}`)
  else parts.push('no agent declared it lately')
  if (named.length > 0) parts.push(`listed: ${named.join(', ')}`)
  return parts.join('; ')
}

export function cadenceWords(r: Pick<Row, 'cadence'>): string {
  return r.cadence ? r.cadence : 'on demand'
}

export type IOStatsRow = {
  openReplies: number
  overdue: number
  held: number
  windows: {
    days: number
    sweeps: number
    failed: number
    in: number
    out: number
    replyHours: number
    replied: number
  }[]
}

// statsWords is one line of a source's numbers (io_stats): its first window's sweeps, messages
// in and out, and median reply time, then the replies owed now and drafts held.
export function statsWords(s: IOStatsRow | undefined): string {
  if (!s) return ''
  const parts: string[] = []
  const w = s.windows[0]
  if (w) {
    parts.push(
      `${w.days}d: ${w.sweeps} sweeps${w.failed > 0 ? ` (${w.failed} failed)` : ''}, ${w.in} in, ${w.out} out`,
    )
    if (w.replyHours >= 0) parts.push(`median reply ${w.replyHours} h`)
  }
  if (s.openReplies > 0) {
    parts.push(
      `${s.openReplies} ${s.openReplies === 1 ? 'reply' : 'replies'} owed${s.overdue > 0 ? `, ${s.overdue} overdue` : ''}`,
    )
  }
  if (s.held > 0) parts.push(`${s.held} held by the pre-send check`)
  return parts.join(' - ')
}

// kindWords is a source's class, with its vendor after a slash when set: mail/outlook.
export function kindWords(r: { system: string; vendor?: string | null }): string {
  return r.vendor ? `${r.system}/${r.vendor}` : r.system
}
