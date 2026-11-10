import { useState } from 'react'
import { useNavigate } from '@tanstack/react-router'
import Button from '@mui/material/Button'
import Chip from '@mui/material/Chip'
import Collapse from '@mui/material/Collapse'
import Stack from '@mui/material/Stack'
import Typography from '@mui/material/Typography'
import { useIoMade } from '@/api/io'
import { openTarget } from '@/lib/agenda'
import { humanTime } from '@/lib/when'

type Made = NonNullable<ReturnType<typeof useIoMade>['data']>['rows'][number]

// actionWords is one row's actions in one word: created, edited, closed, linked, or the op's name.
export const actionWords = (r: Made): string => r.actions.join(' + ')

// IoMade shows what an incoming item made: one chip per document (action and title, a link to the
// document), and What it did, which lists every row with who did it, when, and why.
export function IoMade({ id }: { id: string }) {
  const q = useIoMade(id)
  const navigate = useNavigate()
  const [open, setOpen] = useState(false)
  const rows = q.data?.rows ?? []
  if (q.isLoading) return null
  if (q.isError) {
    return (
      <Typography variant="caption" color="error" sx={{ display: 'block' }}>
        Could not load what it made.
      </Typography>
    )
  }
  if (rows.length === 0) return null
  const go = (r: Made) => {
    const t = openTarget(r.type)
    if (t === 'rfx') void navigate({ to: '/rfx/$id', params: { id: r.doc } })
    else if (t === 'io') void navigate({ to: '/io/$id', params: { id: r.doc } })
    else if (t === 'incident') void navigate({ to: '/incidents/$id', params: { id: r.doc } })
    else void navigate({ to: '/docs/$id', params: { id: r.doc } })
  }
  return (
    <div style={{ width: '100%' }}>
      <Stack direction="row" spacing={0.5} useFlexGap sx={{ flexWrap: 'wrap', mt: 0.5 }}>
        {rows.map((r) => (
          <Chip
            key={`${r.doc}:${r.actions.join(',')}`}
            size="small"
            variant="outlined"
            data-made={r.doc}
            label={`${actionWords(r)}: ${r.title || r.doc}`}
            onClick={() => go(r)}
          />
        ))}
        <Button size="small" data-act="what-it-did" onClick={() => setOpen((o) => !o)}>
          {open ? 'Hide what it did' : 'What it did'}
        </Button>
      </Stack>
      <Collapse in={open} unmountOnExit>
        <Stack component="ul" spacing={0.5} sx={{ m: 0, pl: 2, pb: 1 }}>
          {rows.map((r) => (
            <Typography component="li" variant="body2" key={`${r.doc}:${r.actions.join(',')}`}>
              {[actionWords(r), r.title || r.doc, r.actor, r.at && humanTime(r.at)]
                .filter(Boolean)
                .join(' - ')}
              {r.why ? `. Why: ${r.why}` : ''}
            </Typography>
          ))}
        </Stack>
      </Collapse>
    </div>
  )
}
