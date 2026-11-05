import { useMutation, useQueryClient } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'

// The composer's calls: send the owner's words about an item to an agent (agent_send), take it back
// while no agent has it (agent_unsend), and close a task or a meeting the owner saved notes on
// (item_close).

const AgentSendDoc = graphql(/* GraphQL */ `
  mutation AgentSend($body: para_AgentSendInputInput!) {
    para {
      v1 {
        agentSend(body: $body) {
          request
          status
          title
          note
        }
      }
    }
  }
`)

const AgentUnsendDoc = graphql(/* GraphQL */ `
  mutation AgentUnsend($body: para_AgentUnsendInputInput!) {
    para {
      v1 {
        agentUnsend(body: $body) {
          id
          status
        }
      }
    }
  }
`)

const ItemCloseDoc = graphql(/* GraphQL */ `
  mutation ItemClose($body: para_ItemCloseInputInput!) {
    para {
      v1 {
        itemClose(body: $body) {
          id
          by
        }
      }
    }
  }
`)

export function useAgentSend() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (body: {
      id: string
      text?: string
      attachments?: string[]
      done?: boolean
    }) => need((await gqlClient.request(AgentSendDoc, { body })).para.v1.agentSend, 'agent send'),
    onSettled: () => qc.invalidateQueries(),
  })
}

export async function agentUnsend(request: string) {
  return need(
    (await gqlClient.request(AgentUnsendDoc, { body: { request } })).para.v1.agentUnsend,
    'agent unsend',
  )
}

// closeItem marks a task done or a meeting held (item_close) after the owner saved notes on it.
export async function closeItem(id: string) {
  return need((await gqlClient.request(ItemCloseDoc, { body: { id } })).para.v1.itemClose, 'close')
}
