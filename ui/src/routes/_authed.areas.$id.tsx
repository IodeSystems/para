import { createFileRoute } from '@tanstack/react-router'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import { useArea } from '@/api/areas'
import { PageTitle, Section, StateChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { DocLink } from '@/components/DocLink'
import { IncidentRows } from '@/components/IncidentRows'
import { PointerLinks } from '@/components/PointerLinks'
import { TextLink } from '@/components/links'
import { Markdown } from '@/components/Markdown'
import { nameOr } from '@/lib/names'
import { QuestionRows } from '@/components/Questions'
import { TaskRows } from '@/components/TaskRows'

export const Route = createFileRoute('/_authed/areas/$id')({ component: AreaPage })

function AreaPage() {
  const { id } = Route.useParams()
  const q = useArea(id)
  return (
    <DataState q={q}>
      {(d) => (
        <>
          <PageTitle action={<StateChip state={d.area.status} />}>{d.area.title || id}</PageTitle>
          <PointerLinks links={d.area.links} />
          <Section title="Projects" count={d.projects.length} hideEmpty>
            {
              <List dense disablePadding>
                {d.projects.map((p) => (
                  <ListItem key={p.id} disableGutters>
                    <ListItemText
                      primary={
                        <TextLink to="/projects/$id" params={{ id: p.id }} underline="hover">
                          {p.title || p.id}
                        </TextLink>
                      }
                      secondary={p.owner ? `owner ${nameOr('', p.owner)}` : undefined}
                    />
                    <StateChip state={p.status} />
                  </ListItem>
                ))}
              </List>
            }
          </Section>
          <Section title="Tasks" count={d.tasks.length} hideEmpty>
            <TaskRows tasks={d.tasks} showProject />
          </Section>
          <Section title="Recurring" count={d.recurring.length} hideEmpty>
            <TaskRows tasks={d.recurring} />
          </Section>
          <Section title="Questions" count={d.questions.length} hideEmpty>
            <QuestionRows list={d.questions} />
          </Section>
          <Section title="Incidents" count={d.incidents.length} hideEmpty>
            <IncidentRows list={d.incidents} />
          </Section>
          {d.notes.length > 0 && (
            <Section title="Notes" count={d.notes.length}>
              <List dense disablePadding>
                {d.notes.map((n) => (
                  <ListItem key={n.id} disableGutters>
                    <ListItemText primary={<DocLink id={n.id}>{n.title || n.id}</DocLink>} />
                  </ListItem>
                ))}
              </List>
            </Section>
          )}
          {d.body.trim() !== '' && (
            <Section title="About">
              <Markdown source={d.body} />
            </Section>
          )}
        </>
      )}
    </DataState>
  )
}
