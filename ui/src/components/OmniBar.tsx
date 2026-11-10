import { Fragment, useEffect, useId, useMemo, useRef, useState } from 'react'
import { useNavigate } from '@tanstack/react-router'
import Box from '@mui/material/Box'
import IconButton from '@mui/material/IconButton'
import InputBase from '@mui/material/InputBase'
import Paper from '@mui/material/Paper'
import Typography from '@mui/material/Typography'
import CloseIcon from '@mui/icons-material/Close'
import SearchIcon from '@mui/icons-material/Search'
import { useSearch } from '@/api/search'
import { entriesFor, isOmniKey, nextIndex, type OmniEntry } from '@/lib/omni'

const DEBOUNCE_MS = 150

// OMNI_FOCUS is the event the "/" shortcut sends to focus the bar.
export const OMNI_FOCUS = 'para:omni-focus'

// OmniBar is the one search field of the app bar: results by kind as you type, page names, a
// pasted id. Enter with nothing picked goes to the full results page.
export function OmniBar() {
  const navigate = useNavigate()
  const uid = useId()
  const listId = `${uid}-list`
  const input = useRef<HTMLInputElement>(null)
  const [text, setText] = useState('')
  const [debounced, setDebounced] = useState('')
  const [open, setOpen] = useState(false)
  // expanded: on a phone the field replaces the app bar row until it is closed.
  const [expanded, setExpanded] = useState(false)
  const [active, setActive] = useState(-1)

  useEffect(() => {
    const t = window.setTimeout(() => setDebounced(text.trim()), DEBOUNCE_MS)
    return () => window.clearTimeout(t)
  }, [text])

  const hits = useSearch(debounced)
  const entries = useMemo(
    () => entriesFor(text, debounced === '' ? [] : (hits.data?.hits ?? [])),
    [text, debounced, hits.data],
  )

  const focus = () => {
    setExpanded(true)
    window.setTimeout(() => {
      input.current?.focus()
      input.current?.select()
    }, 0)
  }
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (!isOmniKey(e)) return
      e.preventDefault()
      focus()
    }
    window.addEventListener('keydown', onKey)
    window.addEventListener(OMNI_FOCUS, focus)
    return () => {
      window.removeEventListener('keydown', onKey)
      window.removeEventListener(OMNI_FOCUS, focus)
    }
  }, [])

  const close = () => {
    setOpen(false)
    setActive(-1)
  }
  const reset = () => {
    close()
    setText('')
    setDebounced('')
    setExpanded(false)
  }

  const go = (e: OmniEntry) => {
    reset()
    input.current?.blur()
    if (e.to) void navigate({ href: e.to })
    else if (e.id) void navigate({ to: '/docs/$id', params: { id: e.id } })
  }

  const onKeyDown = (e: React.KeyboardEvent) => {
    switch (e.key) {
      case 'ArrowDown':
      case 'ArrowUp':
        e.preventDefault()
        setOpen(true)
        setActive((a) => nextIndex(a, e.key === 'ArrowDown' ? 'down' : 'up', entries.length))
        break
      case 'Enter': {
        e.preventDefault()
        const picked = entries[active]
        const q = text.trim()
        if (picked) go(picked)
        else if (entries[0]?.group === 'Open') go(entries[0])
        else if (q !== '') {
          reset()
          input.current?.blur()
          void navigate({ to: '/search', search: { q } })
        }
        break
      }
      case 'Escape':
        e.preventDefault()
        if (open && entries.length > 0) close()
        else {
          reset()
          input.current?.blur()
        }
        break
    }
  }

  const showList = open && text.trim() !== ''
  return (
    <>
      <IconButton
        color="inherit"
        aria-label="Search"
        onClick={focus}
        sx={{ display: { xs: expanded ? 'none' : 'inline-flex', sm: 'none' } }}
      >
        <SearchIcon />
      </IconButton>
      <Box
        sx={{
          display: { xs: expanded ? 'flex' : 'none', sm: 'flex' },
          position: { xs: 'absolute', sm: 'relative' },
          left: { xs: 0, sm: 'auto' },
          right: { xs: 0, sm: 'auto' },
          top: { xs: 0, sm: 'auto' },
          height: { xs: '100%', sm: 'auto' },
          zIndex: 1,
          bgcolor: { xs: 'primary.main', sm: 'rgba(255,255,255,0.15)' },
          borderRadius: { sm: 1 },
          alignItems: 'center',
          px: 1,
          width: { sm: 260, md: 340 },
          maxWidth: '100%',
        }}
      >
        <SearchIcon fontSize="small" sx={{ mr: 1, opacity: 0.8 }} aria-hidden />
        <InputBase
          inputRef={input}
          value={text}
          placeholder="Search or go to (Ctrl K)"
          onChange={(e) => {
            setText(e.target.value)
            setOpen(true)
            setActive(-1)
          }}
          onFocus={() => setOpen(true)}
          onBlur={() => {
            close()
            setExpanded(false)
          }}
          onKeyDown={onKeyDown}
          fullWidth
          sx={{ color: 'inherit', fontSize: 14, py: 0.25 }}
          slotProps={{
            input: {
              role: 'combobox',
              'aria-label': 'Search',
              'aria-expanded': showList,
              'aria-controls': listId,
              'aria-autocomplete': 'list',
              'aria-haspopup': 'listbox',
              'aria-activedescendant': showList && active >= 0 ? `${uid}-opt-${active}` : undefined,
              autoComplete: 'off',
              spellCheck: false,
            },
          }}
        />
        <IconButton
          size="small"
          color="inherit"
          aria-label="Close search"
          onMouseDown={(e) => e.preventDefault()}
          onClick={() => {
            reset()
            input.current?.blur()
          }}
          sx={{ display: { xs: 'inline-flex', sm: 'none' } }}
        >
          <CloseIcon fontSize="small" />
        </IconButton>
        {showList && (
          <Paper
            elevation={8}
            // Clicks must not blur the input before they land.
            onMouseDown={(e) => e.preventDefault()}
            sx={{
              position: 'absolute',
              top: '100%',
              left: 0,
              right: 0,
              mt: 0.5,
              maxHeight: '70dvh',
              overflowY: 'auto',
              color: 'text.primary',
              minWidth: { sm: 360 },
            }}
          >
            <Box
              component="ul"
              role="listbox"
              id={listId}
              aria-label="Results"
              sx={{ m: 0, p: 0, listStyle: 'none' }}
            >
              {entries.map((e, i) => {
                const head = e.group !== entries[i - 1]?.group
                return (
                  <Fragment key={e.key}>
                    {head && (
                      <Typography
                        component="li"
                        role="presentation"
                        variant="overline"
                        color="text.secondary"
                        sx={{ display: 'block', px: 1.5, pt: 1, lineHeight: 1.6 }}
                      >
                        {e.group}
                      </Typography>
                    )}
                    <Box
                      component="li"
                      role="option"
                      id={`${uid}-opt-${i}`}
                      aria-selected={i === active}
                      onClick={() => go(e)}
                      onMouseMove={() => setActive(i)}
                      sx={{
                        px: 1.5,
                        py: 0.75,
                        cursor: 'pointer',
                        bgcolor: i === active ? 'action.selected' : undefined,
                        overflowWrap: 'anywhere',
                      }}
                    >
                      <Typography variant="body2">{e.label}</Typography>
                      {e.hint && e.hint !== e.label && (
                        <Typography variant="caption" color="text.secondary">
                          {e.hint}
                        </Typography>
                      )}
                    </Box>
                  </Fragment>
                )
              })}
              {entries.length === 0 && (
                <Typography
                  component="li"
                  role="presentation"
                  variant="body2"
                  color="text.secondary"
                  sx={{ px: 1.5, py: 1 }}
                >
                  {hits.isFetching || debounced !== text.trim()
                    ? 'Searching…'
                    : 'No matches. Enter shows the full search.'}
                </Typography>
              )}
            </Box>
          </Paper>
        )}
      </Box>
    </>
  )
}
