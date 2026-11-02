import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'

export const ReviewFields = graphql(`
  fragment ReviewFields on para_UIReviewBody {
    count
    position
    canUndo
    needs {
      kind
      id
      title
    }
    groups {
      actor
      events {
        ts
        op
        doc
        from
        to
        surface
      }
    }
  }
`)

const ReviewDoc = graphql(`
  query Review {
    para {
      v1 {
        uiReview {
          ...ReviewFields
        }
      }
    }
  }
`)

const AcceptDoc = graphql(`
  mutation ReviewAccept($body: para_UIReviewAcceptInputInput!) {
    para {
      v1 {
        uiReviewAccept(body: $body) {
          ...ReviewFields
        }
      }
    }
  }
`)

const ReopenDoc = graphql(`
  mutation ReviewReopen {
    para {
      v1 {
        uiReviewReopen {
          ...ReviewFields
        }
      }
    }
  }
`)

export function useReview() {
  return useQuery({
    queryKey: ['review'],
    queryFn: async () => need((await gqlClient.request(ReviewDoc)).para.v1.uiReview, 'review'),
  })
}

// useAccept accepts every event up to `at`, or everything listed when `at` is empty.
export function useAccept() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (at: string) =>
      need(
        (await gqlClient.request(AcceptDoc, { body: at ? { at } : {} })).para.v1.uiReviewAccept,
        'review',
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['review'] }),
  })
}

export function useReopen() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async () =>
      need((await gqlClient.request(ReopenDoc)).para.v1.uiReviewReopen, 'review'),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['review'] }),
  })
}
