import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'

// Notes are the ## Notes section of any item. A save names the hash it read; the server refuses it
// when the notes changed since, with the current hash in the message.

const NotesReadDoc = graphql(/* GraphQL */ `
  query NotesRead($id: String!) {
    para {
      v1 {
        notesRead(id: $id) {
          id
          md
          hash
        }
      }
    }
  }
`)

const NotesSaveDoc = graphql(/* GraphQL */ `
  mutation NotesSave($body: para_NotesEditInputInput!) {
    para {
      v1 {
        notesSave(body: $body) {
          id
          hash
        }
      }
    }
  }
`)

const MergesDoc = graphql(/* GraphQL */ `
  query DocMerges($id: String!) {
    para {
      v1 {
        docMerges(id: $id) {
          id
          duplicateOf
          archived
          mergedFrom {
            id
            title
            kind
          }
        }
      }
    }
  }
`)

export function useNotes(id: string) {
  return useQuery({
    queryKey: ['notes', id],
    retry: false,
    queryFn: async () =>
      need((await gqlClient.request(NotesReadDoc, { id })).para.v1.notesRead, 'notes'),
  })
}

export function useSaveNotes(id: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (v: { hash: string; md: string }) =>
      need(
        (
          await gqlClient.request(NotesSaveDoc, {
            body: { id, hash: v.hash || undefined, md: v.md },
          })
        ).para.v1.notesSave,
        'notes',
      ),
    onSettled: () => qc.invalidateQueries(),
  })
}

export function useMerges(id: string) {
  return useQuery({
    queryKey: ['merges', id],
    retry: false,
    queryFn: async () =>
      need((await gqlClient.request(MergesDoc, { id })).para.v1.docMerges, 'merges'),
  })
}
