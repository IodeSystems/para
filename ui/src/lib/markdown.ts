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
      line
        .split(/(`[^`]*`)/)
        .map((part, i) => (i % 2 === 1 ? part : rewrite(part)))
        .join(''),
    )
  }
  return out.join('\n')
}

function rewrite(s: string): string {
  return s.replace(/\[\[([^\]\n]+)\]\]/g, (_all, inner: string) => {
    const { id, label } = wikilinkTarget(inner)
    if (!id) return _all
    // Brackets in the label would end the link text early.
    return `[${label.replace(/[[\]]/g, '')}](${docPath(id)})`
  })
}

// isInternalHref says whether a rendered link stays inside the app.
export function isInternalHref(href: string | undefined): boolean {
  return !!href && href.startsWith('/') && !href.startsWith('//')
}

// mermaidLabel makes a node label safe inside a quoted mermaid string.
export function mermaidLabel(s: string): string {
  return s
    .replace(/["\\]/g, "'")
    .replace(/[\r\n]+/g, ' ')
    .replace(/[<>&]/g, ' ')
    .slice(0, 60)
}

export type GraphNode = { id: string; title: string; type: string; status: string; state: string }
export type GraphEdge = { from: string; to: string }

// graphToMermaid draws dependencies, top down. Graph edges say `from` depends on `to`, so the
// arrow points from `to` (what must finish) to `from` (what waits on it). State is a class, so the diagram needs no per-node styling code. Node ids are
// numbered, so no document id can break the syntax.
export function graphToMermaid(nodes: GraphNode[], edges: GraphEdge[]): string {
  const idx = new Map(nodes.map((n, i) => [n.id, `n${i}`]))
  const lines = ['flowchart TD']
  for (const n of nodes) {
    lines.push(
      `  ${idx.get(n.id)}["${mermaidLabel(n.title || n.id)}"]:::${stateClass(n.state, n.status)}`,
    )
  }
  for (const e of edges) {
    const a = idx.get(e.from)
    const b = idx.get(e.to)
    if (a && b) lines.push(`  ${b} --> ${a}`)
  }
  lines.push('  classDef done fill:#2e7d32,stroke:#1b5e20,color:#fff')
  lines.push('  classDef ready fill:#1565c0,stroke:#0d47a1,color:#fff')
  lines.push('  classDef blocked fill:#c62828,stroke:#8e0000,color:#fff')
  lines.push('  classDef waiting fill:#ef6c00,stroke:#bf360c,color:#fff')
  lines.push('  classDef other fill:#546e7a,stroke:#37474f,color:#fff')
  return lines.join('\n')
}

function stateClass(state: string, status: string): string {
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
