import { REQUEST_TIMEOUT_MS, UNAUTHORIZED_EVENT } from '@/client/gqlClient'

// uploadAttachment sends one file to the item's attachments: a multipart POST, the one network path
// besides gqlClient and session.ts (a file does not fit a GraphQL body). The HttpOnly session
// cookie rides along; the server runs attachment_add, a person op, and answers with what it stored.

export type Uploaded = {
  id: string
  path: string
  embed: string
  sha256: string
  size: number
  mime: string
}

// UPLOAD_TIMEOUT_MS: a large video on a phone link takes longer than a GraphQL call.
const UPLOAD_TIMEOUT_MS = Math.max(REQUEST_TIMEOUT_MS, 5 * 60_000)

export async function uploadAttachment(id: string, file: File): Promise<Uploaded> {
  const form = new FormData()
  form.append('id', id)
  form.append('file', file, file.name || 'file')
  const res = await fetch('/api/v1/attachment/upload', {
    method: 'POST',
    body: form,
    signal: AbortSignal.timeout(UPLOAD_TIMEOUT_MS),
  })
  if (res.status === 401) window.dispatchEvent(new Event(UNAUTHORIZED_EVENT))
  const body: unknown = await res.json().catch(() => null)
  if (!res.ok) {
    const detail =
      body && typeof body === 'object' && 'detail' in body && typeof body.detail === 'string'
        ? body.detail
        : `server answered ${res.status}`
    throw new Error(`${file.name || 'file'}: ${detail}`)
  }
  return body as Uploaded
}
