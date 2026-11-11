// Pure helpers of the Output page's Proposals tab: assessments as shown, the batch an
// "Approve all shown" sends, refusals, and order.

export type AssessmentIn = { name: string; percent: number; basis?: string | null }

export type AssessmentRow = {
  name: string
  label: string
  // percent is null when the draft does not state it.
  percent: number | null
  basis?: string
  text: string
}

// The three assessments an agent states for a proposal, in the order they show.
export const ASSESSMENT_NAMES = ['confidence', 'recommend', 'risk'] as const

const cap = (s: string) => (s ? s[0].toUpperCase() + s.slice(1) : s)

// assessmentRows gives one row per standard assessment (stated or "not stated"), then any other
// the agent stated. notStated lists names the server says are missing; a standard name that is
// not stated either way shows as not stated too.
export function assessmentRows(
  assessments: readonly AssessmentIn[],
  notStated: readonly string[] = [],
): AssessmentRow[] {
  const by = new Map(assessments.map((a) => [a.name.toLowerCase(), a]))
  const names = [
    ...ASSESSMENT_NAMES,
    ...assessments.map((a) => a.name.toLowerCase()).filter((n) => !(ASSESSMENT_NAMES as readonly string[]).includes(n)),
    ...notStated.map((n) => n.toLowerCase()).filter((n) => !by.has(n) && !(ASSESSMENT_NAMES as readonly string[]).includes(n)),
  ]
  return [...new Set(names)].map((name) => {
    const a = by.get(name)
    const label = cap(name)
    if (!a) return { name, label, percent: null, text: `${label}: not stated` }
    const percent = Math.max(0, Math.min(100, Math.round(a.percent)))
    return {
      name,
      label,
      percent,
      basis: a.basis?.trim() || undefined,
      text: `${label}: ${percent}%`,
    }
  })
}

export type DraftLike = { id: string; state: string; approval: string; needsYou?: boolean }
export type SetLike<D extends DraftLike = DraftLike> = { root: string; drafts: readonly D[] }

// selectedDraft is the draft shown in a set: the chosen one if it is in the set, else the first
// that needs the owner, else the first.
export function selectedDraft<D extends DraftLike>(
  set: SetLike<D>,
  chosen?: string,
): D | undefined {
  return (
    set.drafts.find((d) => d.id === chosen) ??
    set.drafts.find((d) => d.needsYou) ??
    set.drafts[0]
  )
}

// canApprove: a draft that is still a draft (not yet ready, sent, or dropped).
export const canApprove = (d: Pick<DraftLike, 'state'>) => d.state === 'draft'

export type BatchPair = { id: string; expectHash: string }

// batchPairs builds what "Approve all shown" sends: one {id, expectHash} per set, the draft that
// set shows, and only if it can be approved and its hash is known. A set not rendered, or another
// variation of a shown set, never appears. skipped counts shown drafts left out for a missing hash.
export function batchPairs<D extends DraftLike>(
  sets: readonly SetLike<D>[],
  chosen: Readonly<Record<string, string>>,
  hashes: Readonly<Record<string, string | undefined>>,
): { pairs: BatchPair[]; skipped: number } {
  const pairs: BatchPair[] = []
  let skipped = 0
  for (const s of sets) {
    const d = selectedDraft(s, chosen[s.root])
    if (!d || !canApprove(d)) continue
    const h = hashes[d.id]
    if (!h) skipped++
    else pairs.push({ id: d.id, expectHash: h })
  }
  return { pairs, skipped }
}

// A refusal for a draft that changed after the owner looked.
export const CHANGED_WORDS = 'changed since you looked — reload'

export function isChangedRefusal(msg: string): boolean {
  return msg.includes('stale_hash:')
}

// refusalText is what a refused approval shows: the changed-since words for a mismatch, else the
// server's words.
export function refusalText(msg: string): string {
  return isChangedRefusal(msg) ? CHANGED_WORDS : msg
}

// refusalsFor splits a batch refusal over the items it names: the ids of the batch that the
// message mentions get its text. A message that names none is for the whole batch (in "all").
export function refusalsFor(
  ids: readonly string[],
  msg: string,
): { byId: Record<string, string>; all?: string } {
  const byId: Record<string, string> = {}
  for (const id of ids) {
    const esc = id.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
    if (new RegExp(`(^|[^\\w-])${esc}($|[^\\w-])`).test(msg)) byId[id] = refusalText(msg)
  }
  return Object.keys(byId).length ? { byId } : { byId, all: refusalText(msg) }
}

// orderGroups puts groups that need the owner first, and keeps the server's order otherwise.
export function orderGroups<G extends { needsYou: boolean }>(groups: readonly G[]): G[] {
  return [...groups.keys()]
    .sort((a, b) => Number(groups[b].needsYou) - Number(groups[a].needsYou) || a - b)
    .map((i) => groups[i])
}

export function variationLabel(i: number, n: number): string {
  return n > 1 ? `Variation ${i + 1} of ${n}` : 'Draft'
}
