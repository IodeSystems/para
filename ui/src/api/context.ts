import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { errorText, gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'
import type { CaptureCall } from '@/lib/context'
import { tellText, type TellResult } from '@/lib/tell'

// Context is what an item is for: where it came from, who it is for, and why the owner does it.
// The server reads what the vault already links; it stores nothing.

// DepRowFields is one row of what an item waits on or what waits on it.
export const DepRowFields = graphql(/* GraphQL */ `
  fragment DepRowFields on para_DepRow {
    id
    title
    type
    state
    open
    optional
    reason
    via
  }
`)

const ContextDoc = graphql(/* GraphQL */ `
  query Context($id: String!) {
    para {
      v1 {
        context(id: $id) {
          id
          type
          looking
          later
          laterRef
          partOf {
            kind
            via
            id
            title
          }
          blockedBy {
            id
            type
            state
            title
          }
          waitsOn {
            ...DepRowFields
          }
          blocks {
            ...DepRowFields
          }
          cameFrom {
            kind
            id
            title
            medium
            mediumWords
            from
            fromId
            when
            url
            text
          }
          for {
            role
            id
            name
          }
          reasons {
            kind
            ref
            title
            due
            status
            text
            via {
              how
              id
              title
            }
          }
        }
      }
    }
  }
`)

export type ItemContextData = NonNullable<ReturnType<typeof useItemContext>['data']>

export function useItemContext(id: string) {
  return useQuery({
    queryKey: ['context', id],
    queryFn: async () =>
      need((await gqlClient.request(ContextDoc, { id })).para.v1.context, 'context'),
  })
}

const IOAddDoc = graphql(/* GraphQL */ `
  mutation IOAdd($body: para_IOAddInputInput!) {
    para {
      v1 {
        ioAdd(body: $body) {
          id
          warnings
        }
      }
    }
  }
`)

export const ThreadPostDoc = graphql(/* GraphQL */ `
  mutation ThreadPost($body: para_ThreadPostInputInput!) {
    para {
      v1 {
        threadPost(body: $body) {
          request
          status
          note
          thread
        }
      }
    }
  }
`)

const ThreadWaitingDoc = graphql(/* GraphQL */ `
  query ThreadWaiting($id: String!) {
    para {
      v1 {
        thread(id: $id) {
          waitingNoAgent
        }
      }
    }
  }
`)

// CaptureRun: tell is the owner's words to send to the agent after the item is saved.
export type CaptureRun = { call: CaptureCall; tell: string }
export type CaptureDone = {
  id: string
  warnings: string[]
  // told is the agent turn; tellError says why it was not sent (the words are saved anyway).
  told?: TellResult
  tellError?: string
}

// useCapture saves the owner's words as an io item (io_add), then posts a thread turn on it for
// the agent to decide what it is. A failed turn does not undo the save.
export function useCapture() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async ({ call: c, tell }: CaptureRun): Promise<CaptureDone> => {
      const r = await gqlClient.request(IOAddDoc, { body: c.body })
      const added = need(r.para.v1.ioAdd, 'message')
      try {
        const p = need(
          (
            await gqlClient.request(ThreadPostDoc, {
              body: { key: added.id, kind: 'other', text: tellText(tell) },
            })
          ).para.v1.threadPost,
          'thread',
        )
        let waitingNoAgent: boolean | null = null
        try {
          waitingNoAgent = need(
            (await gqlClient.request(ThreadWaitingDoc, { id: added.id })).para.v1.thread,
            'thread',
          ).waitingNoAgent
        } catch {
          // the toast then goes by the post's status and note
        }
        return {
          ...added,
          told: { request: p.request, status: p.status, note: p.note, waitingNoAgent },
        }
      } catch (e) {
        return { ...added, tellError: errorText(e) }
      }
    },
    onSettled: () => qc.invalidateQueries(),
  })
}

const DependsSetDoc = graphql(/* GraphQL */ `
  mutation DependsSet($body: para_DependsSetInputInput!) {
    para {
      v1 {
        dependsSet(body: $body) {
          id
          rev
        }
      }
    }
  }
`)

export type DependsSetBody = {
  id: string
  on?: string
  reason?: string
  optional?: boolean
  remove?: boolean
}

// useDependsSet records, removes, or makes optional one thing an item waits on (depends_set).
export function useDependsSet() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (body: DependsSetBody) =>
      need((await gqlClient.request(DependsSetDoc, { body })).para.v1.dependsSet, 'depends'),
    onSettled: () => qc.invalidateQueries(),
  })
}
