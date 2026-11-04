// Overlays are URL-routed (WEB-10): the open state is a search param on the current route, so
// the browser's Back button closes them. Open pushes a history entry; close drops the params
// in place (replace) so Back never re-opens a closed one. The viewer writes nothing, so the only
// overlays are help and the screen list.

export type OverlaySearch = {
  help?: true
  menu?: true // the screen list on a narrow window
}

const flag = (v: unknown): true | undefined =>
  v === true || v === 'true' || v === 1 ? true : undefined

export function validateOverlays(s: Record<string, unknown>): OverlaySearch {
  return { help: flag(s.help), menu: flag(s.menu) }
}

export const NO_OVERLAYS: OverlaySearch = { help: undefined, menu: undefined }
