import { useMutation, useQuery } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'
import type { Para_CopyInputAs } from '@/gql/graphql'

const DocDoc = graphql(/* GraphQL */ `
  query Doc($id: String!) {
    para {
      v1 {
        uiDoc(id: $id) {
          id
          title
          body
          folder
          rev
          meta {
            type
            status
            created
            updated
            tags
            url
            verified
            start
            scheduled
            kind
            optional
            attend
            source {
              url
              fidelity
              status
              checked
            }
          }
        }
      }
    }
  }
`)

export function useDoc(id: string) {
  return useQuery({
    queryKey: ['doc', id],
    retry: false,
    queryFn: async () => need((await gqlClient.request(DocDoc, { id })).para.v1.uiDoc, 'document'),
  })
}

// CopyDoc is a vault:read op: it changes no document, it logs the egress and returns the text.
const CopyDoc = graphql(/* GraphQL */ `
  mutation CopyDoc($body: para_CopyInputInput!) {
    para {
      v1 {
        copy(body: $body) {
          text
          marking
          destination
        }
      }
    }
  }
`)

// useCopyText renders a document for pasting. clipboard: true logs the destination as the
// clipboard: the browser does the copy, and the server logs the egress before it returns the text.
export function useCopyText() {
  return useMutation({
    mutationFn: async (a: { id: string; as: Para_CopyInputAs }) =>
      need(
        (await gqlClient.request(CopyDoc, { body: { id: a.id, as: a.as, clipboard: true } })).para
          .v1.copy,
        'copy',
      ),
  })
}
