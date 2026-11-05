// diffKind names the kind of a diff line from its first characters.
export function diffKind(line: string): 'add' | 'del' | 'same' {
  if (line.startsWith('+ ')) return 'add'
  if (line.startsWith('- ')) return 'del'
  return 'same'
}

// lineDiff compares two texts line by line (longest common subsequence) and gives diff lines in
// the form diffKind reads: "+ " added, "- " removed, "  " the same.
export function lineDiff(before: string, after: string): string[] {
  const a = before === '' ? [] : before.split('\n')
  const b = after === '' ? [] : after.split('\n')
  // A very long text is shown as all removed, then all added: the table would be too big.
  if (a.length * b.length > 250_000) return [...a.map((l) => `- ${l}`), ...b.map((l) => `+ ${l}`)]
  const n = a.length
  const m = b.length
  const lcs: number[][] = Array.from({ length: n + 1 }, () =>
    Array.from({ length: m + 1 }, () => 0),
  )
  for (let i = n - 1; i >= 0; i--) {
    const row = lcs[i] ?? []
    const below = lcs[i + 1] ?? []
    for (let j = m - 1; j >= 0; j--) {
      row[j] = a[i] === b[j] ? (below[j + 1] ?? 0) + 1 : Math.max(below[j] ?? 0, row[j + 1] ?? 0)
    }
  }
  const out: string[] = []
  let i = 0
  let j = 0
  while (i < n && j < m) {
    if (a[i] === b[j]) {
      out.push(`  ${a[i]}`)
      i++
      j++
    } else if ((lcs[i + 1]?.[j] ?? 0) >= (lcs[i]?.[j + 1] ?? 0)) {
      out.push(`- ${a[i]}`)
      i++
    } else {
      out.push(`+ ${b[j]}`)
      j++
    }
  }
  for (; i < n; i++) out.push(`- ${a[i]}`)
  for (; j < m; j++) out.push(`+ ${b[j]}`)
  return out
}

// splitDiff gives one diff line per text line: a line that holds newlines is split, and each part
// keeps the line's "+ ", "- ", or "  " mark.
export function splitDiff(lines: string[]): string[] {
  const out: string[] = []
  for (const l of lines) {
    const parts = l.replace(/\r\n?/g, '\n').split('\n')
    const mark = /^[+-] |^ {2}/.test(l) ? l.slice(0, 2) : ''
    // A part that has its own mark (diff lines joined into one string) keeps it.
    parts.forEach((p, n) => out.push(n === 0 || mark === '' || /^[+-] /.test(p) ? p : mark + p))
  }
  return out
}

// NewDoc is a new document read from its all-added diff: the frontmatter as key and value, and
// the body.
export type NewDoc = { fields: { key: string; value: string }[]; body: string }

// newDoc reads a diff whose every line is added as a new document. It is null when a line is not
// added or there is no frontmatter.
export function newDoc(lines: string[]): NewDoc | null {
  const all = splitDiff(lines)
  if (all.length === 0 || !all.every((l) => l.startsWith('+ ') || l === '+')) return null
  const text = all.map((l) => l.slice(2))
  if (text[0] !== '---') return null
  const end = text.indexOf('---', 1)
  if (end < 0) return null
  const fields: NewDoc['fields'] = []
  for (const l of text.slice(1, end)) {
    const at = l.indexOf(':')
    const last = fields[fields.length - 1]
    if (at > 0 && !l.startsWith(' ') && !l.startsWith('-')) {
      fields.push({ key: l.slice(0, at), value: unquote(l.slice(at + 1).trim()) })
    } else if (last) {
      last.value = [last.value, l.trim()].filter(Boolean).join(' ')
    }
  }
  return {
    fields,
    body: text
      .slice(end + 1)
      .join('\n')
      .trim(),
  }
}

function unquote(v: string): string {
  const q = v[0]
  if (v.length >= 2 && (q === '"' || q === "'") && v.endsWith(q)) return v.slice(1, -1)
  return v
}
