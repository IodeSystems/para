// Notes are the ## Notes section of an item. A save names the hash it was based on; the server
// refuses a save whose base is stale and names the current hash.

// staleRefusal says a save was refused because the notes changed since they were read.
export function staleRefusal(message: string): boolean {
  return /changed since you read them|has no notes now/.test(message)
}

// badNotesLine is the first line a save would refuse: a # or ## heading would end the section.
export function badNotesLine(md: string): string | null {
  for (const line of md.split('\n')) {
    if (line.startsWith('# ') || line.startsWith('## ')) return line
  }
  return null
}

// notesDirty says the draft differs from the saved notes, ignoring white space at the ends.
export function notesDirty(draft: string, saved: string): boolean {
  return draft.trim() !== saved.trim()
}
