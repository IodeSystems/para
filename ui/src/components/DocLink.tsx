import type { ReactNode } from 'react'
import { TextLink } from '@/components/links'

// A link to a document in the app.
export function DocLink({ id, children }: { id: string; children?: ReactNode }) {
  return (
    <TextLink to="/docs/$id" params={{ id }} underline="hover">
      {children ?? id}
    </TextLink>
  )
}

// A link to the RFX screen of a request.
export function RfxLink({ id, children }: { id: string; children?: ReactNode }) {
  return (
    <TextLink to="/rfx/$id" params={{ id }} underline="hover">
      {children ?? id}
    </TextLink>
  )
}
