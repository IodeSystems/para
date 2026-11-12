import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'

// Tracked documents: remote documents the owner follows, with every version and the comments
// people made on them. Agents add versions; the owner reads the change and marks it seen
// (review:decide).

const DocTrackedDoc = graphql(/* GraphQL */ `
  query DocTracked($status: para_docTrackedStatus, $unseen: Boolean) {
    para {
      v1 {
        docTracked(status: $status, unseen: $unseen) {
          unseen
          openComments
          docs {
            id
            title
            url
            system
            status
            owner
            goal
            area
            latest
            lastSeen
            unseen
            openComments
            lostAnchors
            checked
            seenAt
          }
        }
      }
    }
  }
`)

const DocShowDoc = graphql(/* GraphQL */ `
  query DocTrackedShow($id: String!, $version: Int) {
    para {
      v1 {
        docTrackedShow(id: $id, version: $version) {
          doc {
            id
            title
            url
            system
            status
            owner
            goal
            area
            latest
            lastSeen
            unseen
            openComments
            checked
            seenAt
          }
          note
          watchers
          route
          version
          text
          versions {
            version
            id
            fetchedAt
            fetchedBy
            hash
            fidelity
            proposal
            item
            seen
          }
        }
      }
    }
  }
`)

const DocDiffDoc = graphql(/* GraphQL */ `
  query DocDiff($id: String!, $from: Int, $to: Int, $unit: String) {
    para {
      v1 {
        docDiff(id: $id, from: $from, to: $to, unit: $unit) {
          from
          to
          unit
          same
          added
          removed
          parts {
            op
            text
            a
            b
          }
        }
      }
    }
  }
`)

const DocCommentsDoc = graphql(/* GraphQL */ `
  query DocComments($id: String!) {
    para {
      v1 {
        docComments(id: $id) {
          latest
          open
          comments {
            id
            by
            at
            url
            text
            quote
            version
            start
            end
            anchor {
              version
              start
              end
              lost
            }
            became
            becameTitle
            becameClosed
            resolution
            resolvedAt
            note
            lost
          }
        }
      }
    }
  }
`)

const DocSeenDoc = graphql(/* GraphQL */ `
  mutation DocSeen($body: para_DocSeenInputInput!) {
    para {
      v1 {
        docSeen(body: $body) {
          id
          lastSeen
          unseen
        }
      }
    }
  }
`)

export function useDocTracked(unseen = false) {
  return useQuery({
    queryKey: ['docs', unseen],
    queryFn: async () =>
      need((await gqlClient.request(DocTrackedDoc, { unseen })).para.v1.docTracked, 'documents'),
  })
}

export function useDocShow(id: string, version = 0) {
  return useQuery({
    queryKey: ['docs', 'show', id, version],
    enabled: id !== '',
    queryFn: async () =>
      need(
        (await gqlClient.request(DocShowDoc, { id, version: version || undefined })).para.v1
          .docTrackedShow,
        'document',
      ),
  })
}

// useDocDiff compares two versions; from 0 and to 0 are the server's defaults (the last version
// seen, and the latest).
export function useDocDiff(id: string, from = 0, to = 0, unit = 'line') {
  return useQuery({
    queryKey: ['docs', 'diff', id, from, to, unit],
    enabled: id !== '',
    queryFn: async () =>
      need(
        (
          await gqlClient.request(DocDiffDoc, {
            id,
            from: from || undefined,
            to: to || undefined,
            unit,
          })
        ).para.v1.docDiff,
        'diff',
      ),
  })
}

export function useDocComments(id: string) {
  return useQuery({
    queryKey: ['docs', 'comments', id],
    enabled: id !== '',
    queryFn: async () =>
      need((await gqlClient.request(DocCommentsDoc, { id })).para.v1.docComments, 'comments'),
  })
}

export type DocComment = NonNullable<ReturnType<typeof useDocComments>['data']>['comments'][number]

// useDocSeen marks a document read up to a version (0: the latest). People only.
export function useDocSeen() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (body: { id: string; version?: number }) =>
      need((await gqlClient.request(DocSeenDoc, { body })).para.v1.docSeen, 'seen'),
    onSettled: () => qc.invalidateQueries(),
  })
}
