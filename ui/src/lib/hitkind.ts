// What a search hit is, in words. `kind` is a document type, or "summary" or "transcript".

const LABEL: Record<string, string> = {
  summary: 'Policy summary',
  transcript: 'Transcript',
  rfx: 'Ask',
  project: 'Goal',
  request: 'Agent work',
  inbox: 'Inbox',
  io: 'Message',
  tracked_doc: 'Tracked document',
  person: 'Person',
  proposal: 'Legacy proposal',
}

export function hitKindLabel(kind: string): string {
  if (LABEL[kind]) return LABEL[kind]
  return kind ? kind[0]?.toUpperCase() + kind.slice(1) : 'Document'
}
