import { createFileRoute } from '@tanstack/react-router'
import Stack from '@mui/material/Stack'
import Typography from '@mui/material/Typography'
import { usePools } from '@/api/pools'
import { canDecide } from '@/client/session'
import { Empty, PageTitle, Section, StatusChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { AddIdea, IdeaPick } from '@/components/Pools'
import { nextFire } from '@/lib/pools'

export const Route = createFileRoute('/_authed/pools')({ component: Pools })

// Pools lists the idea pools of .para/pools.yaml: each one's trigger, size, and when it fires next.
// A firing pool shows its ideas to pick; the owner writes the pools file itself.
function Pools() {
  const { session } = Route.useRouteContext()
  const decide = canDecide(session)
  const q = usePools()
  return (
    <>
      <PageTitle subtitle="Ideas put off someday with a pool's tag, picked when the pool's trigger comes: a date, an item that is near, or an item that closes. You write the pools in .para/pools.yaml.">
        Pools
      </PageTitle>
      <DataState q={q}>
        {(pools) => (
          <>
            {pools.length === 0 && (
              <Empty>No pool yet. Add one to .para/pools.yaml: a name, a tag, and when.</Empty>
            )}
            {pools.length > 0 && (
              <Section title="Add an idea">
                <AddIdea pools={pools} />
              </Section>
            )}
            {pools.map((p) => (
              <Section
                key={p.name}
                title={p.fires ? p.title : p.name}
                count={p.size}
                action={<StatusChip tone={p.fires ? 'good' : 'quiet'} label={nextFire(p)} />}
              >
                <Stack spacing={0.5} data-pool={p.name}>
                  <Typography variant="body2" color="text.secondary">
                    Tag {p.tag} · pick {p.pick} · {p.trigger}
                  </Typography>
                  {p.fires ? (
                    <IdeaPick key={p.instance ?? ''} pool={p} decide={decide} />
                  ) : p.ideas.length > 0 ? (
                    <Typography variant="body2">
                      {p.ideas.map((i) => i.title).join(' · ')}
                    </Typography>
                  ) : (
                    <Empty>No idea in this pool yet.</Empty>
                  )}
                </Stack>
              </Section>
            ))}
          </>
        )}
      </DataState>
    </>
  )
}
