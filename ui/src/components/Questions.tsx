import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import Alert from '@mui/material/Alert'
import Stack from '@mui/material/Stack'
import { useQuestions, useUndefer, type QuestionScope } from '@/api/questions'
import { errorText } from '@/client/gqlClient'
import { Empty, Section, StateChip, StatusChip } from '@/components/Bits'
import { questionState } from '@/lib/answer'
import { nameList } from '@/lib/names'
import { nameOr } from '@/lib/names'
import { DocLink } from '@/components/DocLink'
import { LinkButton } from '@/components/links'
import { RowMenu } from '@/components/RowMenu'
import { findContextItem } from '@/components/FindContext'

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
  const undefer = useUndefer()
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
          </Stack>
          <Stack direction="row" spacing={0.5} useFlexGap sx={{ flexWrap: 'wrap', mt: 0.5 }}>
            <StatusChip tone="quiet" label={x.kind} />
            {x.agentLooking && <StatusChip tone="good" label="Agent looking" />}
            {x.deferred && (
              <StatusChip tone="quiet" label={`Later: back ${laterWords(x.deferUntil)}`} />
            )}
            {(x.hasProposal || x.waitingOn.length > 0) && <StateChip {...questionState(x)} />}
            {x.blocking > 0 && <StatusChip tone="attn" label={`blocks ${x.blocking}`} />}
            {x.hasProposal && (
              <LinkButton size="small" variant="outlined" to="/review">
                Check answer
              </LinkButton>
            )}
            <LinkButton
              size="small"
              variant="outlined"
              to="."
              search={(p) => ({ ...p, answer: x.id })}
            >
              Answer
            </LinkButton>
            <RowMenu
              label={`More actions for ${x.title || x.id}`}
              items={[
                ...(x.rfx
                  ? []
                  : [
                      {
                        label: 'Ask someone',
                        hint: 'Send this question to a person as an ask',
                        search: { ask: x.id },
                      },
                    ]),
                findContextItem(x.id, x.agentLooking),
                x.deferred
                  ? {
                      label: 'Not later',
                      hint: 'Bring this back to Today',
                      onSelect: () => undefer.mutate([x.id]),
                    }
                  : {
                      label: 'Later',
                      hint: 'Put this off. It still blocks what depends on it',
                      search: { later: x.id },
                    },
                ...(x.blocks.length > 0 || x.waitingOn.length > 0
                  ? [
                      {
                        label: 'Cut a dependency',
                        hint: 'Let the work go ahead without this, and say why',
                        search: { cut: x.id },
                      },
                    ]
                  : []),
                {
                  label: 'Drop',
                  hint: 'Close this question without answering it',
                  search: { drop: x.id },
                },
              ]}
              always
            />
          </Stack>
        </ListItem>
      ))}
    </List>
  )
}
