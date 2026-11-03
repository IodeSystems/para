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
  decide?: string // proposal dialog: `<deny|hold>:<item id>`
  itemreq?: string // proposal item id being asked for changes, a clarification, or an investigation
  edit?: string // proposal item id being edited
  noteedit?: string // note id whose title and body are edited
  docedit?: string // id of any other document whose title, body, and fields are edited
  answer?: string // question id
  ask?: string // question id to ask someone about
  drop?: string // question id to drop
  done?: string // task id to finish
  toq?: string // task id to turn into a question
  resolve?: string // task id to close with an outcome
  block?: string // task id to block
  assign?: string // task id to assign
  delegate?: string // task id to give to an agent
  update?: string // task id to add an update to
  reject?: string // question id whose proposed answer is rejected
  qedit?: string // question id to edit
  qdeps?: string // question id whose dependencies are edited
  later?: string // comma-separated ids of questions or tasks to put off
  find?: string // question id to send to an agent to find evidence
  cut?: string // question or task id whose dependency is cut
  taskdw?: string // task id whose done-when is set
  goaldw?: string // goal (project) id whose done-when is set
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
    itemreq: id(s.itemreq),
    edit: id(s.edit),
    noteedit: id(s.noteedit),
    docedit: id(s.docedit),
    answer: id(s.answer),
    ask: id(s.ask),
    drop: id(s.drop),
    done: id(s.done),
    toq: id(s.toq),
    resolve: id(s.resolve),
    block: id(s.block),
    assign: id(s.assign),
    delegate: id(s.delegate),
    update: id(s.update),
    reject: id(s.reject),
    qedit: id(s.qedit),
    qdeps: id(s.qdeps),
    later: id(s.later),
    find: id(s.find),
    cut: id(s.cut),
    taskdw: id(s.taskdw),
    goaldw: id(s.goaldw),
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
  itemreq: undefined,
  edit: undefined,
  noteedit: undefined,
  docedit: undefined,
  answer: undefined,
  ask: undefined,
  drop: undefined,
  done: undefined,
  toq: undefined,
  resolve: undefined,
  block: undefined,
  assign: undefined,
  delegate: undefined,
  update: undefined,
  reject: undefined,
  qedit: undefined,
  qdeps: undefined,
  later: undefined,
  find: undefined,
  cut: undefined,
  taskdw: undefined,
  goaldw: undefined,
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
