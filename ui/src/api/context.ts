import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { errorText, gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'
import type { CaptureCall, FileTask } from '@/lib/context'

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

const InteractionAddDoc = graphql(/* GraphQL */ `
  mutation InteractionAdd($body: para_InteractionAddInputInput!) {
    para {
      v1 {
        interactionAdd(body: $body) {
          id
          warnings
        }
      }
    }
  }
`)

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

const IOFileDoc = graphql(/* GraphQL */ `
  mutation IOFile($body: para_IOFileInputInput!) {
    para {
      v1 {
        ioFile(body: $body) {
          id
          became
          warnings
        }
      }
    }
  }
`)

export type CaptureRun = { call: CaptureCall; task?: FileTask | null }
export type CaptureDone = { id: string; warnings: string[]; became?: string }

// useCapture logs a conversation the owner had (interaction_add, or io_add for a name not in the
// vault) or something they carried over by hand (io_add medium swivel). With task, the item it just
// added is filed into a new task (io_file), so the task's Context shows Came from. If the filing
// fails the item stays in the inbox and the error says so.
export function useCapture() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async ({ call: c, task }: CaptureRun): Promise<CaptureDone> => {
      if (c.op === 'interaction_add') {
        const r = await gqlClient.request(InteractionAddDoc, { body: c.body })
        return need(r.para.v1.interactionAdd, 'conversation')
      }
      const r = await gqlClient.request(IOAddDoc, { body: c.body })
      const added = need(r.para.v1.ioAdd, 'message')
      if (!task) return added
      try {
        const f = await gqlClient.request(IOFileDoc, { body: { id: added.id, ...task } })
        const filed = need(f.para.v1.ioFile, 'task')
        return {
          id: added.id,
          warnings: [...added.warnings, ...(filed.warnings ?? [])],
          became: filed.became,
        }
      } catch (e) {
        throw new Error(`Logged, but could not make a task (it is in your inbox): ${errorText(e)}`)
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
