import { useQuery } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'

// Conversations are the owner's threads with the agent that are not over: the io is open, a turn
// is unfinished, or a done reply is not yet seen (conversations, a read).
const ConversationsDoc = graphql(/* GraphQL */ `
  query Conversations {
    para {
      v1 {
        conversations {
          items {
            id
            title
            state
            created
            lastAt
            lastKind
            lastStatus
            lastReply
            turns
            queued
            claimed
            waitingNoAgent
          }
        }
      }
    }
  }
`)

export function useConversations() {
  return useQuery({
    queryKey: ['conversations'],
    queryFn: async () =>
      need((await gqlClient.request(ConversationsDoc)).para.v1.conversations, 'conversations').items,
  })
}

export type ConversationRow = NonNullable<ReturnType<typeof useConversations>['data']>[number]
