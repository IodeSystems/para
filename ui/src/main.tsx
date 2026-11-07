import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { RouterProvider, createRouter } from '@tanstack/react-router'
import { AppProviders } from '@/providers/AppProviders'
import { installChunkReload } from '@/client/chunkReload'
import { routeTree } from './routeTree.gen'

installChunkReload()

const router = createRouter({ routeTree, defaultPreload: 'intent' })

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}

// The boot screen in index.html stays until the router has rendered the first screen.
void router.load().then(() => document.getElementById('boot')?.remove())

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <AppProviders>
      <RouterProvider router={router} />
    </AppProviders>
  </StrictMode>,
)
