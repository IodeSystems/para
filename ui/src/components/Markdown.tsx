import {
  Children,
  createContext,
  isValidElement,
  useContext,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from 'react'
import ReactMarkdown, { type Components, type ExtraProps } from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { TextLink } from '@/components/links'
import Box from '@mui/material/Box'
import Link from '@mui/material/Link'
import Typography from '@mui/material/Typography'
import { useDoc } from '@/api/doc'
import { blockInfo } from '@/lib/dossier'
import { isInternalHref, linkWikilinks, WIKI_TITLE } from '@/lib/markdown'
import { nameOr } from '@/lib/names'
import { Mermaid } from '@/components/Mermaid'
import { whenify } from '@/components/When'

// Renders para-md. No raw HTML: react-markdown does not parse it (it is dropped, never
// injected), and urlTransform keeps only safe link schemes. Wikilinks become in-app routes.
// Tables scroll sideways on a phone. A ```mermaid fence draws a diagram in strict mode. Images
// render as their alt text: the CSP allows no remote images, and a vault is no place to fetch from.
// A dossier page gives Blocks (below): it draws its own card for a fenced block (a para item) and
// adds a part under each ## heading (a comment box).

export type Blocks = {
  // block gets the info string of a fenced block ("yaml para-question id=q1") and its text. It
  // gives undefined to render the block as code.
  block?: (info: string, body: string) => ReactNode | undefined
  // section gets the text of a ## heading and gives what goes under it.
  section?: (title: string) => ReactNode
}

const BlocksContext = createContext<Blocks | null>(null)

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

function WikiLink({ href, children }: { href: string; children?: ReactNode }) {
  const id = decodeURIComponent(href.replace(/^\/doc\//, ''))
  const q = useDoc(id)
  const label = q.data?.title || nameOr('', id) || text(children)
  return <TextLink to={href}>{label}</TextLink>
}

const NUMBER = /^[\s$€£+\-−]*\d[\d,.\s]*(%|[a-zA-Z]{0,3})?\s*$/

// ScrollTable scrolls a wide table sideways. It shows a shadow on the side that has more, says so
// under the table, and right-aligns a column whose cells are all numbers.
function ScrollTable({ children }: { children?: ReactNode }) {
  const box = useRef<HTMLDivElement>(null)
  const [more, setMore] = useState({ left: false, right: false })
  useEffect(() => {
    const el = box.current
    if (!el) return
    const measure = () =>
      setMore({
        left: el.scrollLeft > 1,
        right: el.scrollLeft + el.clientWidth < el.scrollWidth - 1,
      })
    measure()
    el.addEventListener('scroll', measure, { passive: true })
    const ro = new ResizeObserver(measure)
    ro.observe(el)
    return () => {
      el.removeEventListener('scroll', measure)
      ro.disconnect()
    }
  }, [children])
  useEffect(() => {
    const t = box.current?.querySelector('table')
    if (!t) return
    const rows = Array.from(t.rows)
    const width = Math.max(0, ...rows.map((r) => r.cells.length))
    // Below 600 px a table of three or more columns shows as cards: each cell carries its column
    // name (see the data-cards rules in the style below).
    const head = t.tHead?.rows[0]
    if (width >= 3 && head) {
      t.dataset.cards = 'true'
      for (const r of rows) {
        if (r.parentElement?.tagName === 'THEAD') continue
        Array.from(r.cells).forEach((cell, c) => {
          cell.dataset.label = head.cells[c]?.textContent ?? ''
        })
      }
    }
    for (let c = 0; c < width; c++) {
      const body = rows.filter((r) => r.parentElement?.tagName !== 'THEAD')
      const cells = body.map((r) => r.cells[c]).filter((x): x is HTMLTableCellElement => !!x)
      const numeric = cells.length > 0 && cells.every((x) => NUMBER.test(x.textContent ?? ''))
      for (const r of rows) {
        const cell = r.cells[c]
        if (cell && numeric) {
          cell.style.textAlign = 'right'
          cell.style.fontVariantNumeric = 'tabular-nums'
        }
      }
    }
  }, [children])
  const shadow = 'rgba(0,0,0,0.28)'
  return (
    <Box sx={{ my: 1.5, maxWidth: '100%' }}>
      <Box sx={{ position: 'relative' }}>
        <Box
          ref={box}
          tabIndex={0}
          role="region"
          aria-label="Table, scrolls sideways"
          sx={{ overflowX: 'auto', maxWidth: '100%' }}
        >
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
              '@media (max-width: 599px)': {
                '&[data-cards="true"]': {
                  display: 'block',
                  minWidth: 0,
                  width: '100%',
                  '& thead': {
                    position: 'absolute',
                    width: 1,
                    height: 1,
                    overflow: 'hidden',
                    clip: 'rect(0 0 0 0)',
                  },
                  '& tbody, & tr, & td': { display: 'block' },
                  '& tr': { border: 1, borderColor: 'divider', borderRadius: 1, mb: 1 },
                  '& td': {
                    border: 0,
                    borderBottom: 1,
                    borderColor: 'divider',
                    textAlign: 'left !important',
                  },
                  '& td:last-child': { borderBottom: 0 },
                  '& td::before': {
                    content: 'attr(data-label)',
                    display: 'block',
                    fontSize: '0.75rem',
                    color: 'text.secondary',
                  },
                },
              },
            }}
          >
            {children}
          </Box>
        </Box>
        {more.right && (
          <Box
            aria-hidden
            sx={{
              position: 'absolute',
              top: 0,
              bottom: 0,
              right: 0,
              width: 20,
              pointerEvents: 'none',
              background: `linear-gradient(to left, ${shadow}, transparent)`,
            }}
          />
        )}
        {more.left && (
          <Box
            aria-hidden
            sx={{
              position: 'absolute',
              top: 0,
              bottom: 0,
              left: 0,
              width: 20,
              pointerEvents: 'none',
              background: `linear-gradient(to right, ${shadow}, transparent)`,
            }}
          />
        )}
      </Box>
      {(more.left || more.right) && (
        <Typography variant="caption" color="text.secondary">
          Scroll sideways for more columns.
        </Typography>
      )}
    </Box>
  )
}

// codeMeta is the info string after the language of a fenced block. The parser keeps it on the
// data of the <code> node, not on its props.
function codeMeta(node: ExtraProps['node']): string | undefined {
  const code = node?.children[0]
  if (!code || code.type !== 'element') return undefined
  const meta: unknown = (code.data as { meta?: unknown } | undefined)?.meta
  return typeof meta === 'string' ? meta : undefined
}

function Pre({ children, node }: { children?: ReactNode } & ExtraProps) {
  const blocks = useContext(BlocksContext)
  const only = Children.toArray(children)[0]
  if (isValidElement<{ className?: string; children?: ReactNode }>(only)) {
    // A mermaid fence is a <pre><code class="language-mermaid">.
    if (/\blanguage-mermaid\b/.test(only.props.className ?? '')) {
      return <Mermaid source={text(only.props.children)} label="Diagram from the document" />
    }
    if (blocks?.block) {
      const drawn = blocks.block(
        blockInfo(only.props.className, codeMeta(node)),
        text(only.props.children),
      )
      if (drawn !== undefined) return <>{drawn}</>
    }
  }
  return (
    <Box
      component="pre"
      sx={{ overflowX: 'auto', p: 1.5, borderRadius: 1, bgcolor: 'action.hover', fontSize: 13 }}
    >
      {children}
    </Box>
  )
}

function H2({ children }: { children?: ReactNode }) {
  const blocks = useContext(BlocksContext)
  return (
    <>
      <h2>{children}</h2>
      {blocks?.section?.(text(children))}
    </>
  )
}

const components: Components = {
  p({ children }) {
    return <p>{whenify(children)}</p>
  },
  li({ children }) {
    return <li>{whenify(children)}</li>
  },
  a({ href, title, children }) {
    if (isInternalHref(href)) {
      // A link with only an id for a label shows the title of what it points at.
      if (title === WIKI_TITLE && href) return <WikiLink href={href}>{children}</WikiLink>
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
    return <ScrollTable>{children}</ScrollTable>
  },
  pre: Pre,
  h2: H2,
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

export function Markdown({ source, blocks }: { source: string; blocks?: Blocks }) {
  return (
    <BlocksContext.Provider value={blocks ?? null}>
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
    </BlocksContext.Provider>
  )
}
