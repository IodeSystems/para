import type { Para_ProduceInputMedium } from '@/gql/graphql'

// Recurring artifacts: obligations an agent owes, with what it produces (PU-REC).

export type ProduceKind = 'report' | 'message' | 'doc' | 'journal' | 'other'

export type RecurringInput = {
  title: string
  agent: string
  kind: ProduceKind
  // repeat is a recur rule (daily, weekly, monthly, ...) for a series, or ongoing with a cadence.
  repeat: string
  ongoing: boolean
  due: string
  template: string
  to: string[]
  medium: Para_ProduceInputMedium | ''
}

// recurringBody is the obligation_add input of the form: a commitment the agent owes.
export function recurringBody(i: RecurringInput) {
  const agent = i.agent.trim()
  return {
    title: i.title.trim(),
    kind: 'commitment' as const,
    owedBy: [agent === '' || agent === 'agent' ? 'agent' : `agent:${agent.replace(/^agent:/, '')}`],
    ...(i.ongoing ? { cadence: i.repeat } : { recur: i.repeat, due: i.due || undefined }),
    produce: {
      kind: i.kind,
      template: i.template.trim() || undefined,
      ...(i.kind === 'message' ? { to: i.to, medium: i.medium || undefined } : {}),
    },
  }
}

// What an agent produces for an obligation it owes, as task_show returns it.
export type ProduceInfo = {
  agent: string
  kind: string
  template?: string | null
  templateName?: string | null
  to?: string[] | null
  medium?: string | null
  builtin?: string | null
  ready: boolean
  request?: string | null
  requestStatus?: string | null
  artifact?: {
    id: string
    type: string
    title: string
    state: string
    occurrence: string
    at: string
  } | null
}

export const KIND_WORD: Record<string, string> = {
  journal: 'Journal summary',
  report: 'Report',
  message: 'Message',
  doc: 'Document',
  other: 'Artifact',
}

// templateId is the document a stored template links ("[[id]]"), or null for inline text.
export function templateId(t?: string | null): string | null {
  const m = /^\[\[([^\]#|]+)\]\]$/.exec(t ?? '')
  return m ? m[1] : null
}

// artifactState is the owner's word for the state of the last artifact.
export function artifactState(p: ProduceInfo): string {
  const a = p.artifact
  if (!a)
    return p.request ? `the agent has it (${p.requestStatus ?? 'queued'})` : 'nothing produced yet'
  if (a.type === 'io') {
    if (a.state === 'draft') return 'draft: waits for your approval'
    if (a.state === 'ready') return 'approved: not sent yet'
    return a.state
  }
  return a.state
}
