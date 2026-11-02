// Helpers for the proposal screen. The review rule itself lives in the server (`actions` on each
// item); nothing here restates it.

export type ItemText = {
  op?: string | null
  target?: string | null
  draft?: string | null
  input?: unknown
  evidence: { quote: string; locator: string }
}

// textPieces are the parts of an item a person can select text from.
export function textPieces(it: ItemText): string[] {
  const out = [it.target, it.draft, it.op, it.evidence.quote, it.evidence.locator].filter(
    (s): s is string => typeof s === 'string' && s !== '',
  )
  if (it.input !== null && it.input !== undefined) {
    out.push(JSON.stringify(it.input, null, 2), JSON.stringify(it.input))
  }
  return out
}

// quoteAnchors says whether a quote sits inside one part of the item. The server decides what
// is anchored; this only warns before a note is sent that cannot anchor.
export function quoteAnchors(it: ItemText, quote: string): boolean {
  const q = quote.trim()
  return q !== '' && textPieces(it).some((p) => p.includes(q))
}

// diffClass names the kind of a diff line from its first character.
export function diffKind(line: string): 'add' | 'del' | 'same' {
  if (line.startsWith('+ ')) return 'add'
  if (line.startsWith('- ')) return 'del'
  return 'same'
}

// parseInputObject reads the JSON of an op item's input. It must be an object.
export function parseInputObject(
  text: string,
): { ok: true; value: Record<string, unknown> } | { ok: false; error: string } {
  let v: unknown
  try {
    v = JSON.parse(text)
  } catch (e) {
    return { ok: false, error: e instanceof Error ? e.message : 'not JSON' }
  }
  if (typeof v !== 'object' || v === null || Array.isArray(v)) {
    return { ok: false, error: 'the input must be a JSON object' }
  }
  return { ok: true, value: v as Record<string, unknown> }
}
