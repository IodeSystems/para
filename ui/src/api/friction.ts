import { useQuery } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'

const FrictionDoc = graphql(/* GraphQL */ `
  query ReportFriction($by: String, $since: String) {
    para {
      v1 {
        reportFriction(by: $by, since: $since) {
          by
          since
          asks
          withFriction
          rows {
            key
            label
            name
            asks
            askNames
            stalls
            redirects
            hopsPerAsk
            firstAnswerDays
            resolutionDays
            stalledDays
            policyConflicts
            exceptionsHit
            playbookCandidates
          }
          repeats {
            title
            asks
            askNames
          }
        }
      }
    }
  }
`)

export function useFriction(by: string, since: string) {
  return useQuery({
    queryKey: ['friction', by, since],
    queryFn: async () =>
      need(
        (await gqlClient.request(FrictionDoc, { by, since: since || undefined })).para.v1
          .reportFriction,
        'friction',
      ),
  })
}
