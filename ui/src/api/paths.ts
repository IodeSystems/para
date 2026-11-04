import { useQuery } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'

const PathsDoc = graphql(/* GraphQL */ `
  query RfxPaths($id: String!) {
    para {
      v1 {
        rfxPaths(id: $id) {
          id
          title
          status
          stalled
          stalledWhy
          stalledDays
          log {
            to
            toName
            medium
            value
            state
            added
            sent
            reason
            why
            about
            contacts
            last
          }
          moves {
            kind
            label
            to
            toName
            medium
            value
            why
            contacts
            last
            hold
            command
          }
        }
      }
    }
  }
`)

export function useRfxPaths(id: string) {
  return useQuery({
    queryKey: ['rfx', id, 'paths'],
    queryFn: async () =>
      need((await gqlClient.request(PathsDoc, { id })).para.v1.rfxPaths, 'paths'),
  })
}

export const REASONS: { code: string; label: string }[] = [
  { code: 'wrong_owner', label: 'Wrong owner' },
  { code: 'no_owner', label: 'No owner' },
  { code: 'policy_conflict', label: 'Policy conflict' },
  { code: 'policy_unclear', label: 'Policy unclear' },
  { code: 'no_response', label: 'No response' },
  { code: 'capacity', label: 'No capacity' },
  { code: 'needs_approval', label: 'Needs approval' },
  { code: 'tool_gap', label: 'Tool gap' },
  { code: 'other', label: 'Other' },
]

export const reasonLabel = (code: string | null | undefined): string =>
  REASONS.find((r) => r.code === code)?.label ?? ''
