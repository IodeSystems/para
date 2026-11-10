import { useState } from 'react'
import ButtonBase from '@mui/material/ButtonBase'
import Tooltip from '@mui/material/Tooltip'
import { copyText } from '@/lib/clipboard'

// CopyId shows a document id in small monospace text; a click copies it, so the owner can name the
// item to an agent without pasting the whole title.
export function CopyId({ id }: { id: string }) {
  const [state, setState] = useState<'idle' | 'ok' | 'fail'>('idle')
  const onClick = async () => {
    setState((await copyText(id)) ? 'ok' : 'fail')
    setTimeout(() => setState('idle'), 2000)
  }
  return (
    <Tooltip title={state === 'ok' ? 'Copied' : state === 'fail' ? 'Copy failed' : 'Copy id'}>
      <ButtonBase
        data-copy-id={id}
        onClick={() => void onClick()}
        sx={{
          fontFamily: 'monospace',
          fontSize: '0.75rem',
          color: state === 'fail' ? 'error.main' : 'text.secondary',
          px: 0.5,
          borderRadius: 0.5,
          '&:hover': { bgcolor: 'action.hover' },
        }}
      >
        {state === 'ok' ? 'copied' : id}
      </ButtonBase>
    </Tooltip>
  )
}
