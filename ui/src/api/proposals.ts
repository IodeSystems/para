import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'

// Proposals (dossiers) and the review stack. A session with review:decide drafts answers, submits
// them, and approves or rejects items across dossiers. Every other change goes through the chat.

const ProposalListDoc = graphql(/* GraphQL */ `
  query ProposalList {
    para {
      v1 {
        proposalList {
          items {
            id
            title
            status
            revision
            itemCount
            pending
            proposedBy
            created
            sourceName
            error
          }
        }
      }
    }
  }
`)

const ProposalShowDoc = graphql(/* GraphQL */ `
  query ProposalShow($id: String!) {
    para {
      v1 {
        proposalShow(id: $id) {
          rev
          tldr
          context
          untrusted
          dossier
          markdown
          sections {
            id
            title
          }
          response {
            state
            via
            submittedAt
            overall
            sections
          }
          replies {
            section
            turn {
              id
              kind
              text
              by
            }
          }
          reviewCommand
          reviewUrl
          supersededBy
          proposal {
            id
            title
            status
            revision
            itemCount
            pending
            proposedBy
            created
            sourceDoc
            sourcePath
            sourceName
            error
          }
          source {
            id
            title
            body
            error
          }
          items {
            id
            kind
            summary
            stack
            section
            rev
            multi
            options
            recommended
            optionDetails {
              name
              pros {
                text
                source
              }
              cons {
                text
                source
              }
              cost {
                text
                source
              }
              benefit {
                text
                source
              }
              confidence
              confidenceBasis
              recommendation
              examples {
                kind
                text
                source
              }
            }
            range
            old
            new
            from
            to
            comments
            claimedBy
            leaseUntil
            answer {
              decision
              input
              subject
              body
              new
              choices
              addedOptions
              editedOptions
              ownWords
              comments {
                id
                to
                note
                due
              }
              comment
              via
              stale
            }
            change
            resultName
            op
            input
            subtype
            people
            channel
            subject
            io
            target
            what
            previous
            how
            impact
            title
            body
            diff
            fields
            pros {
              text
              source
            }
            cons {
              text
              source
            }
            cost {
              text
              source
            }
            benefit {
              text
              source
            }
            confidence
            confidenceBasis
            recommendation
            reversible
            reversibleHow
            after
            bundle
            choice
            group
            waits
            filled
            execution {
              result
              url
              locator
              by
              at
              selfReported
              receipt
            }
            state
            edited
            reason
            result
            heldBy
            risk
            for
            notDuplicate
            decided {
              decision
              by
              at
              ownerSaid
              selfReported
              surface
            }
            hash
            approvedHash
            actions
            thread {
              id
              kind
              quote
              text
              by
              revision
              state
              answer
            }
            preview {
              error
              changes {
                doc
                title
                type
                new
                lines
              }
              downstream {
                doc
                title
                effect
              }
            }
            evidence {
              locator
              quote
            }
          }
          history {
            id
            revision
            status
            created
          }
        }
      }
    }
  }
`)

const ProposalDiffDoc = graphql(/* GraphQL */ `
  query ProposalDiff($id: String!) {
    para {
      v1 {
        proposalDiff(id: $id) {
          id
          previous
          revision
          items {
            item
            change
            fields {
              field
              lines
            }
          }
        }
      }
    }
  }
`)

const DraftDoc = graphql(/* GraphQL */ `
  mutation ProposalDraft($body: para_ProposalDraftInputInput!) {
    para {
      v1 {
        proposalDraft(body: $body) {
          id
          rev
          revision
          drafted
          total
        }
      }
    }
  }
`)

const SubmitDoc = graphql(/* GraphQL */ `
  mutation ProposalSubmit($body: para_ProposalSubmitInputInput!) {
    para {
      v1 {
        proposalSubmit(body: $body) {
          id
          status
          failed
          items {
            item
            state
            reason
          }
        }
      }
    }
  }
`)

