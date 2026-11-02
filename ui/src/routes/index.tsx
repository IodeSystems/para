import { createFileRoute } from '@tanstack/react-router'
import Typography from '@mui/material/Typography'

export const Route = createFileRoute('/')({
  component: Index,
})

function Index() {
  return (
    <Typography variant="h4" component="h1">
      para
    </Typography>
  )
}
