import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
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

const NoteEditDoc = graphql(/* GraphQL */ `
  mutation NoteEdit($body: para_NoteEditInputInput!) {
    para {
      v1 {
        noteEdit(body: $body) {
          id
          rev
        }
      }
    }
  }
`)

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

export function useDoc(id: string) {
  return useQuery({
    queryKey: ['doc', id],
    retry: false,
    queryFn: async () => need((await gqlClient.request(DocDoc, { id })).para.v1.uiDoc, 'document'),
  })
}

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

// useNoteEdit changes the title and body of a note. Only a person can; it is a direct change.
export function useNoteEdit() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (v: { id: string; title: string; body: string }) =>
      need((await gqlClient.request(NoteEditDoc, { body: v })).para.v1.noteEdit, 'result'),
    onSuccess: () => void qc.invalidateQueries(),
  })
}
