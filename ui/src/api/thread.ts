import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'
import { ThreadPostDoc } from '@/api/context'
import { latestReceipt, revertReceipt } from '@/api/receipts'
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
            edited {
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

const ThreadEditDoc = graphql(/* GraphQL */ `
  mutation ThreadEdit($body: para_ThreadEditInputInput!) {
    para {
      v1 {
        threadEdit(body: $body) {
          request
          status
        }
      }
    }
  }
`)

const ThreadWithdrawDoc = graphql(/* GraphQL */ `
  mutation ThreadWithdraw($body: para_ThreadWithdrawInputInput!) {
    para {
      v1 {
        threadWithdraw(body: $body) {
          id
          status
        }
      }
    }
  }
`)

export type ThreadEditBody = { id: string; text?: string; kind?: PostKind; quote?: string }

// editTurn changes an owner turn no agent has claimed (thread_edit); the server keeps the old words
// in the request and refuses a claimed turn.
export async function editTurn(b: ThreadEditBody) {
  return need((await gqlClient.request(ThreadEditDoc, { body: b })).para.v1.threadEdit, 'turn')
}

// withdrawTurn cancels an owner turn (thread_withdraw). Its one receipt undoes it: the returned undo
// reverts the newest thread_withdraw receipt, read right after the write.
export async function withdrawTurn(id: string): Promise<{ undo?: () => Promise<void> }> {
  need(
    (await gqlClient.request(ThreadWithdrawDoc, { body: { id } })).para.v1.threadWithdraw,
    'turn',
  )
  const receipt = await latestReceipt('thread_withdraw').catch(() => null)
  return { undo: receipt ? () => revertReceipt(receipt).then(() => {}) : undefined }
}

export function useThreadEdit() {
  const qc = useQueryClient()
  return useMutation({ mutationFn: editTurn, onSettled: () => qc.invalidateQueries() })
}

export function useThreadWithdraw() {
  const qc = useQueryClient()
  return useMutation({ mutationFn: withdrawTurn, onSettled: () => qc.invalidateQueries() })
}

export function useThreadPost() {
  const qc = useQueryClient()
  return useMutation({ mutationFn: postThread, onSettled: () => qc.invalidateQueries() })
}
