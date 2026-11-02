// People read names. An API view carries a name beside each id; when the name is missing (the
// document is gone, or it is the vault itself), words made from the id are better than the id.

const PREFIX =
  /^(task|project|area|person|policy|question|rfx|proposal|request|incident|note|org|domain|group|team|role|inbox|constraint|exception|transcript)-/

// nameOr is the name, or the id in words.
export function nameOr(name: string | null | undefined, id: string | null | undefined): string {
  if (name) return name
  if (!id) return ''
  if (id === 'root') return 'the whole vault'
  if (id === 'me' || id === 'self') return 'you'
  return id
    .replace(/^agent:/, '')
    .replace(PREFIX, '')
    .replace(/[-_]/g, ' ')
}

// nameList pairs a list of ids with its parallel list of names.
export function nameList(
  ids: readonly string[] | null | undefined,
  names: readonly string[] | null | undefined,
): string[] {
  return (ids ?? []).map((id, i) => nameOr(names?.[i], id))
}

// joinNames is "A", "A and B", "A, B and C".
export function joinNames(list: readonly string[]): string {
  if (list.length <= 1) return list[0] ?? ''
  return `${list.slice(0, -1).join(', ')} and ${list[list.length - 1]}`
}

// ID_TOKEN is a generated document id inside running text: a type word and a long base-36 stamp.
const ID_TOKEN = new RegExp(`\\s*\\b(?:${PREFIX.source.slice(2, -2)})-[a-z0-9]{12,}\\b`, 'g')

// withNames swaps each id found in a title for its name. Ids without a name are left to
// stripIds.
export function withNames(
  title: string,
  ids: readonly string[] | null | undefined,
  names: readonly string[] | null | undefined,
): string {
  let out = title
  ;(ids ?? []).forEach((id, i) => {
    const n = names?.[i]
    if (n) out = out.split(id).join(n)
  })
  return out
}

// stripIds drops generated ids from running text: "Revise proposal proposal-1m6d..." reads
// "Revise proposal".
export function stripIds(text: string): string {
  return text.replace(ID_TOKEN, '').trim()
}
