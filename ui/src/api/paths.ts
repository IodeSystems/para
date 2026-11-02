import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'
import type { Para_RfxPathAddInputMedium, Para_RfxPathAddInputReason } from '@/gql/graphql'

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

const PathAddDoc = graphql(/* GraphQL */ `
  mutation RfxPathAdd($body: para_RFXPathAddInputInput!) {
    para {
      v1 {
        rfxPathAdd(body: $body) {
          id
          held
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

export type PathAddInput = {
  id: string
  to: string
  medium: string
  value?: string | null
  reason: string
  why: string
  override?: boolean
}

export function useRfxPathAdd() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (b: PathAddInput) =>
      need(
        (
          await gqlClient.request(PathAddDoc, {
            body: {
              ...b,
              value: b.value || undefined,
              medium: b.medium as Para_RfxPathAddInputMedium,
              reason: b.reason as Para_RfxPathAddInputReason,
            },
          })
        ).para.v1.rfxPathAdd,
        'path',
      ),
    onSuccess: () => void qc.invalidateQueries(),
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
