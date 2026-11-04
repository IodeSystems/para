// Helpers for tracked documents: where each comment's quote is in a version's text, and the text
// cut into runs so the page can mark them. Offsets count Unicode code points, as para stores
// them, not UTF-16 units.

export type Mark = { id: string; start: number; end: number }

export type Run = { text: string; ids: string[] }

type CommentLike = {
  id: string
  version: number
  start?: number | null
  end?: number | null
  anchor?: {
    version: number
    start?: number | null
    end?: number | null
    lost?: boolean | null
  } | null
}

// markOf is where a comment's quote is in version v, or null: the comment's own place when it was
// made on v, else para's anchor when that is v and not lost.
export function markOf(c: CommentLike, v: number): Mark | null {
  const a = c.anchor
  if (a && a.version === v) {
    if (a.lost || a.start == null || a.end == null) return null
    return { id: c.id, start: a.start, end: a.end }
  }
  if (c.version === v && c.start != null && c.end != null)
    return { id: c.id, start: c.start, end: c.end }
  return null
}

// runs cuts text at every mark boundary. Each run lists the marks that cover it; overlapping marks
// both list. A mark outside the text is clipped.
export function runs(text: string, marks: Mark[]): Run[] {
  const cps = Array.from(text)
  const cuts = new Set<number>([0, cps.length])
  const ok = marks
    .map((m) => ({ ...m, start: Math.max(0, m.start), end: Math.min(cps.length, m.end) }))
    .filter((m) => m.start < m.end)
  for (const m of ok) {
    cuts.add(m.start)
    cuts.add(m.end)
  }
  const at = [...cuts].sort((a, b) => a - b)
  const out: Run[] = []
  for (let i = 0; i + 1 < at.length; i++) {
    const s = at[i] ?? 0
    const e = at[i + 1] ?? 0
    if (s === e) continue
    const ids = ok.filter((m) => m.start <= s && m.end >= e).map((m) => m.id)
    out.push({ text: cps.slice(s, e).join(''), ids })
  }
  return out
}

// commentState is a comment's state in words.
export function commentState(c: { resolution?: string | null; lost?: boolean | null }): string {
  switch (c.resolution) {
    case 'became':
      return 'became an item'
    case 'resolved':
      return 'resolved'
    case 'dropped':
      return 'dropped'
  }
  return c.lost ? 'open, quote gone' : 'open'
}

// COMMENT_TARGETS are what the owner can make of a comment in an upstream item.
export const COMMENT_TARGETS: { value: string; words: string }[] = [
  { value: 'commitment', words: 'Commitment (I owe it)' },
  { value: 'ask', words: 'Ask (they ask me)' },
  { value: 'question', words: 'Open question' },
  { value: 'resolved', words: 'Resolved' },
  { value: 'dropped', words: 'Drop' },
]

type PartLike = { op: string; text: string }

// diffLines writes diff parts as the lines DiffBlock reads: "+ " added, "- " removed, "  " the
// same, and a gap as "@@ 5 unchanged @@".
export function diffLines(parts: readonly PartLike[]): string[] {
  const out: string[] = []
  for (const p of parts) {
    if (p.op === 'gap') {
      out.push(`@@ ${p.text} @@`)
      continue
    }
    const prefix = p.op === 'add' ? '+ ' : p.op === 'del' ? '- ' : '  '
    for (const l of p.text.split('\n')) out.push(prefix + l)
  }
  return out
}
