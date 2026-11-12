// KPIs on a project (kpi.go). A KPI the owner owns (owner self) that is off target or stale is one
// row of the order, id kpi:<project>/<name slug>; it is not a document. The owner adds a reading on
// the project page or on that Next card (kpi_reading_add); a reading always names its source.

// OWNER_SOURCE is the source of the owner's own reading in the web app: the owner's word, as a
// person cites free text elsewhere. The owner may replace it with a URL or a document id.
export const OWNER_SOURCE = 'owner'

// isKPIRow says whether an order row is a KPI the owner owns, not an item.
export function isKPIRow(r: { id: string; kind?: string | null }): boolean {
  return r.kind === 'kpi' || r.id.startsWith('kpi:')
}

// kpiRowParts splits a KPI row id into its project id and the slug of the KPI's name.
export function kpiRowParts(id: string): { project: string; slug: string } | null {
  const m = /^kpi:([^/]+)\/(.*)$/.exec(id)
  return m ? { project: m[1], slug: m[2] } : null
}

// kpiSlug is the server's slug of a KPI name (ops slug): lowercase, runs of other characters to -.
export function kpiSlug(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
}

export type ReadingInput = { name: string; value: number; source: string; date?: string }

// readingInput checks the reading form: a KPI, a number, a source, and an optional date. It returns
// the input of kpi_reading_add, or what is wrong.
export function readingInput(f: {
  name: string
  value: string
  source: string
  date: string
}): ReadingInput | string {
  const name = f.name.trim()
  const source = f.source.trim().split(/\s+/).join(' ')
  const date = f.date.trim()
  const raw = f.value.trim()
  const value = Number(raw)
  if (!name) return 'Pick a KPI.'
  if (raw === '' || !Number.isFinite(value)) return 'The value is a number.'
  if (!source) return 'Say where you read it: a URL, a document id, or owner.'
  if (date && !/^\d{4}-\d{2}-\d{2}$/.test(date)) return 'The date is YYYY-MM-DD, or empty for today.'
  return date ? { name, value, source, date } : { name, value, source }
}
