import { createFileRoute } from '@tanstack/react-router'
import Typography from '@mui/material/Typography'
import { PageTitle } from '@/components/Bits'
import { ItemsUnder } from '@/components/ItemsUnder'

export const Route = createFileRoute('/_authed/tags/$tag')({ component: TagPage })

// TagPage lists the open items with a tag, for one decision on several of them.
function TagPage() {
  const { tag } = Route.useParams()
  return (
    <>
      <PageTitle pin={{ key: `#${tag}` }}>#{tag}</PageTitle>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
        Open items tagged {tag}.
      </Typography>
      <ItemsUnder theme={`#${tag}`} title="Items with this tag" />
    </>
  )
}
