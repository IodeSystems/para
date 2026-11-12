import { useState } from 'react'
import Box from '@mui/material/Box'
import Tab from '@mui/material/Tab'
import Tabs from '@mui/material/Tabs'
import { Activity } from '@/components/Activity'
import { Thread } from '@/components/Thread'

// ItemLog is the two logs of an item: Conversation (the thread, the owner's turns and the agents'
// answers) and Activity (every change to the item by anyone, with the reason and the diff of an
// agent write). The tab stays on the page; it is not kept between pages.
export function ItemLog({
  id,
  composer = false,
  fill = false,
}: {
  id: string
  composer?: boolean
  fill?: boolean
}) {
  const [tab, setTab] = useState<'conversation' | 'activity'>('conversation')
  return (
    <Box
      data-item-log={id}
      sx={
        fill
          ? { display: 'flex', flexDirection: 'column', height: '100%', minHeight: 0 }
          : { mt: 2 }
      }
    >
      <Tabs
        value={tab}
        onChange={(_, v: 'conversation' | 'activity') => setTab(v)}
        aria-label="Conversation and activity"
        sx={{ mb: 1 }}
      >
        <Tab value="conversation" label="Conversation" data-item-log-tab="conversation" />
        <Tab value="activity" label="Activity" data-item-log-tab="activity" />
      </Tabs>
      {tab === 'conversation' ? (
        <Thread id={id} composer={composer} fill={fill} />
      ) : (
        <Box sx={fill ? { flex: 1, overflowY: 'auto', minHeight: 0 } : undefined}>
          <Activity id={id} tabbed />
        </Box>
      )}
    </Box>
  )
}
