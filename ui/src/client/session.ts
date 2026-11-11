// The session calls. This file is the only one allowed to call fetch (see .oxlintrc.json): a
// sign-in has no session yet, so it cannot use gqlClient's cookie-or-redirect handling. Every
// other call goes through gqlClient.

export type SessionView = {
  vault: string
  key: string
  surface: string
  admin?: boolean
  scopes?: string[]
  // expires is when the key ends (RFC 3339), or absent for a key with no end.
  expires?: string
  // login marks a `para serve login` key, which renews near its end.
  login?: boolean
}

// RENEW_WINDOW_MS matches the server: a login key renews within 7 days of its end.
export const RENEW_WINDOW_MS = 7 * 24 * 60 * 60 * 1000

// needsRenew says whether the session's login key is within the renew window and not yet ended.
export function needsRenew(s: SessionView | null | undefined, now = Date.now()): boolean {
  if (!s?.login || !s.expires) return false
  const end = Date.parse(s.expires)
  if (Number.isNaN(end)) return false
  return end > now && end - now <= RENEW_WINDOW_MS
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
  if (res.status === 403)
    throw new SessionError(
      403,
      'This key cannot be used from this address. The admin token works from the machine that runs para only; from another device use a login key (para login, or para key create --scope web).',
    )
  if (!res.ok) throw new SessionError(res.status, await detail(res))
  return (await res.json()) as SessionView
}

// renewSession replaces the login key: the server makes a new key with the same scopes, revokes
// the old one, and sets a new session cookie. The new token never reaches the page.
export async function renewSession(): Promise<SessionView> {
  const res = await fetch('/auth/session/renew', {
    method: 'POST',
    headers: { Accept: 'application/json' },
  })
  if (!res.ok) throw new SessionError(res.status, await detail(res))
  return (await res.json()) as SessionView
}

export async function deleteSession(): Promise<void> {
  const res = await fetch('/auth/session/delete', { method: 'POST' })
  if (!res.ok) throw new SessionError(res.status, await detail(res))
}
