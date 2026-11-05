// The exact change of a vault change is one line per field, "Label: value". A short value stays in
// a compact list. A long one, or one over several lines, is text (a message, a note body): it is
// drawn as markdown, collapsed.

export type ChangeField = { label: string; value: string; long: boolean }

// LONG_CHARS is the length from which a value is long text.
export const LONG_CHARS = 140

// parseChange splits each line at its first ": ". A line with no label is all value.
export function parseChange(lines: string[]): ChangeField[] {
  return lines.map((line) => {
    const at = line.indexOf(': ')
    const hasLabel = at > 0 && at <= 40 && !line.slice(0, at).includes('\n')
    const l = hasLabel ? line.slice(0, at) : ''
    const value = hasLabel ? line.slice(at + 2) : line
    return { label: l, value, long: value.includes('\n') || value.length > LONG_CHARS }
  })
}
