import { useNavigate } from '@tanstack/react-router'
import { NO_OVERLAYS } from '@/lib/overlays'

// Every overlay is a search param on the current route (WEB-10). Closing drops the params in
// place with replace, so Back never re-opens one.
export function useClose() {
  const navigate = useNavigate()
  return () =>
    void navigate({ to: '.', search: (prev) => ({ ...prev, ...NO_OVERLAYS }), replace: true })
}
