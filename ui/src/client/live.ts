import { useEffect } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'
import { subscribeRaw, type SubResponse } from '@/gen/live-runtime'
import type { VaultEvent } from '@/gen/live-types'

// Live refresh. The server pushes ids over /ws (gwag relay, signed caps). A push only invalidates
// queries: the screens refetch through gqlClient, so a missed push costs at most one poll
// (60 s, AppProviders). Without a socket (plain HTTP behind a proxy that drops upgrades, older
// Safari with connect-src 'self') the polling alone keeps screens current.

const SubscribeDoc = graphql(/* GraphQL */ `
  mutation LiveSubscribe {
    para {
      v1 {
        uiLiveSubscribe {
          token
          channel
          exp
        }
      }
    }
  }
`)

export async function mintVaultCap(): Promise<SubResponse<VaultEvent>> {
  const r = need((await gqlClient.request(SubscribeDoc)).para.v1.uiLiveSubscribe, 'cap')
  return { token: r.token, channel: r.channel, exp: r.exp, __event: undefined }
}

// useLiveRefresh subscribes for the life of the signed-in shell.
export function useLiveRefresh(): void {
  const qc = useQueryClient()
  useEffect(() => {
    if (typeof WebSocket === 'undefined') return
    return subscribeRaw(mintVaultCap, () => {
      void qc.invalidateQueries()
    })
  }, [qc])
}
