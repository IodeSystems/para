import { createFileRoute } from '@tanstack/react-router'
import Typography from '@mui/material/Typography'
import { useProject } from '@/api/areas'
import Stack from '@mui/material/Stack'
import { AgentLooking } from '@/components/FindContext'
import { PageTitle, Section, StateChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { DoneWhen } from '@/components/DoneWhen'
import { GoalBrief } from '@/components/GoalBrief'
import { PointerLinks } from '@/components/PointerLinks'
import { TextLink } from '@/components/links'
import { Markdown } from '@/components/Markdown'
import { nameOr } from '@/lib/names'
import { QuestionRows } from '@/components/Questions'
import { TaskRows } from '@/components/TaskRows'
import { ConceptLinks } from '@/components/AreaLinks'
import { RulesInEffect } from '@/components/RulesInEffect'

export const Route = createFileRoute('/_authed/projects/$id')({ component: ProjectPage })

function ProjectPage() {
  const { id } = Route.useParams()
  const q = useProject(id)
  return (
    <DataState q={q}>
      {(d) => (
        <>
          <PageTitle
            action={
              <Stack direction="row" spacing={0.5} sx={{ alignItems: 'center' }}>
                <AgentLooking id={id} />
                <StateChip state={d.project.status} />
              </Stack>
            }
          >
            {d.project.title || id}
          </PageTitle>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
            {d.project.owner ? `owner ${nameOr('', d.project.owner)}` : 'no owner'}
            {d.project.area && (
              <>
                {' - project '}
                <TextLink to="/areas/$id" params={{ id: d.project.area }} underline="hover">
                  {nameOr(d.project.areaName, d.project.area)}
                </TextLink>
              </>
            )}
            {d.project.concepts.length > 0 && (
              <>
                {' - concepts '}
                <ConceptLinks ids={d.project.concepts} />
              </>
            )}
          </Typography>
          <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 1 }}>
            {d.lastAligned
              ? `last checked against sources ${d.lastAligned}`
              : 'not checked against sources yet'}
          </Typography>
          <DoneWhen value={d.project.doneWhen} prompt="No done-when yet." />
          <GoalBrief brief={d.project.brief} />
          <PointerLinks links={d.project.links} />
          <RulesInEffect rules={d.rules} />
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
