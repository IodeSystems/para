import { useQuery } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'

const UpdateStatusDoc = graphql(/* GraphQL */ `
  query UpdateStatus {
    para {
      v1 {
        updateStatus {
          running
          available
          notice
          release
          auto
          installedFrom
          installedTo
          installedAt
        }
      }
    }
  }
`)

// useUpdateStatus reads where this machine stands on para releases. It changes rarely, so it is
// read once an hour; a failure shows no banner.
export function useUpdateStatus() {
  return useQuery({
    queryKey: ['updateStatus'],
    queryFn: async () => (await gqlClient.request(UpdateStatusDoc)).para.v1.updateStatus ?? null,
    staleTime: 60 * 60 * 1000,
    retry: false,
  })
}
