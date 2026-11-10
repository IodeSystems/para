import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'
import { ThreadPostDoc } from '@/api/context'
import type { PostKind } from '@/lib/thread'

const ThreadDoc = graphql(/* GraphQL */ `
  query ThreadOf($id: String!) {
    para {
      v1 {
        thread(id: $id) {
          key
          kind
          waitingNoAgent
          queued
          claimed
          agents
          turns {
            kind
            at
            request
            status
            title
            body
            ownerWords
            turnKind
            quote
            attachments
            claimedBy
            claimedAt
            resolution
            result
            reply
            error
            notes {
              actor
              at
              text
            }
            steps {
              heading
              text
            }
            io
            direction
            medium
            state
            from
            to
            subject
            attempts {
              at
              result
              error
              tool
              url
            }
            receipts {
              id
              op
              doc
              docName
              why
              undone
              actor
              at
            }
          }
        }
      }
    }
  }
`)

// useThreadOf reads the thread of an item (thread, read only). The shell's live refresh and
// polling keep it current.
export function useThreadOf(id: string) {
  return useQuery({
    queryKey: ['thread', id],
    queryFn: async () =>
      need((await gqlClient.request(ThreadDoc, { id })).para.v1.thread, 'thread'),
  })
}

export type ThreadData = NonNullable<ReturnType<typeof useThreadOf>['data']>
export type ThreadTurnData = ThreadData['turns'][number]

export type ThreadPostBody = {
  id: string
  text: string
  kind: PostKind
  quote?: string
  attachments?: string[]
}

// postThread sends the owner's words on an item to the agent (thread_post).
export async function postThread(b: ThreadPostBody) {
  const body = { ...b, quote: b.quote || undefined }
  return need((await gqlClient.request(ThreadPostDoc, { body })).para.v1.threadPost, 'thread')
}

export function useThreadPost() {
  const qc = useQueryClient()
  return useMutation({ mutationFn: postThread, onSettled: () => qc.invalidateQueries() })
}
