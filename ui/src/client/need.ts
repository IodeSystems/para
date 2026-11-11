// The gateway types every field as nullable. A field that is null where the server must answer
// is an error, not an empty screen.
export function need<T>(v: T | null | undefined, what: string): T {
  if (v === null || v === undefined) throw new Error(`the server returned no ${what}`)
  return v
}

// needDetail is the detail of a show answer when it is the type named (a member of
// para_ShowDetail), or an error: the item is not that type.
export function needDetail<D extends { __typename: string }, N extends D['__typename']>(
  show: { detail?: D | null } | null | undefined,
  typename: N,
  what: string,
): Extract<D, { __typename: N }> {
  const d = need(show, what).detail
  if (!d || d.__typename !== typename) throw new Error(`the server returned no ${what}`)
  return d as Extract<D, { __typename: N }>
}
