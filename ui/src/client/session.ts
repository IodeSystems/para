// The session calls. This file is the only one allowed to call fetch (see .oxlintrc.json): a
// sign-in has no session yet, so it cannot use gqlClient's cookie-or-redirect handling. Every
// other call goes through gqlClient.

export type SessionView = {
  vault: string
  key: string
  surface: string
  admin?: boolean
  scopes?: string[]
}

// canDecide says whether the session may answer and decide review items (scope review:decide).
// A read-only key sees the same pages with no control that writes.
export function canDecide(s: SessionView | null | undefined): boolean {
  return !!s && (s.admin === true || (s.scopes ?? []).includes('review:decide'))
}

// parseTokenFragment reads `#token=<value>` from a location hash. Other fragments give null.
export function parseTokenFragment(hash: string): string | null {
  const m = /^#(?:.*&)?token=([^&]+)/.exec(hash)
  if (!m?.[1]) return null
  try {
    return decodeURIComponent(m[1])
  } catch {
    return null
  }
}

// takeTokenFromUrl returns the token in the fragment and removes it from the address bar and the
// history entry, so it is not kept, shared, or sent as a Referer.
export function takeTokenFromUrl(): string | null {
  const tok = parseTokenFragment(window.location.hash)
  if (tok) window.history.replaceState(null, '', window.location.pathname + window.location.search)
  return tok
}

export class SessionError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

async function detail(res: Response): Promise<string> {
  try {
    const j = (await res.json()) as { detail?: string; title?: string }
    return j.detail ?? j.title ?? res.statusText
  } catch {
    return res.statusText
  }
}

// getSession answers who the cookie is, or null when there is no valid session.
export async function getSession(): Promise<SessionView | null> {
  const res = await fetch('/auth/session', { headers: { Accept: 'application/json' } })
  if (res.status === 401 || res.status === 403) return null
  if (!res.ok) throw new SessionError(res.status, await detail(res))
  return (await res.json()) as SessionView
}

// createSession exchanges a key for the HttpOnly session cookie.
export async function createSession(token: string): Promise<SessionView> {
  const res = await fetch('/auth/session', {
    method: 'POST',
    headers: { Authorization: `Bearer ${token.trim()}`, Accept: 'application/json' },
  })
  if (!res.ok) throw new SessionError(res.status, await detail(res))
  return (await res.json()) as SessionView
}

export async function deleteSession(): Promise<void> {
  const res = await fetch('/auth/session/delete', { method: 'POST' })
  if (!res.ok) throw new SessionError(res.status, await detail(res))
}
