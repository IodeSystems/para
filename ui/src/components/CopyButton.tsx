import { useState } from 'react'
import Button from '@mui/material/Button'
import ContentCopyIcon from '@mui/icons-material/ContentCopy'
import CheckIcon from '@mui/icons-material/Check'
import { copyText } from '@/lib/clipboard'

// Copies text produced on demand. `getText` runs on click, so the server renders (and logs the
// egress) only when a person asks.
export function CopyButton({
  getText,
  label = 'Copy',
  disabled,
}: {
  getText: () => Promise<string> | string
  label?: string
  disabled?: boolean
}) {
  const [state, setState] = useState<'idle' | 'ok' | 'fail'>('idle')
  const onClick = async () => {
    try {
      const ok = await copyText(await getText())
      setState(ok ? 'ok' : 'fail')
    } catch {
      setState('fail')
    }
    setTimeout(() => setState('idle'), 2500)
  }
  return (
    <Button
      size="small"
      variant="outlined"
      disabled={disabled}
      onClick={() => void onClick()}
      startIcon={state === 'ok' ? <CheckIcon /> : <ContentCopyIcon />}
      color={state === 'fail' ? 'error' : 'primary'}
    >
      {state === 'ok' ? 'Copied' : state === 'fail' ? 'Copy failed' : label}
    </Button>
  )
}
