import type { ReactNode } from 'react'
import { useNavigate, useRouterState } from '@tanstack/react-router'
import AppBar from '@mui/material/AppBar'
import Box from '@mui/material/Box'
import IconButton from '@mui/material/IconButton'
import Tabs from '@mui/material/Tabs'
import Toolbar from '@mui/material/Toolbar'
import Tooltip from '@mui/material/Tooltip'
import Typography from '@mui/material/Typography'
import AddIcon from '@mui/icons-material/Add'
import HelpOutlinedIcon from '@mui/icons-material/HelpOutlined'
import LogoutIcon from '@mui/icons-material/Logout'
import { HttpBanner } from '@/components/Banners'
import { LinkFab, LinkIconButton, LinkTab } from '@/components/links'
import { signOut } from '@/client/auth'
import { TABS } from '@/lib/keys'
import { UI_COMMIT } from '@/buildStamp'
import type { SessionView } from '@/client/session'

// The signed-in frame: app bar, tabs, the plain-HTTP banner, and the capture button. Phone
// width first: tabs scroll sideways, and the content has a 12 px gutter.
export function AppShell({ session, children }: { session: SessionView; children: ReactNode }) {
  const navigate = useNavigate()
  const path = useRouterState({ select: (s) => s.location.pathname })
  const current = TABS.find((t) => path === t.to || path.startsWith(`${t.to}/`))?.to ?? false

  const out = async () => {
    await signOut()
    await navigate({ to: '/login', search: {}, replace: true })
  }

  return (
    <Box sx={{ minHeight: '100dvh', pb: 10 }}>
      <AppBar position="sticky" color="primary" enableColorOnDark>
        <Toolbar variant="dense" sx={{ gap: 1, pl: 'max(16px, env(safe-area-inset-left))' }}>
          <Typography variant="h6" component="div" sx={{ flexGrow: 1 }} noWrap>
            para{' '}
            <Box component="span" sx={{ opacity: 0.75, fontSize: '0.8em' }}>
              {session.vault}
            </Box>
          </Typography>
          <Tooltip title="Keyboard help (?)">
            <LinkIconButton
              color="inherit"
              aria-label="Help"
              to="."
              search={(p) => ({ ...p, help: true })}
            >
              <HelpOutlinedIcon />
            </LinkIconButton>
          </Tooltip>
          <Tooltip title={`Sign out ${session.key}`}>
            <IconButton color="inherit" aria-label="Sign out" onClick={() => void out()}>
              <LogoutIcon />
            </IconButton>
          </Tooltip>
        </Toolbar>
        <Tabs
          value={current}
          variant="scrollable"
          scrollButtons="auto"
          textColor="inherit"
          indicatorColor="secondary"
          aria-label="Screens"
        >
          {TABS.map((t) => (
            <LinkTab key={t.to} value={t.to} label={t.label} to={t.to} sx={{ minHeight: 44 }} />
          ))}
        </Tabs>
      </AppBar>
      <HttpBanner />
      <Box component="main" sx={{ px: { xs: 1.5, sm: 3 }, py: 2, maxWidth: 960, mx: 'auto' }}>
        {children}
      </Box>
      <Tooltip title="Capture (c)">
        <LinkFab
          color="primary"
          aria-label="Capture"
          to="."
          search={(p) => ({ ...p, capture: true })}
          sx={{ position: 'fixed', right: 16, bottom: 'max(16px, env(safe-area-inset-bottom))' }}
        >
          <AddIcon />
        </LinkFab>
      </Tooltip>
      <Typography
        variant="caption"
        color="text.secondary"
        sx={{ display: 'block', textAlign: 'center', pb: 1 }}
      >
        ui {UI_COMMIT}
      </Typography>
    </Box>
  )
}
