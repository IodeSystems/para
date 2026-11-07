import { useState, type ReactNode } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useRouteContext } from '@tanstack/react-router'
import Button from '@mui/material/Button'
import Link from '@mui/material/Link'
import Stack from '@mui/material/Stack'
import Typography from '@mui/material/Typography'
import { useItemContext, type ItemContextData } from '@/api/context'
import { investigate } from '@/api/verbs'
import { errorText } from '@/client/gqlClient'
import { canDecide } from '@/client/session'
import { Section, StatusChip } from '@/components/Bits'
import { Dependencies } from '@/components/Dependencies'
import { DocLink } from '@/components/DocLink'
import { TextLink } from '@/components/links'
import { forGroups, roleLabel } from '@/lib/context'

// ItemContext is the block at the top of an item page that says what the item is for: where it
// came from (the message or conversation it was filed from, with a link to the original), who it is
// for, and why the owner does it (the goal it leads to and how, who it is owed to, the area it keeps
// up). An item with no reason says so; an agent looks into it, or Investigate asks one. Under it,
// Waits on and Blocks (Dependencies).
export function ItemContext({ id }: { id: string }) {
  const q = useItemContext(id)
  if (!q.data) return null
  return (
    <>
      <ContextBody c={q.data} />
      <Dependencies c={q.data} />
    </>
  )
}

function ContextBody({ c }: { c: ItemContextData }) {
  const groups = forGroups(c.for)
  return (
    <Section title="Context">
      <Stack spacing={0.75} data-context={c.id}>
        {c.cameFrom.length > 0 && (
          <Line label="Came from">
            {c.cameFrom.map((f, i) => (
              <span key={`${f.kind}-${f.id}`} data-came-from={f.kind}>
                {i > 0 && '; '}
                {f.kind === 'io' ? (
                  <TextLink to="/io/$id" params={{ id: f.id }} underline="hover">
                    {f.text}
                  </TextLink>
                ) : (
                  <DocLink id={f.id}>{f.text}</DocLink>
                )}
                {f.title && f.kind === 'io' && <>: {f.title}</>}
                {f.url && (
                  <>
                    {' '}
                    (
                    <Link href={f.url} target="_blank" rel="noreferrer noopener">
                      open the original
                    </Link>
                    )
                  </>
                )}
              </span>
            ))}
          </Line>
        )}
        {groups.map((g) => (
          <Line key={g.role} label={roleLabel(g.role)} data={`for-${g.role}`}>
            {g.parties.map((p, i) => (
              <span key={`${p.id ?? ''}-${p.name}`}>
                {i > 0 && ', '}
                {p.id ? <DocLink id={p.id}>{p.name}</DocLink> : p.name}
              </span>
            ))}
          </Line>
        ))}
        <Line label="Why">
          {c.reasons.map((r, i) => (
            <span key={`${r.kind}-${r.ref ?? ''}-${r.title ?? ''}`} data-reason={r.kind}>
              {i > 0 && '; '}
              {r.kind === 'none' ? (
                <NoReason id={c.id} text={r.text} looking={c.looking} />
              ) : r.ref ? (
                <DocLink id={r.ref}>{r.text}</DocLink>
              ) : (
                r.text
              )}
            </span>
          ))}
        </Line>
      </Stack>
    </Section>
  )
}

function Line({ label, data, children }: { label: string; data?: string; children: ReactNode }) {
  return (
    <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }} data-context-line={data}>
      <strong>{label}:</strong> {children}
    </Typography>
  )
}

// NoReason is the line of an item with no reason: Agent looking while the agent work is open,
// else Investigate (only with review:decide; a read-only session sees the line alone).
function NoReason({ id, text, looking }: { id: string; text: string; looking?: string | null }) {
  const { session } = useRouteContext({ from: '/_authed' })
  const qc = useQueryClient()
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  return (
    <Stack
      component="span"
      direction="row"
      spacing={1}
      sx={{ display: 'inline-flex', alignItems: 'center', flexWrap: 'wrap' }}
    >
      <span>{text}.</span>
      {looking ? (
        <StatusChip tone="good" label="Agent looking" />
      ) : !canDecide(session) ? null : (
        <Button
          size="small"
          disabled={busy}
          onClick={() => {
            setBusy(true)
            setErr('')
            investigate({ id })
              .then(() => qc.invalidateQueries())
              .catch((e: unknown) => setErr(errorText(e)))
              .finally(() => setBusy(false))
          }}
        >
          Investigate
        </Button>
      )}
      {err && (
        <Typography component="span" variant="caption" color="error">
          {err}
        </Typography>
      )}
    </Stack>
  )
}
