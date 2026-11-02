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

const DocFieldsDoc = graphql(/* GraphQL */ `
  query DocFields($id: String!) {
    para {
      v1 {
        uiDocFields(id: $id) {
          editable
          fields {
            name
            kind
            enum
            required
            value
          }
        }
      }
    }
  }
`)

const DocEditDoc = graphql(/* GraphQL */ `
  mutation DocEdit($body: para_DocEditInputInput!) {
    para {
      v1 {
        docEdit(body: $body) {
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

// useDocFields lists the fields of a document a person may edit, with their values.
export function useDocFields(id: string) {
  return useQuery({
    queryKey: ['docfields', id],
    retry: false,
    queryFn: async () =>
      need((await gqlClient.request(DocFieldsDoc, { id })).para.v1.uiDocFields, 'fields'),
  })
}

// useDocEdit changes the title, body, or declared fields of a document. rev is the revision the
// form was opened from; the server refuses the edit when the document has changed since.
export function useDocEdit() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (v: {
      id: string
      title: string
      body: string
      rev: string
      fields: Record<string, unknown>
    }) => need((await gqlClient.request(DocEditDoc, { body: v })).para.v1.docEdit, 'result'),
    onSuccess: () => void qc.invalidateQueries(),
  })
}
