import { createFileRoute } from '@tanstack/react-router'
import Tab from '@mui/material/Tab'
import Tabs from '@mui/material/Tabs'
import { PageTitle } from '@/components/Bits'
import { ProposalsTab } from '@/components/output/ProposalsTab'
import { SENT_MEDIUMS, SentTab } from '@/components/output/SentTab'

type OutputTab = 'proposals' | 'sent'

export const Route = createFileRoute('/_authed/output')({
  validateSearch: (
    s: Record<string, unknown>,
  ): { tab?: OutputTab; medium?: string; to?: string; days?: number } => ({
    tab: s.tab === 'sent' ? 'sent' : undefined,
    medium:
      typeof s.medium === 'string' && SENT_MEDIUMS.includes(s.medium) ? s.medium : undefined,
    to: typeof s.to === 'string' && s.to ? s.to : undefined,
    days: [1, 7, 30].includes(Number(s.days)) ? Number(s.days) : undefined,
  }),
  component: OutputPage,
})

function OutputPage() {
  const { tab, medium, to, days } = Route.useSearch()
  const navigate = Route.useNavigate()
  return (
    <>
      <PageTitle subtitle="What waits for your approval to go out, and a history of what went out and what came of it.">
        Output
      </PageTitle>
      <Tabs
        value={tab ?? 'proposals'}
        onChange={(_e, v: OutputTab) =>
          void navigate({
            search: (p) => ({ ...p, tab: v === 'proposals' ? undefined : v }),
            replace: true,
          })
        }
        sx={{ mb: 2 }}
      >
        <Tab value="proposals" label="Proposals" />
        <Tab value="sent" label="Sent" />
      </Tabs>
      {tab === 'sent' ? (
        <SentTab
          search={{ medium, to, days }}
          onSearch={(p) =>
            void navigate({ search: (prev) => ({ ...prev, ...p }), replace: true })
          }
        />
      ) : (
        <ProposalsTab />
      )}
    </>
  )
}
