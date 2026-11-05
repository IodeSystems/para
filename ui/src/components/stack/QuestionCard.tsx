import { useState } from 'react'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Checkbox from '@mui/material/Checkbox'
import FormControlLabel from '@mui/material/FormControlLabel'
import IconButton from '@mui/material/IconButton'
import Radio from '@mui/material/Radio'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import Tooltip from '@mui/material/Tooltip'
import EditOutlinedIcon from '@mui/icons-material/EditOutlined'
import { Details, StatusChip } from '@/components/Bits'
import { stringMap, type Answer } from '@/lib/dossier'
import { ClaimRows, Row, type CardProps, type Item } from '@/components/stack/parts'

// QuestionCard asks the owner to pick: one option (radio) or several (checkbox, when multi). The
// recommended option has a badge and is one click to accept. The owner can edit the words of an
// option, add an option, or answer in their own words.
export function QuestionCard({
  it,
  answer,
  onAnswer,
}: Omit<CardProps, 'it'> & {
  it: Pick<Item, 'options' | 'multi' | 'recommended' | 'optionDetails'>
}) {
  const added = answer?.addedOptions ?? []
  const edited = stringMap(answer?.editedOptions)
  const choices = answer?.choices ?? []
  const options = [...(it.options ?? []), ...added]
  const multi = it.multi === true
  const [editing, setEditing] = useState<string | null>(null)
  const [newOption, setNewOption] = useState('')
  const details = new Map((it.optionDetails ?? []).map((o) => [o.name, o]))
  const ro = !onAnswer
  // An answer takes the question back from Skip or Send to agent.
  const answerWith = (a: Answer) => {
    if (a.decision !== 'skip' && a.decision !== 'ask') return onAnswer?.(a)
    const out: Answer = { ...a, decision: null }
    delete out.refs
    onAnswer?.(out)
  }

  const toggle = (opt: string) => {
    if (!onAnswer) return
    let next: string[]
    if (multi) next = choices.includes(opt) ? choices.filter((c) => c !== opt) : [...choices, opt]
    else next = choices.includes(opt) ? [] : [opt]
    answerWith({ ...answer, choices: next })
  }
  const editOption = (opt: string, text: string) => {
    if (!onAnswer) return
    const map = { ...edited }
    if (text === '' || text === opt) delete map[opt]
    else map[opt] = text
    answerWith({ ...answer, editedOptions: Object.keys(map).length ? map : null })
  }
  const addOption = () => {
    const o = newOption.trim()
    if (!onAnswer || !o || options.includes(o)) return
    answerWith({
      ...answer,
      addedOptions: [...added, o],
      choices: multi ? [...choices, o] : [o],
    })
    setNewOption('')
  }
  const removeAdded = (o: string) => {
    if (!onAnswer) return
    answerWith({
      ...answer,
      addedOptions: added.filter((x) => x !== o),
      choices: choices.filter((c) => c !== o),
    })
  }

  return (
    <Box>
      {it.recommended && onAnswer && !choices.includes(it.recommended) && (
        <Button
          size="small"
          variant="outlined"
          color="success"
          onClick={() =>
            answerWith({
              ...answer,
              choices: multi ? [...choices, it.recommended ?? ''] : [it.recommended ?? ''],
            })
          }
          sx={{ mb: 1 }}
        >
          Accept the recommended: {edited[it.recommended] ?? it.recommended}
        </Button>
      )}
      <Stack role={multi ? 'group' : 'radiogroup'} aria-label="Options" spacing={0.25}>
        {options.map((opt) => {
          const shown = edited[opt] ?? opt
          const d = details.get(opt)
          const isAdded = added.includes(opt)
          return (
            <Box key={opt}>
              <Stack direction="row" sx={{ alignItems: 'center', flexWrap: 'wrap' }}>
                {editing === opt ? (
                  <TextField
                    size="small"
                    autoFocus
                    label={`Edit option ${opt}`}
                    defaultValue={shown}
                    onBlur={(e) => {
                      editOption(opt, e.target.value.trim())
                      setEditing(null)
                    }}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter') (e.target as HTMLInputElement).blur()
                      if (e.key === 'Escape') setEditing(null)
                    }}
                    sx={{ my: 0.5, flexGrow: 1 }}
                  />
                ) : (
                  <FormControlLabel
                    disabled={ro}
                    control={
                      multi ? (
                        <Checkbox
                          size="small"
                          checked={choices.includes(opt)}
                          onChange={() => toggle(opt)}
                        />
                      ) : (
                        <Radio
                          size="small"
                          checked={choices.includes(opt)}
                          onClick={() => toggle(opt)}
                        />
                      )
                    }
                    label={
                      <Box component="span" sx={{ overflowWrap: 'anywhere' }}>
                        {shown}
                        {shown !== opt && (
                          <Box component="span" sx={{ color: 'text.secondary', ml: 0.75 }}>
                            (was: {opt})
                          </Box>
                        )}
                      </Box>
                    }
                  />
                )}
                {opt === it.recommended && <StatusChip tone="good" label="recommended" />}
                {isAdded && <StatusChip tone="quiet" label="your option" />}
                {!ro && editing !== opt && !isAdded && (
                  <Tooltip title="Edit the words of this option">
                    <IconButton
                      size="small"
                      aria-label={`Edit the words of ${opt}`}
                      onClick={() => setEditing(opt)}
                    >
                      <EditOutlinedIcon fontSize="small" />
                    </IconButton>
                  </Tooltip>
                )}
                {!ro && isAdded && (
                  <Button size="small" onClick={() => removeAdded(opt)}>
                    Remove
                  </Button>
                )}
              </Stack>
              {d && (
                <Box sx={{ pl: 4 }}>
                  <Details summary={`Why ${opt}`}>
                    <ClaimRows label="Pro" list={d.pros ?? []} />
                    <ClaimRows label="Con" list={d.cons ?? []} />
                    {d.cost && <ClaimRows label="Cost" list={[d.cost]} />}
                    {d.benefit && <ClaimRows label="Benefit" list={[d.benefit]} />}
                    {!!d.confidence && (
                      <Row
                        label="Confidence"
                        text={`${d.confidence}%${d.confidenceBasis ? `, because ${d.confidenceBasis}` : ''}`}
                      />
                    )}
                    {(d.examples ?? []).map((x) => (
                      <Row
                        key={x.text}
                        label={`Example (${x.kind})`}
                        text={`${x.text} [${x.source}]`}
                      />
                    ))}
                  </Details>
                </Box>
              )}
            </Box>
          )
        })}
      </Stack>
      {!ro && (
        <Stack direction="row" spacing={1} sx={{ mt: 1, alignItems: 'center' }}>
          <TextField
            size="small"
            label="Add an option"
            value={newOption}
            onChange={(e) => setNewOption(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault()
                addOption()
              }
            }}
            sx={{ flexGrow: 1 }}
          />
          <Button size="small" onClick={addOption} disabled={!newOption.trim()}>
            Add option
          </Button>
        </Stack>
      )}
      {ro ? (
        <Row label="In your words" text={answer?.ownWords} />
      ) : (
        <TextField
          size="small"
          fullWidth
          multiline
          label="Answer in your own words"
          value={answer?.ownWords ?? ''}
          onChange={(e) => answerWith({ ...answer, ownWords: e.target.value })}
          sx={{ mt: 1 }}
        />
      )}
    </Box>
  )
}
