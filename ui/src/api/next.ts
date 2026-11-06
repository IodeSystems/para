import { useQuery } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'

// The value order (order): what needs the owner, ranked by the server, with why, reason, effort,
// and what an agent prepared. The Next page draws its cards from it.
const OrderDoc = graphql(/* GraphQL */ `
  query Order {
    para {
      v1 {
        order {
          rows {
            id
            kind
            title
            group
            bucket
            due
            why
            reason
            effort
            needsYou
            prep
            ready
            readyKind
            readyRef
          }
          summary {
            reply
            replyMinutes
            replyPrep
          }
        }
      }
    }
  }
`)

export function useOrder() {
  return useQuery({
    queryKey: ['order'],
    queryFn: async () => need((await gqlClient.request(OrderDoc)).para.v1.order, 'order'),
  })
}
