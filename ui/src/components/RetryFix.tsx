import { useState } from 'react'
import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Checkbox from '@mui/material/Checkbox'
import FormControlLabel from '@mui/material/FormControlLabel'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import { useDoc } from '@/api/doc'
import { useRetryDecide, type RetryDecision } from '@/api/retry'
import { errorText } from '@/client/gqlClient'
import { DiffBlock, block } from '@/components/stack/parts'
import { lineDiff, parseInput, readRetryFix } from '@/lib/retry'

// RetryFix is the owner card of an agent's refused write: the lint findings, the input that was
// refused, the proposed fix as a diff, and what the owner does with it. Accept runs the op with
// the proposal. Edit saves the owner's fix as the new proposal and leaves it with the owner;
// "and approve" makes the same click run it. Drop closes it, nothing written. Reject sends it
// back to the agent with the owner's notes. It renders nothing for a request that is not a
// retry-write with a proposal.
export function RetryFix({ id, decide }: { id: string; decide: boolean }) {
  const doc = useDoc(id)
  const fix = doc.data ? readRetryFix(doc.data.body ?? '') : null
  if (!fix) return null
  return <RetryCard key={doc.data?.rev} id={id} fix={fix} decide={decide} />
}

function RetryCard({
  id,
  fix,
  decide,
}: {
  id: string
  fix: NonNullable<ReturnType<typeof readRetryFix>>
  decide: boolean
}) {
  const mut = useRetryDecide()
  const [text, setText] = useState(fix.proposed)
  const [approve, setApprove] = useState(false)
  const [notes, setNotes] = useState('')
  const [rejecting, setRejecting] = useState(false)
  const [error, setError] = useState('')
  const edited = text !== fix.proposed
  const run = (d: Omit<RetryDecision, 'request'>) => {
    setError('')
    mut.mutate({ request: id, ...d }, { onError: (e) => setError(errorText(e)) })
  }
  const save = () => {
    const p = parseInput(text)
    if (p.error) return setError(p.error)
    run({ action: 'edit', input: p.input, approve })
  }
  return (
    <Box data-retry-fix={id} sx={{ my: 1 }}>
      <Typography variant="subtitle2">Agent write refused: {fix.op}</Typography>
      {fix.findings && (
        <Box sx={{ ...block, color: 'error.main' }} aria-label="Lint findings">
          {fix.findings}
        </Box>
      )}
      <Typography variant="caption" color="text.secondary">
        Refused input to proposed fix
      </Typography>
      <DiffBlock lines={lineDiff(fix.refused, fix.proposed)} label="Refused input to proposed fix" />
      {decide && (
        <>
          <TextField
            multiline
            fullWidth
            minRows={3}
            size="small"
            label="Fix to run (JSON)"
            value={text}
            onChange={(e) => setText(e.target.value)}
            slotProps={{ htmlInput: { 'data-retry-input': '' } }}
            sx={{ mt: 1 }}
          />
          {edited && (
            <DiffBlock lines={lineDiff(fix.proposed, text)} label="Your fix" />
          )}
          {rejecting && (
            <TextField
              fullWidth
              size="small"
              label="Notes to the agent"
              value={notes}
              onChange={(e) => setNotes(e.target.value)}
              sx={{ mt: 1 }}
              slotProps={{ htmlInput: { 'data-retry-notes': '' } }}
            />
          )}
          <Stack
            direction="row"
            spacing={0.5}
            useFlexGap
            sx={{ flexWrap: 'wrap', alignItems: 'center', mt: 1 }}
          >
            <Button
              size="small"
              variant="contained"
              data-act="retry-accept"
              disabled={mut.isPending || edited}
              onClick={() => run({ action: 'accept' })}
            >
              Accept the fix
            </Button>
            <Button
              size="small"
              data-act="retry-edit"
              disabled={mut.isPending || !edited}
              onClick={save}
            >
              Save edit
            </Button>
            <FormControlLabel
              sx={{ mr: 1 }}
              control={
                <Checkbox
                  size="small"
                  checked={approve}
                  onChange={(e) => setApprove(e.target.checked)}
                  slotProps={{ input: { 'data-retry-approve': '' } as object }}
                />
              }
              label="and approve"
            />
            <Button
              size="small"
              data-act="retry-drop"
              disabled={mut.isPending}
              onClick={() => run({ action: 'drop' })}
            >
              Drop
            </Button>
            <Button
              size="small"
              data-act="retry-reject"
              disabled={mut.isPending || (rejecting && notes.trim() === '')}
              onClick={() => (rejecting ? run({ action: 'reject', notes }) : setRejecting(true))}
            >
              {rejecting ? 'Send back with notes' : 'Reject with notes'}
            </Button>
          </Stack>
          {error && (
            <Alert severity="error" sx={{ mt: 1 }}>
              {error}
            </Alert>
          )}
        </>
      )}
    </Box>
  )
}
