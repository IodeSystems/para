import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'
import type { Para_ProposalHoldInputKind } from '@/gql/graphql'

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
            op
            input
            medium
            target
            draft
            state
            edited
            reason
            result
            heldBy
            hash
            approvedHash
            openAnnotations
            actions
            evidence {
              locator
              quote
            }
          }
          annotations {
            id
            item
            quote
            note
            by
            revision
            state
            answer
            anchored
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
const ReworkDoc = graphql(/* GraphQL */ `
  mutation ProposalRework($body: para_ProposalReasonInputInput!) {
    para {
      v1 {
        proposalRework(body: $body) {
          ...ProposalReviewFields
        }
      }
    }
  }
`)
const SendbackDoc = graphql(/* GraphQL */ `
  mutation ProposalSendback($body: para_ProposalSendbackInputInput!) {
    para {
      v1 {
        proposalSendback(body: $body) {
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
const AnnotateDoc = graphql(/* GraphQL */ `
  mutation ProposalAnnotate($body: para_ProposalAnnotateInputInput!) {
    para {
      v1 {
        proposalAnnotate(body: $body) {
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
    onSuccess: () => qc.invalidateQueries(),
  })
}

export const useApprove = () =>
  useReviewMutation(async (v: { id: string; items: string[] }) =>
    need((await gqlClient.request(ApproveDoc, { body: v })).para.v1.proposalApprove, 'result'),
  )
export const useDeny = () =>
  useReviewMutation(async (v: { id: string; item: string; reason: string }) =>
    need((await gqlClient.request(DenyDoc, { body: v })).para.v1.proposalDeny, 'result'),
  )
export const useRework = () =>
  useReviewMutation(async (v: { id: string; item: string; reason: string }) =>
    need((await gqlClient.request(ReworkDoc, { body: v })).para.v1.proposalRework, 'result'),
  )
export const useSendback = () =>
  useReviewMutation(async (v: { id: string; item: string; reason?: string }) =>
    need((await gqlClient.request(SendbackDoc, { body: v })).para.v1.proposalSendback, 'result'),
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
export const useAnnotate = () =>
  useReviewMutation(async (v: { id: string; item: string; quote: string; note: string }) =>
    need((await gqlClient.request(AnnotateDoc, { body: v })).para.v1.proposalAnnotate, 'result'),
  )
export const useEdit = () =>
  useReviewMutation(
    async (v: {
      id: string
      item: string
      draft?: string
      target?: string
      input?: Record<string, unknown>
    }) => need((await gqlClient.request(EditDoc, { body: v })).para.v1.proposalEdit, 'result'),
  )
