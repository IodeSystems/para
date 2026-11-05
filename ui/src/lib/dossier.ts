// Helpers for the dossier page: find the para blocks in the markdown, name the sections the way
// para does, and count the owner's answers. The rules here copy internal/model/dossier.go.

// BLOCK_INFO is the info string of a para block: `yaml para-<kind>`, with ` id=<id>` on items.
const BLOCK_INFO =
  /^yaml para-(dossier|question|change|message|action|doc-edit|upstream)(?: id=([a-z][a-z0-9-]{0,39}))?$/

export type BlockRef = { block: string; id: string }

// parseBlockInfo reads the info string of a fenced block. It gives null for other code. The header
// block (para-dossier) has an empty id.
export function parseBlockInfo(info: string): BlockRef | null {
  const m = BLOCK_INFO.exec(info.trim())
  if (!m?.[1]) return null
  return { block: m[1], id: m[2] ?? '' }
}

// FENCE is the opening line of a fenced block, as model.fenceRe reads it.
const FENCE = /^ {0,3}(`{3,}|~{3,})(.*)$/

// blockIds lists the ids of the item blocks in a dossier body, in order. Code inside another
// fenced block is not a block.
export function blockIds(md: string): string[] {
  const out: string[] = []
  let open: string | null = null
  for (const line of md.split('\n')) {
    const m = FENCE.exec(line)
    if (!m?.[1]) continue
    const mark = m[1]
    if (open === null) {
      open = mark
      const ref = parseBlockInfo(m[2] ?? '')
      if (ref?.id) out.push(ref.id)
    } else if (mark[0] === open[0] && mark.length >= open.length && (m[2] ?? '').trim() === '') {
      open = null
    }
  }
  return out
}

// blockInfo joins the language and the meta of a fenced block, as the markdown parser splits
// them: "```yaml para-question id=q1" has language "yaml" and meta "para-question id=q1".
export function blockInfo(className: string | undefined, meta: string | undefined): string {
  const lang = /\blanguage-(\S+)/.exec(className ?? '')?.[1] ?? ''
  return meta ? `${lang} ${meta}` : lang
}

// itemKind is the item kind of a block word: the block word "change" is a para (vault) change.
export function itemKind(block: string): string {
  if (block === 'change') return 'para'
  if (block === 'doc-edit') return 'doc_edit'
  return block
}

// sectionId is the id of a ## heading: lower case, each run of other characters one dash, no dash
// at either end. It matches model.SectionID.
export function sectionId(title: string): string {
  return title
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
}

export type Answer = {
  decision?: string | null
  input?: unknown
  subject?: string | null
  body?: string | null
  // people is who a message goes to, as the owner edited it.
  people?: string[] | null
  new?: string | null
  choices?: string[] | null
  addedOptions?: string[] | null
  editedOptions?: unknown
  ownWords?: string | null
  comments?: CommentAnswer[] | null
  comment?: string | null
  // refs are the vault ids an ask names for the agent to read.
  refs?: string[] | null
  via?: string | null
  stale?: boolean | null
}

// CommentAnswer is what the owner makes of one comment of a tracked document in an upstream
// item: ask, question, commitment, resolved, or dropped.
export type CommentAnswer = {
  id: string
  to: string
  note?: string | null
  due?: string | null
}

// answerEmpty says whether an answer says nothing. It matches ItemAnswer.Empty.
export function answerEmpty(a: Answer | null | undefined): boolean {
  if (!a) return true
  const edited = a.editedOptions && typeof a.editedOptions === 'object'
  return (
    !a.decision &&
    (a.input === undefined || a.input === null) &&
    !a.subject &&
    !a.body &&
    !(a.people?.length ?? 0) &&
    !a.new &&
    !(a.choices?.length ?? 0) &&
    !(a.addedOptions?.length ?? 0) &&
    !(edited && Object.keys(a.editedOptions as object).length > 0) &&
    !(a.ownWords ?? '').trim() &&
    !(a.comments?.length ?? 0) &&
    !(a.comment ?? '').trim()
  )
}

// answerInput is the answer as the draft call takes it: no via or stale (para sets them), and no
// empty keys. An empty answer gives null, which clears it.
export function answerInput(a: Answer | null | undefined): Record<string, unknown> | null {
  if (answerEmpty(a) || !a) return null
  const out: Record<string, unknown> = {}
  const keys = [
    'decision',
    'input',
    'subject',
    'body',
    'people',
    'new',
    'choices',
    'addedOptions',
    'editedOptions',
    'ownWords',
    'comments',
    'comment',
    'refs',
  ] as const
  for (const k of keys) {
    const v = a[k]
    if (v === undefined || v === null || v === '') continue
    if (Array.isArray(v) && v.length === 0) continue
    out[k] = v
  }
  if (a.comments?.length) {
    // Only the keys the draft takes: a saved answer read back may carry more.
    out.comments = a.comments.map((c) => ({
      id: c.id,
      to: c.to,
      ...(c.note ? { note: c.note } : {}),
      ...(c.due ? { due: c.due } : {}),
    }))
  }
  return out
}

// stringMap reads a JSON value as a map of strings, as section comments and edited options are.
export function stringMap(v: unknown): Record<string, string> {
  const out: Record<string, string> = {}
  if (!v || typeof v !== 'object' || Array.isArray(v)) return out
  for (const [k, x] of Object.entries(v as Record<string, unknown>)) {
    if (typeof x === 'string') out[k] = x
  }
  return out
}

// EvidenceCheck says how the owner can check evidence, as para decides it: a web address, a
// vault document, or a private connector item with a sentence on how to find it. Kind none: no
// way to verify.
export type EvidenceCheck =
  | { kind: 'url'; url: string }
  | { kind: 'vault'; id: string }
  | { kind: 'via'; source: string; verify: string }
  | { kind: 'none' }

const WEB_URL = /^https?:\/\/[^\s/]+\S*$/
const VAULT_ID = /^\[?\[?([a-z][a-z0-9]*(?:-[a-z0-9]+)+)\]?\]?$/
const VAULT_PATH = /^[a-z][a-z0-9_-]*\/([^\s:/]+)\.md(?::\d+)?$/

export function evidenceCheck(e: {
  locator: string
  via?: { source: string; ref: string } | null
  verify?: string | null
}): EvidenceCheck {
  const loc = e.locator.trim()
  if (WEB_URL.test(loc)) return { kind: 'url', url: loc }
  const path = VAULT_PATH.exec(loc)
  if (path?.[1]) return { kind: 'vault', id: path[1] }
  const id = VAULT_ID.exec(loc)
  if (id?.[1]) return { kind: 'vault', id: id[1] }
  const verify = e.verify?.trim() ?? ''
  if (e.via?.source.trim() && e.via.ref.trim() && verify) {
    return { kind: 'via', source: e.via.source, verify }
  }
  return { kind: 'none' }
}

// URL_LIKE finds a web address in a value, so a card can show evidence as a link to check.
const URL_LIKE = /\bhttps?:\/\/[^\s<>"')\]]+/g

export type TextPart = { text: string; url?: string }

// linkParts splits text into plain parts and web addresses.
export function linkParts(text: string): TextPart[] {
  const out: TextPart[] = []
  let last = 0
  for (const m of text.matchAll(URL_LIKE)) {
    const at = m.index ?? 0
    // A full stop or comma at the end belongs to the sentence, not the address.
    const url = m[0].replace(/[.,;:]+$/, '')
    if (at > last) out.push({ text: text.slice(last, at) })
    out.push({ text: url, url })
    last = at + url.length
  }
  if (last < text.length) out.push({ text: text.slice(last) })
  return out
}
