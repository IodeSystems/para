// Words for what Done, Later, and Bring back tell the owner after an action: the toast text.

export type Verb = 'later' | 'return' | 'done'

const DONE: Record<Verb, string> = {
  later: 'Put off',
  return: 'Brought back',
  done: 'Marked done',
}

const TO: Record<Verb, string> = {
  later: 'put off',
  return: 'bring back',
  done: 'mark done',
}

// toastText says what was done, in one line: "Marked done: <title>", or "Put off 3 items".
export function toastText(verb: Verb, titles: string[]): string {
  return titles.length === 1
    ? `${DONE[verb]}: ${titles[0]}`
    : `${DONE[verb]} ${titles.length} items`
}

// failText says an action failed.
export function failText(verb: Verb, titles: string[], why: string): string {
  const what = titles.length === 1 ? titles[0] : `${titles.length} items`
  return `Could not ${TO[verb]} ${what}: ${why}`
}
