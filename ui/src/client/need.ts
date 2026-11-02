// The gateway types every field as nullable. A field that is null where the server must answer
// is an error, not an empty screen.
export function need<T>(v: T | null | undefined, what: string): T {
  if (v === null || v === undefined) throw new Error(`the server returned no ${what}`)
  return v
}
