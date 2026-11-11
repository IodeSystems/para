import { ClientError } from 'graphql-request'
import { describe, expect, it, vi } from 'vitest'

// gqlClient anchors its endpoint to the page origin; the node test environment has no window.
vi.hoisted(() => {
  Object.assign(globalThis, {
    window: { location: { origin: 'http://acme.localhost:7700' }, dispatchEvent: () => true },
  })
})
const { errorText, forbiddenText, missingScope } = await import('@/client/gqlClient')

const gqlErr = (message: string) =>
  new ClientError({ status: 200, errors: [{ message }] } as never, { query: '' })
const httpErr = (status: number, detail: string) =>
  new ClientError({ status, detail } as never, { query: '' })

describe('scope refusals', () => {
  it('names the missing write scope from a GraphQL error', () => {
    const e = gqlErr('key lacks scope vault:write')
    expect(missingScope(e)).toBe('vault:write')
    expect(errorText(e)).toBe(
      'This sign-in cannot write (needs vault:write). Run para login again.',
    )
  })
  it('names the missing scope from a 403 body', () => {
    const e = httpErr(403, 'key lacks scope review:decide')
    expect(errorText(e)).toBe('This sign-in lacks review:decide. Run para login again.')
  })
  it('names read, and falls back when the server names no scope', () => {
    expect(forbiddenText('vault:read')).toMatch(/cannot read this \(needs vault:read\)/)
    expect(errorText(httpErr(403, 'forbidden'))).toBe(
      'This sign-in may not do this. Run para login again.',
    )
  })
  it('leaves other errors alone', () => {
    expect(errorText(gqlErr('title is required'))).toBe('title is required')
  })
})
