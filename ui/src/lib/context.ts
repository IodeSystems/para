// Helpers for the Context block at the top of an item page: who the item is for, and the quick
// capture of a conversation (verbal) or of something carried over by hand (swivel).

const ROLES: Record<string, string> = {
  requested_by: 'Requested by',
  owed_to: 'Owed to',
  owed_by: 'Owed by',
  with: 'With',
  assignee: 'Assigned to',
}

// roleLabel names the role of a party the item is for.
export function roleLabel(role: string): string {
  return ROLES[role] ?? role
}

type Party = { role: string; name: string; id?: string | null }

// forGroups puts the parties in role order, one group per role, so "Owed to Ada, Bo" reads as one
// line.
export function forGroups<P extends Party>(parties: P[]): { role: string; parties: P[] }[] {
  const out: { role: string; parties: P[] }[] = []
  for (const role of Object.keys(ROLES)) {
    const ps = parties.filter((p) => p.role === role)
    if (ps.length > 0) out.push({ role, parties: ps })
  }
  for (const p of parties) {
    if (!(p.role in ROLES) && !out.some((g) => g.role === p.role)) {
      out.push({ role: p.role, parties: parties.filter((x) => x.role === p.role) })
    }
  }
  return out
}

// CaptureKind is what the owner logs by hand: a conversation they had, or something they carried
// over by hand from another system.
export type CaptureKind = 'verbal' | 'swivel'

export type Capture = {
  kind: CaptureKind
  // with: verbal, the people or groups: a vault id, or a name as text.
  with: { id?: string; name: string }[]
  // from: swivel, the system it came from: a label or a URL.
  from: string
  date: string // YYYY-MM-DD
  summary: string
  url: string
}

// CaptureCall is the op a capture runs and its input. A conversation with people who are all in
// the vault is a logged contact (interaction_add: closed, dated). With a name not in the vault, or
// for something carried over, it is an incoming io item to sort (io_add), dated at noon that day.
export type CaptureCall =
  | {
      op: 'interaction_add'
      body: {
        summary: string
        with: string[]
        medium: 'verbal'
        direction: 'in'
        date: string
        url?: string
      }
    }
  | {
      op: 'io_add'
      body: {
        direction: 'in'
        medium: CaptureKind
        text: string
        from: string
        at: string
        ref?: string
      }
    }

// noonOf is local noon of a day as an RFC 3339 time.
function noonOf(date: string): string {
  return new Date(`${date}T12:00:00`).toISOString()
}

// captureCall turns the capture form into the op to run.
export function captureCall(c: Capture): CaptureCall {
  const summary = c.summary.trim()
  const url = c.url.trim() || undefined
  if (c.kind === 'verbal' && c.with.length > 0 && c.with.every((w) => w.id)) {
    return {
      op: 'interaction_add',
      body: {
        summary,
        with: c.with.map((w) => w.id as string),
        medium: 'verbal',
        direction: 'in',
        date: c.date,
        url,
      },
    }
  }
  const from =
    c.kind === 'verbal'
      ? c.with.map((w) => (w.id ? w.id : w.name.trim())).join(', ')
      : c.from.trim()
  return {
    op: 'io_add',
    body: { direction: 'in', medium: c.kind, text: summary, from, at: noonOf(c.date), ref: url },
  }
}

// captureReady says whether the form can be sent: a summary and a date, and who it was with or
// where it came from.
export function captureReady(c: Capture): boolean {
  if (!c.summary.trim() || !/^\d{4}-\d{2}-\d{2}$/.test(c.date)) return false
  return c.kind === 'verbal' ? c.with.some((w) => w.id || w.name.trim()) : c.from.trim() !== ''
}
