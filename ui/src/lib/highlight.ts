// A search hit marks spans of its snippet. Offsets count Unicode code points, not UTF-16 units.

export type Span = { start: number; end: number }
export type Piece = { text: string; mark: boolean }

// highlight cuts a snippet into plain and marked pieces. Bad or overlapping spans are skipped.
export function highlight(snippet: string, marks: readonly Span[] | null | undefined): Piece[] {
  const cps = Array.from(snippet)
  const spans = [...(marks ?? [])]
    .filter((m) => m.start >= 0 && m.end > m.start && m.start < cps.length)
    .sort((a, b) => a.start - b.start)
  const out: Piece[] = []
  let at = 0
  for (const m of spans) {
    if (m.start < at) continue
    const end = Math.min(m.end, cps.length)
    if (m.start > at) out.push({ text: cps.slice(at, m.start).join(''), mark: false })
    out.push({ text: cps.slice(m.start, end).join(''), mark: true })
    at = end
  }
  if (at < cps.length) out.push({ text: cps.slice(at).join(''), mark: false })
  return out
}
