import { Outlet, createRootRoute } from '@tanstack/react-router'
import Container from '@mui/material/Container'

export const Route = createRootRoute({
  component: () => (
    <Container maxWidth="md" sx={{ py: 4 }}>
      <Outlet />
    </Container>
  ),
})
