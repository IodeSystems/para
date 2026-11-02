import { useQuery } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'
import type { Para_SimilarType } from '@/gql/graphql'

const SimilarDoc = graphql(/* GraphQL */ `
  query Similar($type: para_similarType!, $title: String!) {
    para {
      v1 {
        similar(type: $type, title: $title) {
          items {
            id
            title
          }
        }
      }
    }
  }
`)

// useSimilar finds what may already exist, once the title has a few words.
export function useSimilar(type: Para_SimilarType, title: string) {
  const t = title.trim().split('\n')[0] ?? ''
  return useQuery({
    queryKey: ['similar', type, t],
    enabled: t.length >= 8,
    queryFn: async () =>
      need((await gqlClient.request(SimilarDoc, { type, title: t })).para.v1.similar, 'similar')
        .items,
  })
}
