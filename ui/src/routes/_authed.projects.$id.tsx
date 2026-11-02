import { createFileRoute } from '@tanstack/react-router'
import Typography from '@mui/material/Typography'
import { useProject } from '@/api/areas'
import { PageTitle, Section, StateChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { DoneWhen } from '@/components/DoneWhen'
import { PointerLinks } from '@/components/PointerLinks'
import { TextLink } from '@/components/links'
import { Markdown } from '@/components/Markdown'
import { nameOr } from '@/lib/names'
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
          <PageTitle action={<StateChip state={d.project.status} />}>
            {d.project.title || id}
          </PageTitle>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
            {d.project.owner ? `owner ${nameOr('', d.project.owner)}` : 'no owner'}
            {d.project.area && (
              <>
                {' - area '}
                <TextLink to="/areas/$id" params={{ id: d.project.area }} underline="hover">
                  {nameOr(d.project.areaName, d.project.area)}
                </TextLink>
              </>
            )}
          </Typography>
          <DoneWhen
            value={d.project.doneWhen}
            param="goaldw"
            id={d.project.id}
            prompt="Say how we know it is done"
          />
          <PointerLinks links={d.project.links} />
          <Section title="Tasks" count={d.tasks.length} hideEmpty>
            <TaskRows tasks={d.tasks} />
          </Section>
          <Section title="Questions" count={d.questions.length} hideEmpty>
            <QuestionRows list={d.questions} />
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
