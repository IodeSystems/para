import { useEffect, useId, useState } from 'react'
import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import CircularProgress from '@mui/material/CircularProgress'
import Typography from '@mui/material/Typography'

// Renders one mermaid diagram. Mermaid loads on demand: it is large, and most screens have no
// diagram. securityLevel 'strict' makes mermaid encode HTML in labels and disable click handlers,
// so a vault document cannot script the page through a diagram.
let mermaidReady: Promise<typeof import('mermaid').default> | null = null

function loadMermaid() {
  mermaidReady ??= import('mermaid').then((m) => {
    m.default.initialize({
      startOnLoad: false,
      securityLevel: 'strict',
      theme: window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'default',
    })
    return m.default
  })
  return mermaidReady
}

type Done = { source: string } & ({ kind: 'ok'; svg: string } | { kind: 'err'; text: string })

export function Mermaid({ source, label }: { source: string; label: string }) {
  const id = `mmd-${useId().replace(/[^a-zA-Z0-9]/g, '')}`
  // The result is kept with the source it was drawn from: a different source is still drawing.
  const [done, setDone] = useState<Done | null>(null)
  useEffect(() => {
    let live = true
    loadMermaid()
      .then((m) => m.render(id, source))
      .then((r) => live && setDone({ source, kind: 'ok', svg: r.svg }))
      .catch(
        (e: unknown) =>
          live &&
          setDone({ source, kind: 'err', text: e instanceof Error ? e.message : 'diagram failed' }),
      )
    return () => {
      live = false
    }
  }, [id, source])

  const state = done?.source === source ? done : null
  if (!state) return <CircularProgress size={20} aria-label="Drawing diagram" />
  if (state.kind === 'err') {
    return (
      <Alert severity="warning" sx={{ my: 1 }}>
        <Typography variant="body2">The diagram did not draw: {state.text}</Typography>
        <Box component="pre" sx={{ m: 0, mt: 1, overflowX: 'auto', fontSize: 12 }}>
          {source}
        </Box>
      </Alert>
    )
  }
  // The SVG comes from mermaid in strict mode. role=img with a label gives it a name.
  return (
    <Box
      role="img"
      aria-label={label}
      sx={{ overflowX: 'auto', my: 1, '& svg': { maxWidth: 'none', height: 'auto' } }}
      dangerouslySetInnerHTML={{ __html: state.svg }}
    />
  )
}
