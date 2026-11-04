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
