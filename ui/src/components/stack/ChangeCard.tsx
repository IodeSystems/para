import { useState } from 'react'
import Box from '@mui/material/Box'
import Checkbox from '@mui/material/Checkbox'
import FormControlLabel from '@mui/material/FormControlLabel'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import { Details } from '@/components/Bits'
import { block, Preview, Row, type CardProps } from '@/components/stack/parts'
import {
  fieldKind,
  fieldText,
  inputObject,
  parseField,
  setInputKey,
  type FieldKind,
} from '@/lib/review'

// ChangeCard shows a change in the vault: the exact change, and para's own preview of the
// documents before and after. To edit, the owner changes the op's input in a form; the preview
// shows the agent's input.
export function ChangeCard({ it, answer, onAnswer }: CardProps) {
  const input: unknown = it.input
  const editing = answer?.decision === 'edit'
  return (
    <Box>
      <Typography variant="overline" color="text.secondary" component="div">
        The exact change
      </Typography>
      <Box component="ul" aria-label="Exact change" sx={{ m: 0, mb: 1, pl: 2.5 }}>
        {it.change.map((line) => (
          <Typography component="li" variant="body2" key={line} sx={{ overflowWrap: 'anywhere' }}>
            {line}
          </Typography>
        ))}
      </Box>
      <Row label="What" text={it.what} />
      <Row label="How" text={it.how} />
      <Row label="Impact" text={it.impact} />
      {it.preview && <Preview pv={it.preview} />}
      {editing && onAnswer ? (
        <InputForm
          agent={input}
          value={answer?.input ?? input}
          onChange={(v) => onAnswer({ ...answer, input: v })}
        />
      ) : (
        <Details summary="The op and its input">
          <Typography variant="caption" component="div">
            {it.op}
          </Typography>
          <Box
            component="pre"
            sx={{ ...block, my: 0.5, whiteSpace: 'pre' as const, overflowX: 'auto' }}
          >
            {JSON.stringify((editing ? answer?.input : input) ?? {}, null, 2)}
          </Box>
        </Details>
      )}
    </Box>
  )
}

// InputForm edits the op input as a form: one field per top-level key. Text is a text field, a
// number a number field, true or false a check box, and a list or an object JSON text. A field
// that does not read keeps the last good value.
function InputForm({
  agent,
  value,
  onChange,
}: {
  agent: unknown
  value: unknown
  onChange: (v: Record<string, unknown>) => void
}) {
  const now = inputObject(value)
  const keys = [...new Set([...Object.keys(inputObject(agent)), ...Object.keys(now)])]
  return (
    <Box role="group" aria-label="Your input for the op" sx={{ my: 1 }}>
      <Typography variant="caption" color="text.secondary" component="div" sx={{ mb: 1 }}>
        Para runs the op with this input when you submit.
      </Typography>
      <Stack spacing={1}>
        {keys.map((k) => (
          <InputField
            key={k}
            name={k}
            kind={fieldKind(k in now ? now[k] : inputObject(agent)[k])}
            value={now[k]}
            onChange={(v) => onChange(setInputKey(now, k, v))}
          />
        ))}
      </Stack>
    </Box>
  )
}

function InputField({
  name,
  kind,
  value,
  onChange,
}: {
  name: string
  kind: FieldKind
  value: unknown
  onChange: (v: unknown) => void
}) {
  const [text, setText] = useState(() => fieldText(value))
  const [bad, setBad] = useState('')
  if (kind === 'bool') {
    return (
      <FormControlLabel
        control={
          <Checkbox
            size="small"
            checked={value === true}
            onChange={(e) => onChange(e.target.checked)}
          />
        }
        label={name}
      />
    )
  }
  const json = kind === 'json'
  return (
    <TextField
      size="small"
      fullWidth
      multiline={json || kind === 'text'}
      minRows={json ? 2 : 1}
      type={kind === 'number' ? 'number' : undefined}
      label={json ? `${name} (JSON)` : name}
      value={text}
      error={bad !== ''}
      helperText={bad || undefined}
      slotProps={
        json
          ? { htmlInput: { style: { fontFamily: 'ui-monospace, monospace', fontSize: 13 } } }
          : undefined
      }
      onChange={(e) => {
        setText(e.target.value)
        const r = parseField(kind, e.target.value)
        if (!r.ok) {
          setBad(r.error)
          return
        }
        setBad('')
        onChange(r.value)
      }}
    />
  )
}
