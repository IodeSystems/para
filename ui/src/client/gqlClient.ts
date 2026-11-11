import { ClientError, GraphQLClient } from 'graphql-request'

// The only network path (besides client/session.ts, and client/upload.ts for a file). graphql-request needs an absolute URL, so
// the path is anchored to the page origin. The HttpOnly session cookie rides along on same-origin
// requests; the page never sees it.
const endpoint = () => `${window.location.origin}/api/graphql`

// Bound every call: a black-holed request must reject, not spin for minutes.
export const REQUEST_TIMEOUT_MS = 30_000

export const UNAUTHORIZED_EVENT = 'para:unauthorized'

// httpStatus is the HTTP status of a failed call, or 0 for a network failure.
export function httpStatus(err: unknown): number {
  return err instanceof ClientError ? err.response.status : 0
}

export const gqlClient = new GraphQLClient(endpoint(), {
  requestMiddleware: (request) => ({
    ...request,
    signal: request.signal ?? AbortSignal.timeout(REQUEST_TIMEOUT_MS),
  }),
  responseMiddleware: (response) => {
    // A dead session (401) sends the person to the sign-in page. It only observes: the error still
    // rejects to the caller.
    if (response instanceof ClientError && response.response.status === 401) {
      window.dispatchEvent(new Event(UNAUTHORIZED_EVENT))
    }
  },
})

// forbidden says a call was refused for its key: a 403, or the gateway's 200 whose error says the
// key lacks a scope. It is said calmly, with no retry; errorText names the scope.
export function forbidden(err: unknown): boolean {
  if (httpStatus(err) === 403) return true
  return (
    err instanceof ClientError &&
    (err.response.errors ?? []).some((e) => /lacks scope|forbidden/i.test(e.message))
  )
}

// missingScope is the scope a refusal names ("key lacks scope vault:write"), or ''.
export function missingScope(err: unknown): string {
  const texts: string[] = []
  if (err instanceof ClientError) {
    for (const e of err.response.errors ?? []) texts.push(e.message)
    const detail = (err.response as { detail?: unknown }).detail
    if (typeof detail === 'string') texts.push(detail)
  }
  if (err instanceof Error) texts.push(err.message)
  for (const t of texts) {
    const m = /lacks scope ([a-z]+:[a-z]+|admin)/i.exec(t)
    if (m) return m[1]
  }
  return ''
}

// forbiddenText says what a refusal for the key's scope means, with the scope the server named.
export function forbiddenText(scope: string): string {
  const again = 'Run para login again.'
  switch (scope) {
    case 'vault:write':
      return `This sign-in cannot write (needs vault:write). ${again}`
    case 'vault:read':
      return `This sign-in cannot read this (needs vault:read). ${again}`
    case '':
      return `This sign-in may not do this. ${again}`
    default:
      return `This sign-in lacks ${scope}. ${again}`
  }
}

export const FORBIDDEN_TEXT = forbiddenText('')

// errorText is a line a person can read for a failed call.
export function errorText(err: unknown): string {
  if (forbidden(err)) return forbiddenText(missingScope(err))
  if (err instanceof ClientError) {
    const first = err.response.errors?.[0]?.message
    if (first) return first
    return `server answered ${err.response.status}`
  }
  if (err instanceof DOMException && err.name === 'TimeoutError') return 'no answer from the server'
  if (err instanceof Error) return err.message
  return 'request failed'
}
