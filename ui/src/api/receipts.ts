import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'

// Receipts are what each agent write changed, why, and from what. A key with review:decide marks
// a receipt verified or wrong and undoes the write; any other key only reads them.

const ListDoc = graphql(/* GraphQL */ `
  query ReceiptList(
    $doc: String
    $limit: Int
    $actor: String
    $op: String
    $check: para_receiptListCheck
    $since: String
    $until: String
  ) {
    para {
      v1 {
        receiptList(
          doc: $doc
          limit: $limit
          actor: $actor
          op: $op
          check: $check
          since: $since
          until: $until
        ) {
          items {
            id
            title
            actor
            op
            doc
            docName
            why
            sources
            sourceLinks {
              text
              url
              internal
            }
            verify
            confidence
            check
            checkedBy
            checkNote
            undone
            surface
            background
            ownerSaid
            selfReported
            by
            onBehalfOf
            request
            at
            created
          }
        }
      }
    }
  }
`)

const ActivityDoc = graphql(/* GraphQL */ `
  query AgentActivity {
    para {
      v1 {
        agentActivity {
          items {
            ts
            actor
            op
            doc
            docName
            request
            receipt
            why
            selfReported
          }
        }
      }
    }
  }
`)

const ShowDoc = graphql(/* GraphQL */ `
  query ReceiptShow($id: String!) {
    para {
      v1 {
        receiptShow(id: $id) {
          lines
          revertible
        }
      }
    }
  }
`)

const CheckDoc = graphql(/* GraphQL */ `
  mutation ReceiptCheck($body: para_ReceiptCheckInputInput!) {
    para {
      v1 {
        receiptCheck(body: $body) {
          id
          check
        }
      }
    }
  }
`)

const RevertDoc = graphql(/* GraphQL */ `
  mutation ReceiptRevert($body: para_ReceiptIDInputInput!) {
    para {
      v1 {
        receiptRevert(body: $body) {
          undone {
            receipt
            doc
          }
        }
      }
    }
  }
`)

const OutcomesDoc = graphql(/* GraphQL */ `
  query ReportOutcomes($since: String, $until: String) {
    para {
      v1 {
        reportOutcomes(since: $since, until: $until) {
          since
          until
          closes
          outcomes {
            key
            count
          }
          byActor {
            actor
            total
            outcomes {
              key
              count
            }
          }
          denied {
            proposal
            title
            item
            kind
            summary
            actor
            reason
            at
          }
          undos {
            receipt
            doc
            by
            at
          }
        }
      }
    }
  }
`)

export type Receipt = NonNullable<ReturnType<typeof useReceipts>['data']>[number]

export type ReceiptFilter = {
  doc?: string
  limit?: number
  actor?: string
  op?: string
  check?: 'unchecked' | 'verified' | 'wrong'
  since?: string
  until?: string
}

export function useReceipts(v: ReceiptFilter) {
  return useQuery({
    queryKey: ['receipts', v],
    queryFn: async () =>
      need((await gqlClient.request(ListDoc, v)).para.v1.receiptList, 'receipts').items,
  })
}

export function useReceipt(id: string, enabled: boolean) {
  return useQuery({
    queryKey: ['receipt', id],
    enabled,
    queryFn: async () =>
      need((await gqlClient.request(ShowDoc, { id })).para.v1.receiptShow, 'receipt'),
  })
}

export function useActivity() {
  return useQuery({
    queryKey: ['activity'],
    queryFn: async () =>
      need((await gqlClient.request(ActivityDoc)).para.v1.agentActivity, 'activity').items,
  })
}

export function useOutcomes(v: { since?: string; until?: string }) {
  return useQuery({
    queryKey: ['outcomes', v],
    queryFn: async () =>
      need((await gqlClient.request(OutcomesDoc, v)).para.v1.reportOutcomes, 'outcomes'),
  })
}

export type Outcomes = NonNullable<ReturnType<typeof useOutcomes>['data']>

// useReceiptCheck marks one receipt verified, or wrong with a note.
export function useReceiptCheck() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (body: { id: string; verdict: 'verified' | 'wrong'; note?: string }) =>
      need((await gqlClient.request(CheckDoc, { body })).para.v1.receiptCheck, 'check'),
    onSettled: () => qc.invalidateQueries(),
  })
}

// useReceiptRevert undoes one agent write, as the owner. The server refuses it when the document
// changed since.
export function useReceiptRevert() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (id: string) =>
      need((await gqlClient.request(RevertDoc, { body: { id } })).para.v1.receiptRevert, 'undo'),
    onSettled: () => qc.invalidateQueries(),
  })
}
