import { useState } from 'react'
import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Checkbox from '@mui/material/Checkbox'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemIcon from '@mui/material/ListItemIcon'
import ListItemText from '@mui/material/ListItemText'
import MenuItem from '@mui/material/MenuItem'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import { useIdeaAdd, usePoolPick, usePools, type Pool } from '@/api/pools'
import { errorText } from '@/client/gqlClient'
import { Section, StatusChip } from '@/components/Bits'
import { DocLink } from '@/components/DocLink'
import { LinkButton } from '@/components/links'
import { useToasts } from '@/components/ToastStack'
import { effortWords } from '@/lib/next'
import { pickWords } from '@/lib/pools'

// IdeaPick is a firing pool's ideas as a checklist, best score first, with Pick and Skip this round.
// Pick brings the checked ideas back; Skip passes this round. Both are the owner's (pool_pick).
export function IdeaPick({
  pool,
  decide,
  onDone,
}: {
  pool: Pool
  decide: boolean
  onDone?: () => void
}) {
  const pick = usePoolPick()
  const toasts = useToasts()
  // The first pick ideas, by score, are checked to start with.
  const [checked, setChecked] = useState<string[]>(() =>
    pool.ideas.slice(0, pool.pick).map((i) => i.id),
  )
  const toggle = (id: string) =>
    setChecked((c) => (c.includes(id) ? c.filter((x) => x !== id) : [...c, id]))
  const run = async (skip: boolean) => {
    try {
      const out = await pick.mutateAsync(
        skip ? { pool: pool.name, skip } : { pool: pool.name, ids: checked },
      )
      onDone?.()
      toasts.push({
        text: skip
          ? `Skipped this round of ${pool.name}`
          : pickWords(out.picked.length, out.linked.length, pool.triggerTitle),
        note: out.warnings.join(' ') || undefined,
      })
    } catch (e) {
      toasts.push({ text: `Could not pick: ${errorText(e)}`, tone: 'error' })
    }
  }
  return (
    <Box data-pool-pick={pool.name}>
      <List dense disablePadding>
        {pool.ideas.map((i) => (
          <ListItem key={i.id} disableGutters data-pool-idea={i.id}>
            <ListItemIcon sx={{ minWidth: 36 }}>
              <Checkbox
                edge="start"
                size="small"
                checked={checked.includes(i.id)}
                onChange={() => toggle(i.id)}
                disabled={!decide}
                slotProps={{ input: { 'aria-label': `Pick ${i.title}` } }}
              />
            </ListItemIcon>
            <ListItemText
              primary={<DocLink id={i.id}>{i.title}</DocLink>}
              secondary={[
                i.text,
                `effort ${effortWords(i.effort)}${i.effortSet ? '' : ' (not set)'}`,
                `score ${i.score}`,
              ]
                .filter(Boolean)
                .join(' · ')}
            />
          </ListItem>
        ))}
      </List>
      {decide && (
        <Stack direction="row" spacing={1} sx={{ mt: 1 }}>
          <Button
            size="small"
            variant="contained"
            data-act="pick"
            disabled={pick.isPending || checked.length === 0}
            onClick={() => void run(false)}
          >
            Pick {checked.length || ''}
          </Button>
          <Button
            size="small"
            data-act="skip-round"
            disabled={pick.isPending}
            onClick={() => void run(true)}
          >
            Skip this round
          </Button>
        </Stack>
      )}
    </Box>
  )
}

// PoolCard is the Next card of a firing pool: one card stands for all its ideas.
export function PoolCard({
  row,
  decide,
  onDone,
}: {
  row: string
  decide: boolean
  onDone: () => void
}) {
  const q = usePools()
  const pool = (q.data ?? []).find((p) => p.row === row)
  if (q.isError) return <Alert severity="warning">{errorText(q.error)}</Alert>
  if (!pool) return null
  return (
    <Box sx={{ my: 1 }}>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 0.5 }}>
        Pool {pool.name}, {pool.trigger}
        {pool.triggerId && (
          <>
            {' '}
            (<DocLink id={pool.triggerId}>{pool.triggerTitle || pool.triggerId}</DocLink>)
          </>
        )}
        . The rest stay put off.
      </Typography>
      <IdeaPick pool={pool} decide={decide} onDone={onDone} />
    </Box>
  )
}

// AddIdea keeps an idea in a pool: a title and a sentence. An agent fills in the rest when the pool
// fires.
export function AddIdea({ pools, initial }: { pools: Pool[]; initial?: string }) {
  const add = useIdeaAdd()
  const toasts = useToasts()
  const [pool, setPool] = useState(initial ?? pools[0]?.name ?? '')
  const [title, setTitle] = useState('')
  const [text, setText] = useState('')
  const run = async () => {
    try {
      const out = await add.mutateAsync({ pool, title, text: text || undefined })
      setTitle('')
      setText('')
      toasts.push({
        text: `Kept in ${out.pool}: ${title}`,
        note: out.warnings.join(' ') || undefined,
      })
    } catch (e) {
      toasts.push({ text: `Could not add the idea: ${errorText(e)}`, tone: 'error' })
    }
  }
  return (
    <Stack
      spacing={1}
      data-idea-add
      component="form"
      onSubmit={(e) => (e.preventDefault(), void run())}
    >
      <Stack direction={{ xs: 'column', sm: 'row' }} spacing={1}>
        <TextField
          select
          size="small"
          label="Pool"
          value={pool}
          onChange={(e) => setPool(e.target.value)}
          sx={{ minWidth: 160 }}
        >
          {pools.map((p) => (
            <MenuItem key={p.name} value={p.name}>
              {p.name}
            </MenuItem>
          ))}
        </TextField>
        <TextField
          size="small"
          label="Idea"
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          sx={{ flexGrow: 1 }}
        />
      </Stack>
      <TextField
        size="small"
        label="One sentence (optional)"
        value={text}
        onChange={(e) => setText(e.target.value)}
        multiline
        minRows={1}
      />
      <Box>
        <Button
          size="small"
          variant="contained"
          type="submit"
          data-act="add-idea"
          disabled={add.isPending || !pool || !title.trim()}
        >
          Add idea to pool
        </Button>
      </Box>
    </Stack>
  )
}

// PoolsDue lists the pools that fire now on the Agenda, each with a link to pick.
export function PoolsDue() {
  const q = usePools()
  const firing = (q.data ?? []).filter((p) => p.fires)
  return (
    <Section
      title="Pick from a pool"
      count={firing.length}
      hideEmpty
      action={
        <LinkButton size="small" to="/pools">
          Pools
        </LinkButton>
      }
    >
      <List dense disablePadding>
        {firing.map((p) => (
          <ListItem key={p.name} disableGutters data-pool-due={p.name}>
            <ListItemText primary={p.title} secondary={`pool ${p.name}, ${p.trigger}`} />
            <StatusChip tone="quiet" label="pool" />
          </ListItem>
        ))}
      </List>
    </Section>
  )
}
