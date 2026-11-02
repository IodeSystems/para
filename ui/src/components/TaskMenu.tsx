import { RowMenu } from '@/components/RowMenu'
import type { RowMenuItem } from '@/components/RowMenu'
import { findContextItem } from '@/components/FindContext'
import { useUndefer } from '@/api/questions'
import { useTaskStart } from '@/api/tasks'
import { useLooking, useWorking } from '@/api/requests'

// TaskMenu is the action menu of an open task, wherever it shows: Resolve, Start, Block, Assign,
// Give to agent, Investigate, Later, Cut. extra adds entries that only some lists have.
export function TaskMenu({
  id,
  title,
  status,
  deferred,
  extra,
  always,
}: {
  id: string
  title: string
  status?: string
  deferred?: boolean
  extra?: RowMenuItem[]
  always?: boolean
}) {
  const looking = useLooking()
  const working = useWorking()
  const undefer = useUndefer()
  const start = useTaskStart()
  const items: RowMenuItem[] = [
    {
      label: 'Resolve',
      hint: 'Close this task: done, partly done, dropped, a duplicate, or replaced',
      search: { resolve: id },
    },
    ...(status === 'doing'
      ? []
      : [
          {
            label: 'Start',
            hint: 'Mark this task as being worked on',
            onSelect: () => start.mutate({ id }),
          },
        ]),
    { label: 'Block', hint: 'Say what holds this up', search: { block: id } },
    {
      label: 'Assign',
      hint: 'Hand this to yourself, a person, or agent:name',
      search: { assign: id },
    },
    {
      label: 'Give to agent',
      hint: working.has(id)
        ? 'An agent is working on it already. A hint goes into its work'
        : 'Queue agent work for this task; you check what it reports',
      search: { delegate: id },
    },
    findContextItem(id, looking.has(id)),
    deferred
      ? {
          label: 'Not later',
          hint: 'Bring this back to the Agenda and next',
          onSelect: () => undefer.mutate([id]),
        }
      : {
          label: 'Later',
          hint: 'Put this off. It leaves the Agenda and next',
          search: { later: id },
        },
    {
      label: 'Cut a dependency',
      hint: 'Let it go ahead without something it waits on',
      search: { cut: id },
    },
    ...(extra ?? []),
  ]
  return <RowMenu label={`Actions for ${title || id}`} always={always} items={items} />
}
