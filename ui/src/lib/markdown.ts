// Pure helpers for rendering para-md in the browser.

// docPath is the in-app route of a document id.
export function docPath(id: string): string {
  return `/doc/${encodeURIComponent(id)}`
}

// wikilinkTarget turns the inside of [[...]] into a document id and a label. `[[id|label]]` and
// `[[id#anchor]]` are Obsidian forms; the id is the file name without its folder or extension.
export function wikilinkTarget(inner: string): { id: string; label: string } {
  const [target = '', alias] = inner.split('|', 2)
  const id = (target.split('#', 1)[0] ?? '').trim().replace(/^.*\//, '').replace(/\.md$/, '')
  const label = (alias ?? target).trim() || id
  return { id, label }
}

// linkWikilinks rewrites [[target|label]] into a Markdown link to the in-app route, so the
// renderer routes it with the app's own router. Code fences and inline code are left alone.
export function linkWikilinks(md: string): string {
  const out: string[] = []
  let fence: string | null = null
  for (const line of md.split('\n')) {
    const m = /^\s{0,3}(`{3,}|~{3,})/.exec(line)
    if (m?.[1]) {
      const mark = m[1]
      if (fence === null) fence = mark
      else if (mark.startsWith(fence[0] ?? '') && mark.length >= fence.length) fence = null
      out.push(line)
      continue
    }
    if (fence !== null) {
      out.push(line)
      continue
    }
    // Split on inline code spans so a [[x]] inside backticks stays literal.
    out.push(
      tidyLine(line)
        .split(/(`[^`]*`)/)
        .map((part, i) => (i % 2 === 1 ? part : rewrite(part)))
        .join(''),
    )
  }
  return out.join('\n')
}

// RULE_LINE is a policy rule as para writes it: "R4 policy-logging: text ^R4", maybe in a list.
const RULE_LINE = /^(\s*(?:[-*] )?)R(\d+) [a-z0-9.-]+: (.*) \^R\d+\s*$/
// BLOCK_ID is a block id left at the end of a line: "text ^abc-1".
const BLOCK_ID = /\s\^[A-Za-z0-9-]+\s*$/

// tidyLine takes para's bookkeeping out of a line a person reads: a rule shows as "R4 text", and
// a trailing block id is dropped.
export function tidyLine(line: string): string {
  const r = RULE_LINE.exec(line)
  if (r) return `${r[1] ?? ''}**R${r[2] ?? ''}** ${r[3] ?? ''}`
  return line.replace(BLOCK_ID, '')
}

// WIKI_TITLE marks a link whose label is only an id: the app looks the title up.
export const WIKI_TITLE = 'wiki'

function rewrite(s: string): string {
  return s.replace(/\[\[([^\]\n]+)\]\]/g, (_all, inner: string) => {
    const { id, label } = wikilinkTarget(inner)
    if (!id) return _all
    const aliased = inner.includes('|')
    // Brackets in the label would end the link text early.
    return `[${label.replace(/[[\]]/g, '')}](${docPath(id)}${aliased ? '' : ` "${WIKI_TITLE}"`})`
  })
}

// isInternalHref says whether a rendered link stays inside the app.
export function isInternalHref(href: string | undefined): boolean {
  return !!href && href.startsWith('/') && !href.startsWith('//')
}