const StackDoc = graphql(/* GraphQL */ `
  query ProposalStack(
    $state: String
    $kind: String
    $group: String
    $proposal: String
    $actor: String
    $cursor: String
    $limit: Int
  ) {
    para {
      v1 {
        proposalStack(
          state: $state
          kind: $kind
          group: $group
          proposal: $proposal
          actor: $actor
          cursor: $cursor
          limit: $limit
        ) {
          counts {
            state
            count
          }
          groups {
            proposal
            group
            done
            total
            proposed
          }
          rows {
            proposal
            title
            item
            kind
            state
            stack
            group
            bundle
            actor
            summary
            drafted
            claimedBy
            url
            receipt
            result
            reason
          }
          total
          nextCursor
        }
      }
    }
  }
`)

const StackDecideDoc = graphql(/* GraphQL */ `
  mutation StackDecide($body: para_StackDecideInputInput!) {
    para {
      v1 {
        stackDecide(body: $body) {
          proposals {
            id
          }
        }
      }
    }
  }
`)

const UndoneDoc = graphql(/* GraphQL */ `
  query ProposalUndone {
    para {
      v1 {
        proposalUndone {
          items {
            proposal
            item
            title
            summary
            target
          }
        }
      }
    }
  }
`)

export function useProposals() {
  return useQuery({
    queryKey: ['proposals'],
    queryFn: async () =>
      need((await gqlClient.request(ProposalListDoc)).para.v1.proposalList, 'proposals').items,
  })
}

// useOpenProposals lists the proposals that wait for the owner.
export function useOpenProposals() {
  const q = useProposals()
  return { ...q, open: (q.data ?? []).filter((p) => p.status === 'open' && !p.error) }
}

export function useProposal(id: string) {
  return useQuery({
    queryKey: ['proposal', id],
    queryFn: async () =>
      need((await gqlClient.request(ProposalShowDoc, { id })).para.v1.proposalShow, 'proposal'),
  })
}

export function useProposalDiff(id: string, enabled: boolean) {
  return useQuery({
    queryKey: ['proposal', id, 'diff'],
    enabled,
    queryFn: async () =>
      need((await gqlClient.request(ProposalDiffDoc, { id })).para.v1.proposalDiff, 'diff'),
  })
}

// useUndone lists outward items that were approved and not reported done.
export function useUndone() {
  return useQuery({
    queryKey: ['proposals', 'undone'],
    queryFn: async () =>
      need((await gqlClient.request(UndoneDoc)).para.v1.proposalUndone, 'undone').items,
  })
}

export type DraftBody = Parameters<typeof draftCall>[0]

function draftCall(body: {
  id: string
  revision: number
  items?: { item: string; answer: Record<string, unknown> | null }[]
  sections?: { section: string; comment: string }[]
  overall?: string
}) {
  return gqlClient.request(DraftDoc, { body })
}

// useProposalDraft saves answers as a draft. The server refuses it when the revision is old.
export function useProposalDraft() {
  return useMutation({
    mutationFn: async (body: DraftBody) =>
      need((await draftCall(body)).para.v1.proposalDraft, 'draft'),
  })
}

// useProposalSubmit turns the drafted answers into decisions.
export function useProposalSubmit() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (body: { id: string; revision: number }) =>
      need((await gqlClient.request(SubmitDoc, { body })).para.v1.proposalSubmit, 'submit'),
    onSettled: () => qc.invalidateQueries(),
  })
}

export type StackFilter = {
  state?: string
  kind?: string
  group?: string
  proposal?: string
  actor?: string
}

const STACK_PAGE = 200

// useStack reads the review stack a page at a time.
export function useStack(f: StackFilter) {
  return useInfiniteQuery({
    queryKey: ['stack', f],
    initialPageParam: '',
    queryFn: async ({ pageParam }) =>
      need(
        (
          await gqlClient.request(StackDoc, {
            ...f,
            cursor: pageParam || undefined,
            limit: STACK_PAGE,
          })
        ).para.v1.proposalStack,
        'stack',
      ),
    getNextPageParam: (last) => last.nextCursor || undefined,
  })
}

export type StackDecideBody = {
  items?: { proposal: string; item: string }[]
  group?: string
  proposal?: string
  decision: 'approve' | 'reject'
  comment?: string
}

// useStackDecide approves or rejects items across dossiers, or one group of one dossier.
export function useStackDecide() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (body: StackDecideBody) =>
      need((await gqlClient.request(StackDecideDoc, { body })).para.v1.stackDecide, 'decide'),
    onSettled: () => qc.invalidateQueries(),
  })
}
