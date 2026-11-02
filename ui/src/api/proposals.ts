import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'
import type { Para_ProposalHoldInputKind, Para_ProposalRequestInputIntent } from '@/gql/graphql'

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
            change
            resultName
            op
            input
            subtype
            people
            channel
            subject
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
            hash
            approvedHash
            openNotes
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

export const ProposalReviewFields = graphql(/* GraphQL */ `
  fragment ProposalReviewFields on para_ProposalReviewOutput {
    id
    status
    request
    failed
    items {
      item
      state
      reason
      result
      approvedHash
      heldBy
    }
  }
`)

const ApproveDoc = graphql(/* GraphQL */ `
  mutation ProposalApprove($body: para_ProposalApproveInputInput!) {
    para {
      v1 {
        proposalApprove(body: $body) {
          ...ProposalReviewFields
        }
      }
    }
  }
`)
const DenyDoc = graphql(/* GraphQL */ `
  mutation ProposalDeny($body: para_ProposalReasonInputInput!) {
    para {
      v1 {
        proposalDeny(body: $body) {
          ...ProposalReviewFields
        }
      }
    }
  }
`)
const HoldDoc = graphql(/* GraphQL */ `
  mutation ProposalHold($body: para_ProposalHoldInputInput!) {
    para {
      v1 {
        proposalHold(body: $body) {
          ...ProposalReviewFields
        }
      }
    }
  }
`)
const RequestDoc = graphql(/* GraphQL */ `
  mutation ProposalRequest($body: para_ProposalRequestInputInput!) {
    para {
      v1 {
        proposalRequest(body: $body) {
          ...ProposalReviewFields
        }
      }
    }
  }
`)
const EditDoc = graphql(/* GraphQL */ `
  mutation ProposalEdit($body: para_ProposalEditInputInput!) {
    para {
      v1 {
        proposalEdit(body: $body) {
          ...ProposalReviewFields
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

// useOpenProposals lists the proposals that wait for a decision from the owner.
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

// Every review call changes the proposal, the Review tab's "needs you", and what an op item
// wrote, so all queries refetch.
function useReviewMutation<V>(call: (v: V) => Promise<unknown>) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: call,
    onSuccess: () => void qc.invalidateQueries(),
  })
}

export const useApprove = () =>
  useReviewMutation(async (v: { id: string; items?: string[]; group?: string }) =>
    need((await gqlClient.request(ApproveDoc, { body: v })).para.v1.proposalApprove, 'result'),
  )
export const useDeny = () =>
  useReviewMutation(async (v: { id: string; item: string; reason: string }) =>
    need((await gqlClient.request(DenyDoc, { body: v })).para.v1.proposalDeny, 'result'),
  )
export const useHold = () =>
  useReviewMutation(
    async (v: {
      id: string
      item: string
      kind: Para_ProposalHoldInputKind
      text: string
      project?: string
      area?: string
    }) => need((await gqlClient.request(HoldDoc, { body: v })).para.v1.proposalHold, 'result'),
  )
export const useItemRequest = () =>
  useReviewMutation(
    async (v: {
      id: string
      item: string
      note: string
      intent?: Para_ProposalRequestInputIntent
    }) => need((await gqlClient.request(RequestDoc, { body: v })).para.v1.proposalRequest, 'result'),
  )
export const useEdit = () =>
  useReviewMutation(
    async (v: {
      id: string
      item: string
      body?: string
      target?: string
      title?: string
      subject?: string
      input?: Record<string, unknown>
    }) => need((await gqlClient.request(EditDoc, { body: v })).para.v1.proposalEdit, 'result'),
  )
