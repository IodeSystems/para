import { useQuery } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'

// The generic verbs (close, later, reopen, send_to_agent): any id. Each runs the op of the item's
// type on the server and says which (op); a close, later, or reopen by the owner leaves a receipt
// that receipt_revert undoes, so Done has an Undo.

const CloseDoc = graphql(/* GraphQL */ `
  mutation Close($body: para_CloseInputInput!) {
    para {
      v1 {
        close(body: $body) {
          op
          receipt
          result
        }
      }
    }
  }
`)

const LaterDoc = graphql(/* GraphQL */ `
  mutation Later($body: para_LaterInputInput!) {
    para {
      v1 {
        later(body: $body) {
          op
          receipt
          result
        }
      }
    }
  }
`)

const ReopenDoc = graphql(/* GraphQL */ `
  mutation Reopen($body: para_ReopenInputInput!) {
    para {
      v1 {
        reopen(body: $body) {
          op
          receipt
          result
        }
      }
    }
  }
`)

const SendToAgentDoc = graphql(/* GraphQL */ `
  mutation SendToAgent($body: para_SendToAgentInputInput!) {
    para {
      v1 {
        sendToAgent(body: $body) {
          op
          result
        }
      }
    }
  }
`)

// VerbResult is what a verb did: the op that ran, its receipt (the owner's writes), and that op's
// output.
export type VerbResult = { op: string; receipt?: string | null; result: Record<string, unknown> }

const asResult = (r: { op: string; receipt?: string | null; result?: unknown }): VerbResult => ({
  op: r.op,
  receipt: r.receipt,
  result: (r.result ?? {}) as Record<string, unknown>,
})

export type CloseBody = { id: string; outcome?: string; reason?: string; fields?: unknown }

export async function closeAny(body: CloseBody): Promise<VerbResult> {
  return asResult(need((await gqlClient.request(CloseDoc, { body })).para.v1.close, 'close'))
}

export type LaterAnyBody = {
  id?: string
  ids?: string[]
  items?: { proposal: string; item: string }[]
  until?: string
  reason?: string
}

export async function laterAny(body: LaterAnyBody): Promise<VerbResult> {
  return asResult(need((await gqlClient.request(LaterDoc, { body })).para.v1.later, 'later'))
}

export type ReopenBody = {
  id?: string
  ids?: string[]
  items?: { proposal: string; item: string }[]
  reason?: string
}

export async function reopenAny(body: ReopenBody): Promise<VerbResult> {
  return asResult(need((await gqlClient.request(ReopenDoc, { body })).para.v1.reopen, 'reopen'))
}

export type SendBody = { id: string; note?: string; attachments?: string[]; done?: boolean }

export async function sendToAgent(body: SendBody): Promise<VerbResult> {
  return asResult(
    need((await gqlClient.request(SendToAgentDoc, { body })).para.v1.sendToAgent, 'send to agent'),
  )
}

// strings reads a list of strings from a verb's result.
export function strings(v: unknown): string[] {
  return Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : []
}

const ListDoc = graphql(/* GraphQL */ `
  query ListItems($type: para_listType, $state: String) {
    para {
      v1 {
        list(type: $type, state: $state) {
          count
          rows {
            id
            title
            type
            status
            life
          }
        }
      }
    }
  }
`)

// useClosedList lists the closed items of a type (list, state closed), for Show done.
export function useClosedList(type: 'task' | 'question', enabled: boolean) {
  return useQuery({
    queryKey: ['list', type, 'closed'],
    enabled,
    queryFn: async () =>
      need((await gqlClient.request(ListDoc, { type, state: 'closed' })).para.v1.list, 'list').rows,
  })
}
