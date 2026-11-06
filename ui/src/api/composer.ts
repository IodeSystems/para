import { useMutation, useQueryClient } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'
import { closeAny, sendToAgent } from '@/api/verbs'

// The composer's calls: send the owner's words about an item to an agent (send_to_agent, which runs
// agent_send), take it back while no agent has it (agent_unsend), and close a task or a meeting the
// owner saved notes on (close).

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

export function useAgentSend() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (body: {
      id: string
      text?: string
      attachments?: string[]
      done?: boolean
    }) => {
      const r = await sendToAgent({
        id: body.id,
        note: body.text,
        attachments: body.attachments,
        done: body.done,
      })
      return {
        request: r.result.request as string,
        status: r.result.status as string,
        title: r.result.title as string,
        note: (r.result.note as string | undefined) ?? null,
      }
    },
    onSettled: () => qc.invalidateQueries(),
  })
}

export async function agentUnsend(request: string) {
  return need(
    (await gqlClient.request(AgentUnsendDoc, { body: { request } })).para.v1.agentUnsend,
    'agent unsend',
  )
}

// closeItem marks a task done, a meeting held, or an ask closed (close with no outcome) after the
// owner saved notes on it. receipt undoes it.
export async function closeItem(id: string) {
  const r = await closeAny({ id })
  return { id, by: r.op, receipt: r.receipt }
}
