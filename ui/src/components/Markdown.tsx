import { Children, isValidElement, type ReactNode } from 'react'
import ReactMarkdown, { type Components } from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { TextLink } from '@/components/links'
import Box from '@mui/material/Box'
import Link from '@mui/material/Link'
import { isInternalHref, linkWikilinks } from '@/lib/markdown'
import { Mermaid } from '@/components/Mermaid'

// Renders para-md. No raw HTML: react-markdown does not parse it (it is dropped, never
// injected), and urlTransform keeps only safe link schemes. Wikilinks become in-app routes.
// Tables scroll sideways on a phone. A ```mermaid fence draws a diagram in strict mode. Images
// render as their alt text: the CSP allows no remote images, and a vault is no place to fetch from.

function text(node: ReactNode): string {
  return Children.toArray(node)
    .map((c) =>
      typeof c === 'string'
        ? c
        : isValidElement<{ children?: ReactNode }>(c)
          ? text(c.props.children)
          : '',
    )
    .join('')
}

const components: Components = {
  a({ href, children }) {
    if (isInternalHref(href)) {
      return <TextLink to={href}>{children}</TextLink>
    }
    return (
      <Link href={href} target="_blank" rel="noreferrer noopener">
        {children}
      </Link>
    )
  },
  img({ alt }) {
    return (
      <Box component="span" sx={{ fontStyle: 'italic', opacity: 0.8 }}>
        [image: {alt || 'no description'}]
      </Box>
    )
  },
  table({ children }) {
    return (
      <Box sx={{ overflowX: 'auto', my: 1.5, maxWidth: '100%' }}>
        <Box
          component="table"
          sx={{
            borderCollapse: 'collapse',
            minWidth: 'max-content',
            '& th, & td': {
              border: 1,
              borderColor: 'divider',
              px: 1.25,
              py: 0.75,
              textAlign: 'left',
              verticalAlign: 'top',
            },
            '& th': { bgcolor: 'action.hover' },
          }}
        >
          {children}
        </Box>
      </Box>
    )
  },
  pre({ children }) {
    // A mermaid fence is a <pre><code class="language-mermaid">.
    const only = Children.toArray(children)[0]
    if (
      isValidElement<{ className?: string; children?: ReactNode }>(only) &&
      /\blanguage-mermaid\b/.test(only.props.className ?? '')
    ) {
      return <Mermaid source={text(only.props.children)} label="Diagram from the document" />
    }
    return (
      <Box
        component="pre"
        sx={{ overflowX: 'auto', p: 1.5, borderRadius: 1, bgcolor: 'action.hover', fontSize: 13 }}
      >
        {children}
      </Box>
    )
  },
  code({ className, children }) {
    return (
      <Box
        component="code"
        className={className}
        sx={{ fontFamily: 'ui-monospace, monospace', fontSize: '0.9em' }}
      >
        {children}
      </Box>
    )
  },
}

export function Markdown({ source }: { source: string }) {
  return (
    <Box
      sx={{
        overflowWrap: 'anywhere',
        '& h1': { fontSize: '1.5rem' },
        '& h2': { fontSize: '1.25rem' },
        '& h3': { fontSize: '1.1rem' },
        '& img': { maxWidth: '100%' },
      }}
    >
      <ReactMarkdown remarkPlugins={[remarkGfm]} components={components}>
        {linkWikilinks(source)}
      </ReactMarkdown>
    </Box>
  )
}
