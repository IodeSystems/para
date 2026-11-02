import { StatusChip } from '@/components/Bits'
import { useLooking, useWorking } from '@/api/requests'
import type { RowMenuItem } from '@/components/RowMenu'

// findContextItem is the "Find context" entry of a row menu. It opens the hint dialog on the
// current route (the `find` overlay).
export function findContextItem(id: string, looking: boolean): RowMenuItem {
  return {
    label: 'Find context',
    hint: looking
      ? 'An agent is looking already. A hint goes into its work'
      : 'Send this to an agent to find context and sources; you check what it proposes',
    search: { find: id },
  }
}

// AgentLooking is the chip a row shows while open agent work looks into the item.
export function AgentLooking({ id }: { id: string }) {
  const looking = useLooking()
  return looking.has(id) ? <StatusChip tone="good" label="Agent looking" /> : null
}

// AgentWorking is the chip a task row shows while delegated agent work serves it.
export function AgentWorking({ id }: { id: string }) {
  const working = useWorking()
  return working.has(id) ? <StatusChip tone="good" label="Agent working" /> : null
}
