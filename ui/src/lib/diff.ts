// diffKind names the kind of a diff line from its first characters.
export function diffKind(line: string): 'add' | 'del' | 'same' {
  if (line.startsWith('+ ')) return 'add'
  if (line.startsWith('- ')) return 'del'
  return 'same'
}
