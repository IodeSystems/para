// Source strings of an agent write, shown as links. The server resolves them (sourceLinks); this
// is the fallback for a write read from an older server, and the rules for how a link opens.

export type RefLink = { text: string; url?: string | null; internal?: boolean | null }

const isWeb = (s: string): boolean => /^https?:\/\//i.test(s)

// dedupe drops a link whose text or address was already listed.
export function dedupe(links: RefLink[]): RefLink[] {
  const seen = new Set<string>()
  const out: RefLink[] = []
  for (const l of links) {
    const key = l.url || l.text
    if (!l.text || seen.has(key) || seen.has(l.text)) continue
    seen.add(key)
    seen.add(l.text)
    out.push(l)
  }
  return out
}

// refLinks gives the links to show: the server's resolved list when it has one, else the stored
// strings with repeats dropped and web addresses linked.
export function refLinks(r: {
  sources?: string[] | null
  sourceLinks?: RefLink[] | null
}): RefLink[] {
  if (r.sourceLinks && r.sourceLinks.length > 0) return dedupe(r.sourceLinks)
  return dedupe((r.sources ?? []).map((s) => (isWeb(s) ? { text: s, url: s } : { text: s })))
}

// refKind says how a link opens: in the app, in a new tab, or not at all. An app link starts with
// one slash and names a path of this app.
export function refKind(l: RefLink): 'app' | 'web' | 'none' {
  if (!l.url) return 'none'
  if (l.internal && l.url.startsWith('/') && !l.url.startsWith('//')) return 'app'
  return isWeb(l.url) ? 'web' : 'none'
}
