import { nameOr } from '@/lib/names'

// A sender or recipient of an io item is a link to a person or group ("[[person-x]]") or free
// text such as an address. ioTarget is the linked id, or '' for text.
export function ioTarget(party: string | null | undefined): string {
  const m = /^\[\[([^\]#|]+)(?:[#|][^\]]*)?\]\]$/.exec(party ?? '')
  return m?.[1] ?? ''
}

// ioParty is a party in words: the linked id as a name, or the text as written.
export function ioParty(party: string | null | undefined): string {
  const id = ioTarget(party)
  return id ? nameOr(null, id) : (party ?? '')
}
