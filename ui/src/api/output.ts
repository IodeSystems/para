import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'
import { revertReceipt } from '@/api/receipts'
import { ioReview } from '@/api/io'

// The Output page reads what went out (io_sent) and what waits for approval (io_proposals).

const IoSentDoc = graphql(/* GraphQL */ `
  query IoSent($limit: Int, $medium: para_ioSentMedium, $since: String, $to: String) {
    para {
      v1 {
        ioSent(limit: $limit, medium: $medium, since: $since, to: $to) {
          rows {
            id
            title
            to
            at
            medium
            url
            group
            variant
            of
            variantOf
            textChanged
            reviewed
            reply {
              id
              at
              from
              subject
            }
            ask {
              id
              type
              status
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
`)

export type SentRow = NonNullable<
  ReturnType<typeof useIoSent>['data']
>[number]

export type SentFilter = { medium?: string; to?: string; since?: string }

export function useIoSent(f: SentFilter, limit = 100) {
  return useQuery({
    queryKey: ['io', 'sent', f.medium ?? '', f.to ?? '', f.since ?? '', limit],
    queryFn: async () =>
      need(
        (
          await gqlClient.request(IoSentDoc, {
            medium: f.medium as never,
            to: f.to,
            since: f.since,
            limit,
          })
        ).para.v1.ioSent,
        'sent messages',
      ).rows,
  })
}

const IoProposalsCountDoc = graphql(/* GraphQL */ `
  query IoProposalsCount {
    para {
      v1 {
        ioProposals {
          count
          needsYou
        }
      }
    }
  }
`)

export function useIoProposalsCount() {
  return useQuery({
    queryKey: ['io', 'proposals', 'count'],
    queryFn: async () =>
      need((await gqlClient.request(IoProposalsCountDoc)).para.v1.ioProposals, 'proposals'),
  })
}

const IoReviewReceiptDoc = graphql(/* GraphQL */ `
  query IoReviewReceipt {
    para {
      v1 {
        receiptList(op: "io_review", limit: 1) {
          items {
            id
            undone
          }
        }
      }
    }
  }
`)

// reviewIo marks io items reviewed (io_review, a person op). The write leaves one receipt; undo
// reverts the newest io_review receipt, read right after the write.
export async function reviewIo(
  ids: string[],
): Promise<{ count: number; undo?: () => Promise<void> }> {
  const out = await ioReview(ids)
  const r = (await gqlClient.request(IoReviewReceiptDoc)).para.v1.receiptList?.items[0]
  const id = r && !r.undone ? r.id : undefined
  return {
    count: out.reviewed.length,
    undo: id ? () => revertReceipt(id).then(() => {}) : undefined,
  }
}

export function useIoReview() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: reviewIo,
    onSettled: () => qc.invalidateQueries(),
  })
}
