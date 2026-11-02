import {
  createSession,
  deleteSession,
  getSession,
  takeTokenFromUrl,
  type SessionView,
} from '@/client/session'

// The page's session. A token in the URL fragment is read once, at module load, and removed from
// the address bar at once; the exchange for a cookie happens on the first ensureSession().
let fragmentToken: string | null = takeTokenFromUrl()
let current: Promise<SessionView | null> | null = null

// lastSignInError is why the fragment token was refused, for the sign-in page.
export let lastSignInError = ''

export function ensureSession(): Promise<SessionView | null> {
  current ??= (async () => {
    const tok = fragmentToken
    fragmentToken = null
    if (tok) {
      try {
        return await createSession(tok)
      } catch (e) {
        lastSignInError = e instanceof Error ? e.message : 'sign-in failed'
        // A refused token leaves any cookie session as it was.
      }
    }
    return getSession()
  })()
  return current
}

export async function signIn(token: string): Promise<SessionView> {
  const s = await createSession(token)
  signingOut = false
  lastSignInError = ''
  current = Promise.resolve(s)
  return s
}

// signingOut is true from sign-out on: a call in flight then gets a 401 that means nothing.
export let signingOut = false

export async function signOut(): Promise<void> {
  signingOut = true
  try {
    await deleteSession()
  } finally {
    current = Promise.resolve(null)
  }
}

// dropSession forgets the cached session after a 401, so the next guard asks the server again.
export function dropSession(): void {
  current = null
}
