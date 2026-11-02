// Overlays are URL-routed (WEB-10): the open state is a search param on the current route, so
// the browser's Back button closes them. Open pushes a history entry; close drops the params
// in place (replace) so Back never re-opens a closed one.

export type OverlaySearch = {
  capture?: true
  help?: true
  request?: true
  triage?: string // inbox item id
  reply?: string // rfx id
}

const flag = (v: unknown): true | undefined =>
  v === true || v === 'true' || v === 1 ? true : undefined
const id = (v: unknown): string | undefined => (typeof v === 'string' && v !== '' ? v : undefined)

export function validateOverlays(s: Record<string, unknown>): OverlaySearch {
  return {
    capture: flag(s.capture),
    help: flag(s.help),
    request: flag(s.request),
    triage: id(s.triage),
    reply: id(s.reply),
  }
}

export const NO_OVERLAYS: OverlaySearch = {
  capture: undefined,
  help: undefined,
  request: undefined,
  triage: undefined,
  reply: undefined,
}

export function anyOverlay(s: OverlaySearch): boolean {
  return !!(s.capture || s.help || s.request || s.triage || s.reply)
}
