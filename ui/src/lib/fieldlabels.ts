// FIELD_LABELS name the fields of a document in words, for the edit form.
const FIELD_LABELS: Record<string, string> = {
  project: 'Goal',
  area: 'Area',
  due: 'Due',
  recur: 'Repeats',
  incident: 'Incident',
  parent: 'Part of task',
  assignee: 'Assigned to',
  done_when: 'Done when',
  blocked_by: 'Blocked because',
  depends_on: 'Waits on',
  outcome: 'Outcome',
  kind: 'Kind',
  scheduled: 'Scheduled',
  end: 'Ends',
  with: 'With',
  rfx: 'Asks it settles',
  previous: 'Previous in the series',
  owner: 'Owner',
  why: 'Why',
  pivot: 'Change of course',
  validation: 'Validation',
  signals: 'Signals',
  contacts: 'Contacts',
  aliases: 'Also called',
  org: 'Organization',
  role: 'Role',
  team: 'Team',
  groups: 'Groups',
  links: 'Links',
  pointers: 'Pointers',
  tags: 'Tags',
  review_by: 'Review by',
  by: 'Set by',
  policy: 'Policy',
  policy_text: 'Policy in words',
  source: 'Source',
  visibility: 'Visibility',
  quiet_days: 'Quiet days',
  direction: 'Direction',
  targets: 'Asked of',
  question: 'Question',
  context: 'Context',
  required: 'Required',
  start: 'Starts',
  away_until: 'Away until',
}

// fieldLabel is the name of a field in words: a known label, else the field name with its
// underscores as spaces.
export function fieldLabel(name: string): string {
  const w = FIELD_LABELS[name]
  if (w) return w
  const s = name.replace(/_/g, ' ')
  return s.charAt(0).toUpperCase() + s.slice(1)
}

// linkTarget reads the id out of a stored link such as "[[task-abc]]" or "[[task-abc|name]]".
export function linkTarget(v: string): string {
  const m = /^\s*\[\[([^\]|]+)(?:\|[^\]]*)?\]\]\s*$/.exec(v)
  return m ? m[1] : v.trim()
}

// linkList reads a stored list of links (a JSON array of "[[id]]") into ids.
export function linkList(v: string): string[] {
  if (v.trim() === '') return []
  try {
    const a: unknown = JSON.parse(v)
    return Array.isArray(a)
      ? a.filter((x): x is string => typeof x === 'string').map(linkTarget)
      : []
  } catch {
    return []
  }
}

export function linkText(id: string): string {
  return `[[${id}]]`
}
