import { useQuery } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'

// The brief of a meeting (brief, a read op): who is in it and what is open with each of them, what it
// is linked to, and what came of the last occurrence. The server builds it from the vault only.

export const BriefLineFields = graphql(/* GraphQL */ `
  fragment BriefLineFields on para_BriefLine {
    id
    title
    type
    status
    reason
  }
`)

const BriefDoc = graphql(/* GraphQL */ `
  query Brief($id: String!) {
    para {
      v1 {
        brief(id: $id) {
          id
          doneWhen
          agenda
          people {
            id
            name
            text
            resolved
            unresolved
            youAsked {
              ...BriefLineFields
            }
            theyAsked {
              ...BriefLineFields
            }
            tasks {
              ...BriefLineFields
            }
            questions {
              ...BriefLineFields
            }
          }
          linked {
            ...BriefLineFields
          }
          previous {
            id
            title
            start
            notes
            outcome
            skipped
          }
        }
      }
    }
  }
`)

export function useBrief(id: string, enabled = true) {
  return useQuery({
    queryKey: ['brief', id],
    enabled,
    retry: false,
    queryFn: async () => need((await gqlClient.request(BriefDoc, { id })).para.v1.brief, 'brief'),
  })
}
