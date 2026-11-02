// Overlays are URL-routed (WEB-10): the open state is a search param on the current route, so
// the browser's Back button closes them. Open pushes a history entry; close drops the params
// in place (replace) so Back never re-opens a closed one.

export type OverlaySearch = {
  capture?: true
  help?: true
  menu?: true // the screen list on a narrow window
  seed?: string // text to start a capture with
  request?: true
  triage?: string // inbox item id
  reply?: string // rfx id
  decide?: string // proposal dialog: `<deny|rework|sendback|hold>:<item id>`
  note?: string // proposal item id being annotated
  quote?: string // the text the annotation is anchored to
  edit?: string // proposal item id being edited
  answer?: string // question id
  ask?: string // question id to ask someone about
  drop?: string // question id to drop
  done?: string // task id to finish
  toq?: string // task id to turn into a question
}

const flag = (v: unknown): true | undefined =>
  v === true || v === 'true' || v === 1 ? true : undefined
const id = (v: unknown): string | undefined => (typeof v === 'string' && v !== '' ? v : undefined)

export function validateOverlays(s: Record<string, unknown>): OverlaySearch {
  return {
    capture: flag(s.capture),
    help: flag(s.help),
    menu: flag(s.menu),
    seed: id(s.seed),
    request: flag(s.request),
    triage: id(s.triage),
    reply: id(s.reply),
    decide: id(s.decide),
    note: id(s.note),
    quote: id(s.quote),
    edit: id(s.edit),
    answer: id(s.answer),
    ask: id(s.ask),
    drop: id(s.drop),
    done: id(s.done),
    toq: id(s.toq),
  }
}

export const NO_OVERLAYS: OverlaySearch = {
  capture: undefined,
  help: undefined,
  menu: undefined,
  seed: undefined,
  request: undefined,
  triage: undefined,
  reply: undefined,
  decide: undefined,
  note: undefined,
  quote: undefined,
  edit: undefined,
  answer: undefined,
  ask: undefined,
  drop: undefined,
  done: undefined,
  toq: undefined,
}

export function anyOverlay(s: OverlaySearch): boolean {
  return Object.values(s).some((v) => v !== undefined)
}

// parseDecide splits a `decide` param into its action and item id.
export function parseDecide(v: string): { action: string; item: string } | null {
  const i = v.indexOf(':')
  if (i <= 0 || i === v.length - 1) return null
  return { action: v.slice(0, i), item: v.slice(i + 1) }
}
