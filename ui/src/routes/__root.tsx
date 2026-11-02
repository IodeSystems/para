import { Outlet, createRootRoute } from '@tanstack/react-router'
import Box from '@mui/material/Box'
import { LinkButton } from '@/components/links'
import Typography from '@mui/material/Typography'
import { PwaUpdatePrompt } from '@/components/PwaUpdatePrompt'
import { validateOverlays } from '@/lib/overlays'

export const Route = createRootRoute({
  validateSearch: validateOverlays,
  component: () => (
    <>
      <Outlet />
      <PwaUpdatePrompt />
    </>
  ),
  notFoundComponent: () => (
    <Box sx={{ p: 4 }}>
      <Typography variant="h5" component="h1" gutterBottom>
        Not found
      </Typography>
      <LinkButton to="/today">Go to Today</LinkButton>
    </Box>
  ),
})