// mermaidLabel makes a node label safe inside a quoted mermaid string.
export function mermaidLabel(s: string, max = 160): string {
  return s
    .replace(/["\\]/g, "'")
    .replace(/[\r\n]+/g, ' ')
    .replace(/[<>&]/g, ' ')
    .trim()
    .slice(0, max)
}

// wrapLabel breaks a label into lines of about `width` characters, with mermaid's <br/>.
export function wrapLabel(s: string, width = 26): string {
  const lines: string[] = []
  let line = ''
  for (const w of s.split(/\s+/).filter(Boolean)) {
    if (line && line.length + 1 + w.length > width) {
      lines.push(line)
      line = w
    } else {
      line = line ? `${line} ${w}` : w
    }
  }
  if (line) lines.push(line)
  return lines.join('<br/>')
}

export type GraphNode = {
  id: string
  title: string
  type: string
  status: string
  state: string
  kind?: string | null
}
export type GraphEdge = { from: string; to: string }

// Calm fills with dark text: they read on a light or a dark page.
export const GRAPH_CLASSES = {
  done: { label: 'Done', fill: '#d6ebd8', stroke: '#4c8c55', color: '#16301a', tone: 'good' },
  waiting: {
    label: 'Waiting on someone',
    fill: '#d8e5f6',
    stroke: '#4a76b0',
    color: '#10243f',
    tone: 'wait',
  },
  blocked: { label: 'Blocked', fill: '#f4d8d4', stroke: '#b5483f', color: '#3d1410', tone: 'bad' },
  ready: {
    label: 'Ready to start',
    fill: '#eceff1',
    stroke: '#546e7a',
    color: '#1f2328',
    tone: 'quiet',
  },
  other: { label: 'Other', fill: '#f6f6f6', stroke: '#9a9a9a', color: '#2b2b2b', tone: 'quiet' },
} as const

export type GraphClass = keyof typeof GRAPH_CLASSES

// nodeShape: a task is a box, a question a hexagon, a request to someone a pill.
function nodeText(n: GraphNode, label: string): string {
  if (n.type === 'question') return `{{"${label}"}}`
  if (n.type === 'rfx') return `(["${label}"])`
  return `["${label}"]`
}

// graphToMermaid draws dependencies, top down. Graph edges say `from` depends on `to`, so the
// arrow points from `to` (what must finish) to `from` (what waits on it). State is a class, so the
// diagram needs no per-node styling code. Node ids are numbered, so no document id can break the
// syntax. Questions also get the `question` class: a dashed border.
export function graphToMermaid(nodes: GraphNode[], edges: GraphEdge[]): string {
  const idx = new Map(nodes.map((n, i) => [n.id, `n${i}`]))
  const lines = ['flowchart TD']
  const questions: string[] = []
  for (const n of nodes) {
    const label = wrapLabel(mermaidLabel(n.title || n.id))
    lines.push(`  ${idx.get(n.id)}${nodeText(n, label)}:::${graphClass(n.state, n.status)}`)
    if (n.type === 'question') questions.push(idx.get(n.id) ?? '')
  }
  for (const e of edges) {
    const a = idx.get(e.from)
    const b = idx.get(e.to)
    if (a && b) lines.push(`  ${b} --> ${a}`)
  }
  for (const [name, c] of Object.entries(GRAPH_CLASSES))
    lines.push(`  classDef ${name} fill:${c.fill},stroke:${c.stroke},color:${c.color}`)
  lines.push('  classDef question stroke-dasharray:6 4,stroke-width:2px')
  if (questions.length > 0) lines.push(`  class ${questions.join(',')} question`)
  return lines.join('\n')
}

export function graphClass(state: string, status: string): GraphClass {
  const v = `${state} ${status}`.toLowerCase()
  if (/\b(done|closed|answered|replied)\b/.test(v)) return 'done'
  if (/\bblocked\b/.test(v)) return 'blocked'
  if (/\b(waiting|sent|stalled)\b/.test(v)) return 'waiting'
  if (/\b(ready|open|todo|in_progress|started)\b/.test(v)) return 'ready'
  return 'other'
}

// withoutSection drops a `## <name>` section, up to the next `##` heading or the end. The RFX
// screen shows the draft in its own panel, so the request text omits it.
export function withoutSection(md: string, name: string): string {
  const out: string[] = []
  let skipping = false
  for (const line of md.split('\n')) {
    const h = /^##\s+(.*?)\s*$/.exec(line)
    if (h) skipping = (h[1] ?? '').toLowerCase() === name.toLowerCase()
    if (!skipping) out.push(line)
  }
  return out.join('\n').trim()
}
