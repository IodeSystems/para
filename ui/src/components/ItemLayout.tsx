import { useState, type ReactNode } from 'react'
import Box from '@mui/material/Box'
import Tab from '@mui/material/Tab'
import Tabs from '@mui/material/Tabs'
import useMediaQuery from '@mui/material/useMediaQuery'
import { useTheme } from '@mui/material/styles'
import { ItemLog } from '@/components/ItemLog'

// ItemLayout is the page of one item. From the md breakpoint (900px) up: the item on the left, the
// conversation on the right in a sticky pane of the viewport's height, with its own scroll.
// Below md: tabs Item / Conversation. AppShell widens the page for these routes (ITEM_PATH).
export const ITEM_PATH = /^\/(goals|projects|obligations|io|docs|rfx)\/[^/]+\/?$/

// Header height (toolbar and screen tabs) the sticky pane sits under, and the page padding.
const HEADER = 120

export function ItemLayout({
  id,
  composer = false,
  children,
}: {
  id: string
  composer?: boolean
  children: ReactNode
}) {
  const theme = useTheme()
  const wide = useMediaQuery(theme.breakpoints.up('md'))
  const [tab, setTab] = useState<'item' | 'conversation'>('item')
  if (wide) {
    return (
      <Box
        data-item-layout="columns"
        sx={{ display: 'grid', gridTemplateColumns: 'minmax(0, 1fr) minmax(360px, 460px)', gap: 3 }}
      >
        <Box sx={{ minWidth: 0 }}>{children}</Box>
        <Box
          sx={{
            position: 'sticky',
            top: HEADER,
            alignSelf: 'start',
            height: `calc(100vh - ${HEADER + 16}px)`,
          }}
        >
          <ItemLog id={id} composer={composer} fill />
        </Box>
      </Box>
    )
  }
  return (
    <Box data-item-layout="tabs">
      <Tabs
        value={tab}
        onChange={(_, v: 'item' | 'conversation') => setTab(v)}
        aria-label="Item and conversation"
        sx={{ mb: 1 }}
      >
        <Tab value="item" label="Item" data-item-tab="item" />
        <Tab value="conversation" label="Conversation" data-item-tab="conversation" />
      </Tabs>
      {tab === 'item' ? (
        children
      ) : (
        <Box sx={{ height: 'calc(100vh - 260px)', minHeight: 360 }}>
          <ItemLog id={id} composer={composer} fill />
        </Box>
      )}
    </Box>
  )
}
