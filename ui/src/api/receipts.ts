import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'
import type { Para_ReceiptCheckInputVerdict, Para_ReceiptListCheck } from '@/gql/graphql'

const ListDoc = graphql(/* GraphQL */ `
  query ReceiptList($doc: String, $check: para_receiptListCheck, $limit: Int) {
    para {
      v1 {
        receiptList(doc: $doc, check: $check, limit: $limit) {
          items {
            id
            title
            actor
            op
            doc
            docName
            why
            check
            selfReported
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
          receipt {
            id
            sources
            verify
            confidence
            why
          }
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
          request
        }
      }
    }
  }
`)

const UndoDoc = graphql(/* GraphQL */ `
  mutation ReceiptUndo($body: para_ReceiptUndoInputInput!) {
    para {
      v1 {
        receiptUndo(body: $body) {
          undone {
            receipt
            doc
            rev
          }
        }
      }
    }
  }
`)

export function useReceipts(v: { doc?: string; check?: Para_ReceiptListCheck; limit?: number }) {
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

export function useReceiptCheck() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (v: { id: string; verdict: Para_ReceiptCheckInputVerdict; note?: string }) =>
      need((await gqlClient.request(CheckDoc, { body: v })).para.v1.receiptCheck, 'result'),
    onSuccess: () => void qc.invalidateQueries(),
  })
}

export function useReceiptUndo() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (v: { id: string }) =>
      need((await gqlClient.request(UndoDoc, { body: v })).para.v1.receiptUndo, 'result'),
    onSuccess: () => void qc.invalidateQueries(),
  })
}

export function useActivity() {
  return useQuery({
    queryKey: ['activity'],
    queryFn: async () =>
      need((await gqlClient.request(ActivityDoc)).para.v1.agentActivity, 'activity').items,
  })
}
