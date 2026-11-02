// Helpers for the proposal screen. The review rule itself lives in the server (`actions` on each
// item); nothing here restates it.

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

// UNTRUSTED_WORDS is what the untrusted mark means, in one sentence.
export const UNTRUSTED_WORDS = 'This text came from a document, not from you.'

// EXTERNAL_PROMISE says what approving an email does.
// isOutward says whether an item is an act outside the vault: a message or an action.
export const isOutward = (kind: string) => kind !== 'para'

export const EXTERNAL_PROMISE =
  'Approving marks this OK to send. Para does not send it: send it yourself, or tell the agent to.'
