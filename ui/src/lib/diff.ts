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
