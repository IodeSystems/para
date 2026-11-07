// A retry-write request (the vault's record of a write that lint refused) holds the op, the lint
// findings, the input the agent sent, and the fixes it proposed, as sections of its body. These
// functions read them for the owner card.

export type RetryFix = {
  op: string
  findings: string
  refused: string
  proposed: string
}

// sections splits a body into its "## " sections, name to text (a repeated name keeps the last).
function sections(body: string): Map<string, string> {
  const out = new Map<string, string>()
  for (const part of ('\n' + body).split('\n## ').slice(1)) {
    const nl = part.indexOf('\n')
    const head = (nl < 0 ? part : part.slice(0, nl)).trim()
    out.set(head, nl < 0 ? '' : part.slice(nl + 1).trim())
  }
  return out
}

// lastFence is the text of the last closed fenced block in text, or ''.
export function lastFence(text: string): string {
  const lines = text.split('\n')
  let start = -1
  let mark = ''
  let last = ''
  lines.forEach((line, i) => {
    const t = line.trim()
    if (start < 0) {
      if (t.startsWith('```')) {
        mark = t.replace(/[a-z]+$/, '')
        start = i
      }
    } else if (t === mark) {
      last = lines.slice(start + 1, i).join('\n')
      start = -1
    }
  })
  return last
}

// readRetryFix reads a request body, or null when it is not a retry-write request with a
// proposed fix for the owner to decide on.
export function readRetryFix(body: string): RetryFix | null {
  const op = /^Op: (\S+)$/m.exec(body)?.[1]
  if (!op) return null
  const s = sections(body)
  const proposed = lastFence(s.get('Proposed fix') ?? '')
  if (!proposed) return null
  return {
    op,
    findings: lastFence(s.get('Findings') ?? ''),
    refused: lastFence(s.get('Refused input') ?? ''),
    proposed,
  }
}

// lineDiff writes the difference of two texts as the lines DiffBlock reads: "+ " added, "- "
// removed, "  " the same.
export function lineDiff(a: string, b: string): string[] {
  const x = a.split('\n')
  const y = b.split('\n')
  const n = x.length
  const m = y.length
  const t: number[][] = Array.from({ length: n + 1 }, () => Array.from({ length: m + 1 }, () => 0))
  for (let i = n - 1; i >= 0; i--)
    for (let j = m - 1; j >= 0; j--)
      t[i][j] = x[i] === y[j] ? t[i + 1][j + 1] + 1 : Math.max(t[i + 1][j], t[i][j + 1])
  const out: string[] = []
  let i = 0
  let j = 0
  while (i < n && j < m) {
    if (x[i] === y[j]) {
      out.push('  ' + x[i++])
      j++
    } else if (t[i + 1][j] >= t[i][j + 1]) out.push('- ' + x[i++])
    else out.push('+ ' + y[j++])
  }
  while (i < n) out.push('- ' + x[i++])
  while (j < m) out.push('+ ' + y[j++])
  return out
}

// parseInput reads the owner's edited JSON: an object, or an error in words.
export function parseInput(text: string): { input?: Record<string, unknown>; error?: string } {
  try {
    const v: unknown = JSON.parse(text)
    if (v === null || typeof v !== 'object' || Array.isArray(v))
      return { error: 'The input must be a JSON object.' }
    return { input: v as Record<string, unknown> }
  } catch (e) {
    return { error: e instanceof Error ? e.message : 'Not valid JSON.' }
  }
}
