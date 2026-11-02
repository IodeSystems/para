import { createFileRoute } from '@tanstack/react-router'
import Chip from '@mui/material/Chip'
import Typography from '@mui/material/Typography'
import { useProject } from '@/api/areas'
import { Empty, PageTitle, Section } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { PointerLinks } from '@/components/PointerLinks'
import { TextLink } from '@/components/links'
import { Markdown } from '@/components/Markdown'
import { QuestionRows } from '@/components/Questions'
import { TaskRows } from '@/components/TaskRows'

export const Route = createFileRoute('/_authed/projects/$id')({ component: ProjectPage })

function ProjectPage() {
  const { id } = Route.useParams()
  const q = useProject(id)
  return (
    <DataState q={q}>
      {(d) => (
        <>
          <PageTitle action={<Chip size="small" variant="outlined" label={d.project.status} />}>
            {d.project.title || id}
          </PageTitle>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
            {d.project.owner ? `owner ${d.project.owner}` : 'no owner'}
            {d.project.area && (
              <>
                {' - area '}
                <TextLink to="/areas/$id" params={{ id: d.project.area }} underline="hover">
                  {d.project.area}
                </TextLink>
              </>
            )}
          </Typography>
          <PointerLinks links={d.project.links} />
          <Section title="Tasks" count={d.tasks.length}>
            <TaskRows tasks={d.tasks} />
          </Section>
          <Section title="Questions" count={d.questions.length}>
            {d.questions.length === 0 ? (
              <Empty>No open question.</Empty>
            ) : (
              <QuestionRows list={d.questions} />
            )}
          </Section>
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
