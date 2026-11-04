import { ClientError, GraphQLClient } from 'graphql-request'

// The only network path (besides client/session.ts). graphql-request needs an absolute URL, so
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
// key lacks a scope. The viewer only reads, so this means the key may not read this part of the
// vault: it is said calmly, with no retry.
export function forbidden(err: unknown): boolean {
  if (httpStatus(err) === 403) return true
  return (
    err instanceof ClientError &&
    (err.response.errors ?? []).some((e) => /lacks scope|forbidden/i.test(e.message))
  )
}

export const FORBIDDEN_TEXT =
  'This key may not see this. Sign in with a key that has vault:read: para serve login.'

// errorText is a line a person can read for a failed call.
export function errorText(err: unknown): string {
  if (forbidden(err)) return FORBIDDEN_TEXT
  if (err instanceof ClientError) {
    const first = err.response.errors?.[0]?.message
    if (first) return first
    return `server answered ${err.response.status}`
  }
  if (err instanceof DOMException && err.name === 'TimeoutError') return 'no answer from the server'
  if (err instanceof Error) return err.message
  return 'request failed'
}
