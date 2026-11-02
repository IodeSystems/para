import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import Alert from '@mui/material/Alert'
import Stack from '@mui/material/Stack'
import { useQuestions, type QuestionScope } from '@/api/questions'
import { errorText } from '@/client/gqlClient'
import { Empty, Section, StatusChip } from '@/components/Bits'
import { nameOr } from '@/lib/names'
import { DocLink } from '@/components/DocLink'
import { LinkButton } from '@/components/links'

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

export function QuestionRows({ list }: { list: Rows }) {
  return (
    <List dense disablePadding>
      {list.map((x) => (
        <ListItem key={x.id} disableGutters divider sx={{ display: 'block' }}>
          <ListItemText
            primary={<DocLink id={x.id}>{x.title || x.id}</DocLink>}
            secondary={[
              scopeWords(x.scopeType, x.scopeName, x.scope),
              x.recommended ? `recommended: ${x.recommended}` : '',
              x.rfx ? 'already asked' : '',
            ]
              .filter(Boolean)
              .join(' - ')}
            slotProps={{ secondary: { sx: { overflowWrap: 'anywhere' } } }}
          />
          <Stack direction="row" spacing={0.5} useFlexGap sx={{ flexWrap: 'wrap', mt: 0.5 }}>
            <StatusChip tone="quiet" label={x.kind} />
            {x.blocking > 0 && <StatusChip tone="attn" label={`blocks ${x.blocking}`} />}
            <LinkButton size="small" to="." search={(p) => ({ ...p, answer: x.id })}>
              Answer
            </LinkButton>
            {!x.rfx && (
              <LinkButton size="small" to="." search={(p) => ({ ...p, ask: x.id })}>
                Ask someone
              </LinkButton>
            )}
            <LinkButton size="small" color="inherit" to="." search={(p) => ({ ...p, drop: x.id })}>
              Drop
            </LinkButton>
          </Stack>
        </ListItem>
      ))}
    </List>
  )
}
