import { useQuery } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'

const PolicyDoc = graphql(/* GraphQL */ `
  query Policy {
    para {
      v1 {
        policyList {
          policies {
            id
            title
            url
            verified
            fidelity
            rules
            sourceStatus
          }
        }
        ruleList {
          rules {
            id
            policy
            text
            link
          }
        }
        exceptionList {
          exceptions {
            id
            title
            policy
            rule
            kind
            status
            by
            reviewBy
          }
        }
        constraintList {
          constraints {
            id
            says
            why
            by
            appliesIn
            notIn
            reviewBy
            sourceUrl
          }
        }
      }
    }
  }
`)

export function usePolicy() {
  return useQuery({
    queryKey: ['policy'],
    queryFn: async () => {
      const v = (await gqlClient.request(PolicyDoc)).para.v1
      return {
        policies: need(v.policyList, 'policies').policies,
        rules: need(v.ruleList, 'rules').rules,
        exceptions: need(v.exceptionList, 'exceptions').exceptions,
        constraints: need(v.constraintList, 'constraints').constraints,
      }
    },
  })
}
