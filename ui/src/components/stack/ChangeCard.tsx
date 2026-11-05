import { useState } from 'react'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Checkbox from '@mui/material/Checkbox'
import FormControlLabel from '@mui/material/FormControlLabel'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import { Details } from '@/components/Bits'
import { Markdown } from '@/components/Markdown'
import { block, Preview, Row, type CardProps } from '@/components/stack/parts'
import { parseChange, type ChangeField } from '@/lib/change'
import { withDecision } from '@/lib/review'
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
      <ChangeFields
        lines={it.change}
        editing={editing}
        onEdit={
          onAnswer ? () => onAnswer(withDecision(answer, 'edit', { input: it.input })) : undefined
        }
      />
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

// COLLAPSED is the height of a long field before Show all: about 8 lines of body text.
const COLLAPSED = '11.5em'

// ChangeFields lists the fields of the exact change. A short field is one compact line, label and
// value. A long one (a message, a note body) is drawn as markdown, collapsed to about 8 lines with
// Show all; Edit opens the editable form of the op's input below.
function ChangeFields({
  lines,
  editing,
  onEdit,
}: {
  lines: string[]
  editing: boolean
  onEdit?: () => void
}) {
  const fields = parseChange(lines)
  const short = fields.filter((f) => !f.long)
  const long = fields.filter((f) => f.long)
  return (
    <Box sx={{ mb: 1 }}>
      {short.length > 0 && (
        <Box component="ul" aria-label="Exact change" sx={{ m: 0, pl: 2.5 }}>
          {short.map((f) => (
            <Typography
              component="li"
              variant="body2"
              key={f.label + f.value}
              sx={{ overflowWrap: 'anywhere' }}
            >
              {f.label ? <strong>{f.label}: </strong> : null}
              {f.value}
            </Typography>
          ))}
        </Box>
      )}
      {long.map((f) => (
        <LongField key={f.label + f.value.slice(0, 40)} f={f} editing={editing} onEdit={onEdit} />
      ))}
    </Box>
  )
}

function LongField({
  f,
  editing,
  onEdit,
}: {
  f: ChangeField
  editing: boolean
  onEdit?: () => void
}) {
  const [all, setAll] = useState(false)
  return (
    <Box sx={{ mt: 1 }} data-change-field={f.label || undefined}>
      <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
        <Typography variant="caption" color="text.secondary" sx={{ flexGrow: 1 }}>
          {f.label}
        </Typography>
        {onEdit && !editing && (
          <Button size="small" onClick={onEdit}>
            Edit
          </Button>
        )}
      </Stack>
      <Box
        data-collapsed={!all}
        sx={{
          position: 'relative',
          maxHeight: all ? 'none' : COLLAPSED,
          overflow: 'hidden',
          p: 1,
          bgcolor: 'action.hover',
          borderRadius: 1,
          '& > div > :first-child': { mt: 0 },
          '& > div > :last-child': { mb: 0 },
          '&[data-collapsed=true]::after': {
            content: '""',
            position: 'absolute',
            insetInline: 0,
            bottom: 0,
            height: '2.5em',
            background: (t) => `linear-gradient(transparent, ${t.palette.background.paper})`,
            pointerEvents: 'none',
          },
        }}
      >
        <Markdown source={f.value} />
      </Box>
      <Button size="small" aria-expanded={all} onClick={() => setAll((a) => !a)}>
        {all ? 'Show less' : 'Show all'}
      </Button>
    </Box>
  )
}
