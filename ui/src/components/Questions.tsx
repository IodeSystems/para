import { useState } from 'react'
import Button from '@mui/material/Button'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import Alert from '@mui/material/Alert'
import Stack from '@mui/material/Stack'
import { useQuestions, type QuestionScope } from '@/api/questions'
import { useClosedList } from '@/api/verbs'
import { errorText } from '@/client/gqlClient'
import { Empty, Section, StateChip, StatusChip } from '@/components/Bits'
import { questionState } from '@/lib/answer'
import { laterLabel, nameList } from '@/lib/names'
import { nameOr } from '@/lib/names'
import { DocLink } from '@/components/DocLink'
import { LinkButton } from '@/components/links'
import { ItemActions } from '@/components/AgendaActions'

// Open questions in a scope, in the order the server gives: the ones that block most come first.
export function QuestionsSection({
  scope,
  title = 'Questions',
  hideEmpty,
}: {
  scope?: QuestionScope
  title?: string
  hideEmpty?: boolean
}) {
  const q = useQuestions(scope)
  const list = q.data ?? []
  return (
    <Section
      title={title}
      count={q.isError ? undefined : list.length}
      hideEmpty={hideEmpty}
      action={
        <LinkButton size="small" to="/questions">
          All
        </LinkButton>
      }
    >
      {q.isError ? (
        <Alert severity="error">{errorText(q.error)}</Alert>
      ) : list.length === 0 ? (
        <Empty>No open question.</Empty>
      ) : (
        <QuestionRows list={list} />
      )}
    </Section>
  )
}

// scopeWords says what a question is about: "about Launch", or "about the whole vault".
export function scopeWords(type: string, name: string | null | undefined, id: string): string {
  if (type === 'root' || id === 'root') return 'about the whole vault'
  return `${type} ${nameOr(name, id)}`
}

type Rows = NonNullable<ReturnType<typeof useQuestions>['data']>

// laterWords says when a put-off item is back.
export function laterWords(until: string | null | undefined): string {
  return !until || until === 'someday' ? 'someday' : `on ${until}`
}

export function QuestionRows({ list }: { list: Rows }) {
  return (
    <List dense disablePadding>
      {list.map((x) => (
        <ListItem key={x.id} disableGutters divider sx={{ display: 'block' }}>
          <Stack direction="row" sx={{ alignItems: 'flex-start' }}>
            <ListItemText
              primary={<DocLink id={x.id}>{x.title || x.id}</DocLink>}
              secondary={[
                scopeWords(x.scopeType, x.scopeName, x.scope),
                x.recommended ? `recommended: ${x.recommended}` : '',
                x.rfx ? 'already asked' : '',
                !x.hasProposal && x.waitingOn.length > 0
                  ? `waiting on ${nameList(x.waitingOn, null).join(', ')}`
                  : '',
                x.rejectedReason ? `Rejected: ${x.rejectedReason}` : '',
              ]
                .filter(Boolean)
                .join(' - ')}
              slotProps={{ secondary: { sx: { overflowWrap: 'anywhere' } } }}
            />
            <ItemActions
              x={{ id: x.id, title: x.title, kind: 'question' }}
              deferred={!!x.deferred}
              open={false}
            />
          </Stack>
          <Stack direction="row" spacing={0.5} useFlexGap sx={{ flexWrap: 'wrap', mt: 0.5 }}>
            <StatusChip tone="quiet" label={x.kind} />
            {x.agentLooking && <StatusChip tone="good" label="Agent looking" />}
            {x.deferred && (
              <StatusChip
                tone="quiet"
                label={laterLabel(x.deferUntil, x.waitsOn, x.waitsOnNames, x.laterText)}
              />
            )}
            {!x.deferred && x.readyWhy && <StatusChip tone="good" label={x.readyWhy} />}
            {(x.hasProposal || x.waitingOn.length > 0) && <StateChip {...questionState(x)} />}
            {x.blocking > 0 && <StatusChip tone="attn" label={`blocks ${x.blocking}`} />}
          </Stack>
        </ListItem>
      ))}
    </List>
  )
}

// DoneQuestions lists closed questions (answered or dropped) once the owner asks to see them, each
// with Reopen.
export function DoneQuestions() {
  const [show, setShow] = useState(false)
  const q = useClosedList('question', show)
  const rows = q.data ?? []
  return (
    <>
      <Button size="small" color="inherit" aria-pressed={show} onClick={() => setShow((v) => !v)}>
        {show ? 'Hide done' : 'Show done'}
      </Button>
      {show && (
        <Section title="Done" count={q.isError ? undefined : rows.length}>
          {q.isError ? (
            <Alert severity="error">{errorText(q.error)}</Alert>
          ) : rows.length === 0 ? (
            <Empty>No closed question.</Empty>
          ) : (
            <List dense disablePadding>
              {rows.map((x) => (
                <ListItem key={x.id} disableGutters divider>
                  <ListItemText primary={<DocLink id={x.id}>{x.title || x.id}</DocLink>} />
                  {x.status && <StateChip state={x.status} />}
                  <ItemActions
                    x={{ id: x.id, title: x.title, kind: 'question' }}
                    closed
                    open={false}
                  />
                </ListItem>
              ))}
            </List>
          )}
        </Section>
      )}
    </>
  )
}
