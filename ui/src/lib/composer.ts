// Pure rules of the composer: the box at the foot of an item's page (and on the stack) where the
// owner writes what happened, attaches files, and sends it to an agent or saves it as notes.

// ACCEPT is what the + button offers: the types the server stores. On a phone it also offers the
// camera and the recorder.
export const ACCEPT =
  'image/*,audio/*,video/*,application/pdf,text/plain,text/markdown,.md,.txt,.pdf,.heic,.m4a'

// composerKey says what a key does in the text box: Enter sends, Shift-Enter is a new line, and
// Enter while an input method composes a word does nothing to the form.
export function composerKey(e: {
  key: string
  shiftKey: boolean
  isComposing?: boolean
}): 'send' | null {
  if (e.key !== 'Enter' || e.shiftKey || e.isComposing) return null
  return 'send'
}

// canSend: a send needs words or a file, and nothing in flight.
export function canSend(text: string, files: number, busy: boolean): boolean {
  return !busy && (text.trim() !== '' || files > 0)
}

// flatHeadings makes # and ## lines bold text: a heading would end the Notes section.
export function flatHeadings(text: string): string {
  return text
    .replace(/\r\n/g, '\n')
    .split('\n')
    .map((l) => {
      const t = l.trimStart()
      return t.startsWith('#') ? `**${t.replace(/^#+/, '').trim()}**` : l
    })
    .join('\n')
}

// notesWithEntry adds a dated entry to the end of the notes: the owner's words and an embed per
// attachment. The notes already there stay as they are.
export function notesWithEntry(old: string, date: string, text: string, embeds: string[]): string {
  const parts = [`**${date}**`]
  const words = flatHeadings(text.trim())
  if (words) parts.push(words)
  if (embeds.length) parts.push(embeds.join('\n'))
  const entry = parts.join('\n\n')
  const before = old.trim()
  return before ? `${before}\n\n${entry}` : entry
}

export type ItemMeta = {
  type: string
  status?: string | null
  start?: string | null
  scheduled?: string | null
  kind?: string | null
  optional?: boolean | null
  attend?: string | null
}

const OPEN_TASK = ['todo', 'doing', 'blocked']

// attends: the owner goes to the meeting. A meeting they skip (attend no) does not count, and an
// optional one counts only when they said yes. A required one counts unless they declined it.
export function attends(m: { optional?: boolean | null; attend?: string | null }): boolean {
  if (m.attend === 'no') return false
  return !m.optional || m.attend === 'yes'
}

// isMeeting: a meeting, or a task of kind meeting or call.
const isMeeting = (m: ItemMeta) =>
  m.type === 'meeting' || (m.type === 'task' && (m.kind === 'meeting' || m.kind === 'call'))

// composerPrompt is what the item asks of the owner, or null: a meeting or call that has passed
// with no record of what came of it. A meeting the owner did not go to asks nothing.
export function composerPrompt(m: ItemMeta, now: Date): string | null {
  if (!isMeeting(m) || !attends(m)) return null
  const when = m.type === 'meeting' ? m.start : m.scheduled
  const open = m.type === 'meeting' ? m.status === 'scheduled' : OPEN_TASK.includes(m.status ?? '')
  if (!open || !when) return null
  const t = Date.parse(when)
  if (Number.isNaN(t) || t > now.getTime()) return null
  return 'This has passed. Say what came of it: what was said, what was decided, who does what next.'
}

// closable says Save as notes with Mark done can close the item itself (item_close): an open task,
// a meeting task, or a scheduled meeting. Anything else only an agent can close (a question needs
// an answer, an ask a reply).
export function closable(m: ItemMeta): boolean {
  if (m.type === 'task') return OPEN_TASK.includes(m.status ?? '')
  return m.type === 'meeting' && (m.status === 'scheduled' || m.status === 'requested')
}

// Attachments are served by the server at one route; an embed names the vault path.
const ATTACHMENT = /^attachments\/[a-z0-9][a-z0-9.-]*\/[0-9]{8}-[0-9]{6}-[A-Za-z0-9._-]+$/

export const isAttachmentPath = (p: string | undefined): p is string =>
  !!p && ATTACHMENT.test(p) && !p.includes('..')

export const attachmentSrc = (p: string): string =>
  `/api/v1/attachment/file?path=${encodeURIComponent(p)}`

const KINDS: Record<string, 'image' | 'audio' | 'video' | 'text'> = {
  png: 'image',
  jpg: 'image',
  jpeg: 'image',
  gif: 'image',
  webp: 'image',
  avif: 'image',
  heic: 'image',
  heif: 'image',
  mp3: 'audio',
  m4a: 'audio',
  aac: 'audio',
  wav: 'audio',
  ogg: 'audio',
  oga: 'audio',
  opus: 'audio',
  flac: 'audio',
  weba: 'audio',
  mp4: 'video',
  m4v: 'video',
  mov: 'video',
  webm: 'video',
  ogv: 'video',
  txt: 'text',
  md: 'text',
}

// attachmentKind says how a page shows a file: inline image, a player, or a link.
export function attachmentKind(p: string): 'image' | 'audio' | 'video' | 'text' | 'other' {
  const ext = p.slice(p.lastIndexOf('.') + 1).toLowerCase()
  return KINDS[ext] ?? 'other'
}

// fileLabel is a short name for a picked file: a phone names a photo image.jpg, a recording blob.
export function fileLabel(f: { name: string; size: number }): string {
  const kb = f.size / 1024
  const size = kb < 1024 ? `${Math.max(1, Math.round(kb))} KB` : `${(kb / 1024).toFixed(1)} MB`
  return `${f.name || 'file'} (${size})`
}
