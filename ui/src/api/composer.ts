import { useMutation, useQueryClient } from '@tanstack/react-query'
import { closeAny, sendToAgent } from '@/api/verbs'

// The composer's calls: send the owner's words about an item to an agent (send_to_agent, which runs
// agent_send), take it back while no agent has it (close on the request), and close a task or a meeting the
// owner saved notes on (close).

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

// agentUnsend takes a request back while no agent has it: close on the request id with no outcome
// cancels it.
export async function agentUnsend(request: string) {
  await closeAny({ id: request })
}

// closeItem marks a task done, a meeting held, or an ask closed (close with no outcome) after the
// owner saved notes on it. receipt undoes it.
export async function closeItem(id: string) {
  const r = await closeAny({ id })
  return { id, by: r.op, receipt: r.receipt }
}
