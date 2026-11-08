import Alert from '@mui/material/Alert'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import Stack from '@mui/material/Stack'
import Typography from '@mui/material/Typography'
import { useAgents } from '@/api/requests'
import { Section, StatusChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { DocLink } from '@/components/DocLink'
import { clientWords, lastCallWords, noListenerWords, pushChip } from '@/lib/agents'

// Agents shows the agents connected to the vault over MCP on this machine (agent_list): who each
// is, its client, when it last called a tool, whether para can wake it, and the requests it holds.
// It warns when work is queued and no agent is connected. Read-only.
export function Agents() {
  const q = useAgents()
  return (
    <DataState q={q}>
      {(out) => (
        <Section title="Agents" count={out.agents.length}>
          {out.noListener && (
            <Alert severity="warning" sx={{ mb: 1 }} data-no-listener>
              {noListenerWords(out.queuedCount)}
            </Alert>
          )}
          {out.agents.length > 0 && (
            <List aria-label="Connected agents" disablePadding>
              {out.agents.map((a) => {
                const chip = pushChip(a.push)
                return (
                  <ListItem
                    key={a.pid}
                    divider
                    disableGutters
                    data-agent={a.actor}
                    sx={{ display: 'block' }}
                  >
                    <Stack
                      direction="row"
                      spacing={1}
                      useFlexGap
                      sx={{ alignItems: 'center', flexWrap: 'wrap' }}
                    >
                      <Typography variant="body2" sx={{ fontWeight: 600 }}>
                        {a.actor}
                      </Typography>
                      <Typography variant="caption" color="text.secondary">
                        {clientWords(a)}, {lastCallWords(a.lastCallAt)}
                      </Typography>
                      <StatusChip tone={chip.tone} label={chip.label} />
                    </Stack>
                    {a.claims.length > 0 && (
                      <Stack
                        direction="row"
                        spacing={1}
                        useFlexGap
                        sx={{ mt: 0.25, flexWrap: 'wrap', alignItems: 'center' }}
                      >
                        <Typography variant="caption" color="text.secondary">
                          holds:
                        </Typography>
                        {a.claims.map((c) => (
                          <Typography key={c.id} variant="caption" data-holds={c.id}>
                            <DocLink id={c.id}>{c.title || c.id}</DocLink>
                          </Typography>
                        ))}
                      </Stack>
                    )}
                  </ListItem>
                )
              })}
            </List>
          )}
          {out.agents.length === 0 && !out.noListener && (
            <Typography variant="body2" color="text.secondary">
              No agent is connected.
            </Typography>
          )}
          <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 0.5 }}>
            Only agents on this machine are shown.
          </Typography>
        </Section>
      )}
    </DataState>
  )
}
