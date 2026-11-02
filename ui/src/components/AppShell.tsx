import type { ReactNode } from 'react'
import { useNavigate, useRouterState } from '@tanstack/react-router'
import AppBar from '@mui/material/AppBar'
import BottomNavigation from '@mui/material/BottomNavigation'
import Paper from '@mui/material/Paper'
import Box from '@mui/material/Box'
import IconButton from '@mui/material/IconButton'
import Tabs from '@mui/material/Tabs'
import Toolbar from '@mui/material/Toolbar'
import Tooltip from '@mui/material/Tooltip'
import Typography from '@mui/material/Typography'
import AddIcon from '@mui/icons-material/Add'
import HelpOutlinedIcon from '@mui/icons-material/HelpOutlined'
import LogoutIcon from '@mui/icons-material/Logout'
import MenuIcon from '@mui/icons-material/Menu'
import RateReviewOutlinedIcon from '@mui/icons-material/RateReviewOutlined'
import SearchIcon from '@mui/icons-material/Search'
import TodayIcon from '@mui/icons-material/Today'
import Badge from '@mui/material/Badge'
import { useOpenProposals } from '@/api/proposals'
import { HttpBanner } from '@/components/Banners'
import { LinkBottomNavAction, LinkButton, LinkIconButton, LinkTab } from '@/components/links'
import { signOut } from '@/client/auth'
import { TABS } from '@/lib/keys'
import type { SessionView } from '@/client/session'

// The signed-in frame: app bar, tabs, the plain-HTTP banner, and the capture button. Capture sits
// in the bar, in one place on every screen, so it never competes with a page's own main button.
// Below 900 px the tabs fold into a menu, so every tab can be reached.
export function AppShell({ session, children }: { session: SessionView; children: ReactNode }) {
  const navigate = useNavigate()
  const path = useRouterState({ select: (s) => s.location.pathname })
  const current = TABS.find((t) => path === t.to || path.startsWith(`${t.to}/`))
  const { open } = useOpenProposals()
  const wide = { xs: 'none', md: 'flex' }
  // On a phone the bottom bar holds Menu, so the top bar shows it only between 600 and 900 px.
  const narrow = { xs: 'none', sm: 'inline-flex', md: 'none' }

  const out = async () => {
    await signOut()
    await navigate({ to: '/login', search: { signedout: true }, replace: true })
  }

  return (
    <Box sx={{ minHeight: '100dvh', pb: { xs: 10, sm: 4 } }}>
      <AppBar position="sticky" color="primary" enableColorOnDark>
        <Toolbar variant="dense" sx={{ gap: 1, pl: 'max(8px, env(safe-area-inset-left))' }}>
          <LinkButton
            color="inherit"
            aria-label="Menu"
            startIcon={
              <Badge color="warning" variant="dot" invisible={open.length === 0}>
                <MenuIcon />
              </Badge>
            }
            to="."
            search={(p) => ({ ...p, menu: true })}
            sx={{ display: narrow, minWidth: 0 }}
          >
            Menu
          </LinkButton>
          <Typography variant="h6" component="div" sx={{ flexGrow: 1, pl: { md: 1 } }} noWrap>
            para
            {/* The vault is shown as plain text: the session is bound to one vault by its address. */}
            <Box component="span" sx={{ ml: 1, opacity: 0.8, fontSize: '0.8em' }} aria-label="Vault">
              {session.vault}
            </Box>
          </Typography>
          <LinkButton
            color="inherit"
            variant="outlined"
            size="small"
            startIcon={<AddIcon />}
            to="."
            search={(p) => ({ ...p, capture: true })}
            aria-keyshortcuts="c"
          >
            Add to inbox
            <Box
              component="kbd"
              sx={{
                display: { xs: 'none', md: 'inline' },
                ml: 1,
                px: 0.75,
                border: 1,
                borderColor: 'currentColor',
                borderRadius: 0.5,
                fontFamily: 'ui-monospace, monospace',
                fontSize: '0.75em',
                opacity: 0.8,
              }}
            >
              c
            </Box>
          </LinkButton>
          <Tooltip title="Keys and about (?)">
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
          value={current?.to ?? false}
          variant="scrollable"
          scrollButtons="auto"
          textColor="inherit"
          indicatorColor="secondary"
          aria-label="Screens"
          sx={{ display: wide }}
        >
          {TABS.map((t) => (
            <LinkTab
              key={t.to}
              value={t.to}
              label={
                t.to === '/review' && open.length > 0 ? (
                  <Badge
                    color="warning"
                    badgeContent={open.length}
                    sx={{ '& .MuiBadge-badge': { right: -14, top: 2 } }}
                  >
                    {t.label}
                  </Badge>
                ) : (
                  t.label
                )
              }
              title={t.hint}
              to={t.to}
              sx={{
                minHeight: 44,
                minWidth: 0,
                px: 2,
                pr: t.to === '/review' && open.length > 0 ? 3.5 : 2,
              }}
            />
          ))}
        </Tabs>
      </AppBar>
      <HttpBanner />
      <Box component="main" sx={{ px: { xs: 1.5, sm: 3 }, py: 2, maxWidth: 960, mx: 'auto' }}>
        {children}
      </Box>
      {/* On a phone the four screens you open most are one tap away. */}
      <Paper
        component="nav"
        aria-label="Quick screens"
        elevation={8}
        sx={{
          display: { xs: 'block', sm: 'none' },
          position: 'fixed',
          bottom: 0,
          left: 0,
          right: 0,
          zIndex: (t) => t.zIndex.appBar,
          pb: 'env(safe-area-inset-bottom)',
        }}
      >
        <BottomNavigation
          showLabels
          value={
            current?.to === '/search' || current?.to === '/agenda' || current?.to === '/review'
              ? current.to
              : false
          }
        >
          <LinkBottomNavAction value="/agenda" label="Agenda" icon={<TodayIcon />} to="/agenda" />
          <LinkBottomNavAction
            value="/review"
            label="Review"
            icon={
              <Badge color="warning" badgeContent={open.length}>
                <RateReviewOutlinedIcon />
              </Badge>
            }
            to="/review"
          />
          <LinkBottomNavAction value="/search" label="Search" icon={<SearchIcon />} to="/search" />
          <LinkBottomNavAction
            value="menu"
            label="Menu"
            icon={<MenuIcon />}
            to="."
            search={(p) => ({ ...p, menu: true })}
          />
        </BottomNavigation>
      </Paper>
    </Box>
  )
}
