import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'

// Idea pools (pool_list): ideas put off someday with a pool's tag, and the trigger that brings them
// up. A firing pool is one row of the order (id pool:<name>); its ideas are ranked here.
const PoolsDoc = graphql(/* GraphQL */ `
  query PoolList {
    para {
      v1 {
        poolList {
          pools {
            name
            tag
            pick
            trigger
            fires
            instance
            triggerId
            triggerTitle
            date
            next
            title
            row
            size
            area
            goal
            problem
            ideas {
              id
              title
              kind
              text
              effort
              effortSet
              impact
              score
            }
          }
        }
      }
    }
  }
`)

export function usePools() {
  return useQuery({
    queryKey: ['pools'],
    queryFn: async () => need((await gqlClient.request(PoolsDoc)).para.v1.poolList, 'pools').pools,
  })
}

export type Pool = NonNullable<ReturnType<typeof usePools>['data']>[number]

const PickDoc = graphql(/* GraphQL */ `
  mutation PoolPick($body: para_PoolPickInputInput!) {
    para {
      v1 {
        poolPick(body: $body) {
          pool
          instance
          picked
          linked
          skipped
          warnings
        }
      }
    }
  }
`)

// usePoolPick is Pick or Skip this round on a firing pool (pool_pick, a person op): the picks come
// back, a task becomes prep of the obligation that fired it, and the round is recorded.
export function usePoolPick() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (body: { pool: string; ids?: string[]; skip?: boolean }) =>
      need((await gqlClient.request(PickDoc, { body })).para.v1.poolPick, 'pool pick'),
    onSettled: () => qc.invalidateQueries(),
  })
}

const IdeaAddDoc = graphql(/* GraphQL */ `
  mutation IdeaAdd($body: para_IdeaAddInputInput!) {
    para {
      v1 {
        ideaAdd(body: $body) {
          id
          pool
          tag
          warnings
        }
      }
    }
  }
`)

// useIdeaAdd keeps an idea in a pool (idea_add): a title and a sentence, put off someday.
export function useIdeaAdd() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (body: { pool: string; title: string; text?: string }) =>
      need((await gqlClient.request(IdeaAddDoc, { body })).para.v1.ideaAdd, 'idea add'),
    onSettled: () => qc.invalidateQueries(),
  })
}
