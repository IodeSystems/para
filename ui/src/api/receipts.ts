import { useQuery } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'

// Receipts are what each agent write changed, why, and from what. The viewer only reads them:
// an undo is a CLI command the page shows for copying.

const ListDoc = graphql(/* GraphQL */ `
  query ReceiptList($doc: String, $limit: Int) {
    para {
      v1 {
        receiptList(doc: $doc, limit: $limit) {
          items {
            id
            title
            actor
            op
            doc
            docName
            why
            sources
            verify
            surface
            background
            ownerSaid
            selfReported
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

export type Receipt = NonNullable<ReturnType<typeof useReceipts>['data']>[number]

export function useReceipts(v: { doc?: string; limit?: number }) {
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
