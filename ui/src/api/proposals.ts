import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'
import { approveIo } from '@/api/io'

// The Output page's Proposals tab: io_proposals (groups of variation sets of drafts), a draft's
// revisions and diff, and the owner's writes: approve (one or a batch, bound to the text seen) and
// drop (with a reason). Done runs through useIODone in api/io.

const IoProposalsDoc = graphql(/* GraphQL */ `
  query IoProposals($all: Boolean, $state: para_ioProposalsState) {
    para {
      v1 {
        ioProposals(all: $all, state: $state) {
          count
          needsYou
          groups {
            group
            needsYou
            sets {
              root
              needsYou
              drafts {
                id
                title
                to
                medium
                kind
                subject
                preview
                state
                created
                needsYou
                why
                impact
                assessments {
                  name
                  percent
                  basis
                }
                notStated
                revision
                revisions
                approval
                approvedHash
                held
                heldBy
                links {
                  id
                  title
                  type
                }
                answers {
                  id
                  title
                  type
                }
                request
                thread {
                  key
                  queued
                  claimed
                  done
                }
                lastAttempt {
                  at
                  result
                  error
                  actor
                }
              }
            }
          }
        }
      }
    }
  }
`)

export type ProposalsData = NonNullable<ReturnType<typeof useIoProposals>['data']>
export type ProposalGroup = ProposalsData['groups'][number]
export type ProposalSet = ProposalGroup['sets'][number]
export type ProposalDraft = ProposalSet['drafts'][number]

export function useIoProposals(all: boolean) {
  return useQuery({
    queryKey: ['io', 'proposals', 'list', all],
    queryFn: async () =>
      need(
        (await gqlClient.request(IoProposalsDoc, { all: all || undefined })).para.v1.ioProposals,
        'proposals',
      ),
  })
}

const IoRevisionsDoc = graphql(/* GraphQL */ `
  query IoRevisions($id: String!, $n: Int) {
    para {
      v1 {
        ioRevisions(id: $id, n: $n) {
          id
          current
          n
          text
          revisions {
            n
            at
            by
            hash
          }
        }
      }
    }
  }
`)

export function useIoRevisions(id: string, n: number | undefined, enabled: boolean) {
  return useQuery({
    queryKey: ['io', 'revisions', id, n ?? 0],
    enabled: enabled && id !== '',
    queryFn: async () =>
      need((await gqlClient.request(IoRevisionsDoc, { id, n })).para.v1.ioRevisions, 'revisions'),
  })
}

const IoDiffDoc = graphql(/* GraphQL */ `
  query IoDiff($id: String!, $from: Int, $to: Int) {
    para {
      v1 {
        ioDiff(id: $id, from: $from, to: $to) {
          from
          to
          same
          added
          removed
          parts {
            op
            text
            a
            b
          }
        }
      }
    }
  }
`)

export function useIoDiff(id: string, from: number, to: number, enabled: boolean) {
  return useQuery({
    queryKey: ['io', 'diff', id, from, to],
    enabled: enabled && id !== '' && from > 0 && to > from,
    queryFn: async () =>
      need((await gqlClient.request(IoDiffDoc, { id, from, to })).para.v1.ioDiff, 'diff'),
  })
}

export type ApprovePair = { id: string; expectHash: string }

// useApproveProposals approves one draft or several (io_approve): each carries the draft_hash the
// owner saw, and the server refuses a draft that changed since. A batch is all or none.
export function useApproveProposals() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (pairs: ApprovePair[]) => {
      const one = pairs.length === 1 ? pairs[0] : undefined
      return approveIo(one ? { id: one.id, expectHash: one.expectHash } : { items: pairs })
    },
    onSettled: () => qc.invalidateQueries(),
  })
}

const IODropDoc = graphql(/* GraphQL */ `
  mutation IODrop($body: para_IODropInputInput!) {
    para {
      v1 {
        ioDrop(body: $body) {
          id
          state
        }
      }
    }
  }
`)

// useDropProposal drops a draft with the owner's reason (io_drop).
export function useDropProposal() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (b: { id: string; reason: string }) =>
      need((await gqlClient.request(IODropDoc, { body: b })).para.v1.ioDrop, 'drop'),
    onSettled: () => qc.invalidateQueries(),
  })
}
