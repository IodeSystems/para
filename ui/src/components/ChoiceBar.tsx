import { useId, useState } from 'react'
import Box from '@mui/material/Box'
import ButtonBase from '@mui/material/ButtonBase'
import IconButton from '@mui/material/IconButton'
import List from '@mui/material/List'
import ListItemButton from '@mui/material/ListItemButton'
import ListItemIcon from '@mui/material/ListItemIcon'
import ListItemText from '@mui/material/ListItemText'
import Stack from '@mui/material/Stack'
import SwipeableDrawer from '@mui/material/SwipeableDrawer'
import Typography from '@mui/material/Typography'
import CheckIcon from '@mui/icons-material/Check'
import CloseIcon from '@mui/icons-material/Close'

// ChoiceBar is one line that shows the last pick of a few single choices (a question's answer once
// picked, else the turn's kind), and opens a sheet from the bottom to change them. The sheet lists every
// choice in full, one large row each; a pick closes it, and so do a swipe down, the backdrop,
// Escape, and the close button. Focus then goes back to the text the choices belong to (onClosed).
// The same bottom sheet on a phone and at a desk.

export type ChoiceGroup = {
  // key is the group's name in data-choice-group; each row has data-choice={value}.
  key: string
  title: string
  options: { value: string; label: string }[]
  value: string
  onChange: (v: string) => void
  // empty is the sheet's prompt while nothing is picked; with it, a picked row taken again clears it.
  empty?: string
}

export function ChoiceBar({
  title,
  groups,
  disabled = false,
  onClosed,
}: {
  // title names the sheet, and the bar to a screen reader.
  title: string
  groups: ChoiceGroup[]
  disabled?: boolean
  onClosed?: () => void
}) {
  const [open, setOpen] = useState(false)
  const titleId = useId()
  const shown = groups.filter((g) => g.options.length > 0)
  if (shown.length === 0) return null
  // The bar shows one label: the last group with a pick. The prompts stay in the sheet.
  const labels = shown.map((g) => g.options.find((o) => o.value === g.value)?.label ?? '')
  const summary = labels.filter(Boolean).at(-1) ?? ''
  const show = () => {
    // The text loses focus first, so a phone's keyboard closes under the sheet.
    if (document.activeElement instanceof HTMLElement) document.activeElement.blur()
    setOpen(true)
  }
  const pick = (g: ChoiceGroup, v: string) => {
    g.onChange(g.empty !== undefined && g.value === v ? '' : v)
    setOpen(false)
  }

  return (
    <>
      <ButtonBase
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-label={`${title}: ${summary}`}
        disabled={disabled}
        onClick={show}
        data-choice-bar
        sx={{
          flex: 1,
          minWidth: 0,
          // 44 px on a phone, as the buttons beside it (a11yGlobals).
          minHeight: { xs: 44, sm: 36 },
          px: 1.5,
          justifyContent: 'flex-start',
          borderRadius: 999,
          border: 1,
          borderColor: 'divider',
          color: 'text.primary',
          fontSize: '0.9375rem',
          '&:hover': { bgcolor: 'action.hover' },
        }}
      >
        <Box
          component="span"
          sx={{ minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
        >
          {summary}
        </Box>
      </ButtonBase>
      <SwipeableDrawer
        anchor="bottom"
        open={open}
        onOpen={() => setOpen(true)}
        onClose={() => setOpen(false)}
        disableSwipeToOpen
        disableDiscovery
        disableRestoreFocus
        slotProps={{
          paper: {
            role: 'dialog',
            'aria-modal': true,
            'aria-labelledby': titleId,
            sx: {
              maxHeight: '80vh',
              maxWidth: 600,
              mx: 'auto',
              borderTopLeftRadius: 20,
              borderTopRightRadius: 20,
              pb: 'env(safe-area-inset-bottom)',
            },
          },
          transition: { onExited: () => onClosed?.() },
        }}
      >
        <Box
          aria-hidden
          sx={{ width: 36, height: 4, borderRadius: 2, bgcolor: 'divider', mx: 'auto', mt: 1 }}
        />
        <Stack
          direction="row"
          sx={{ alignItems: 'center', justifyContent: 'space-between', pl: 2, pr: 1, pt: 0.5 }}
        >
          <Typography id={titleId} variant="subtitle1" component="h2">
            {title}
          </Typography>
          <IconButton aria-label="Close" onClick={() => setOpen(false)} data-choice-close>
            <CloseIcon />
          </IconButton>
        </Stack>
        <Box sx={{ overflowY: 'auto', px: 1, pb: 2 }} data-choice-sheet>
          {shown.map((g) => (
            <Box key={g.key} component="section" data-choice-group={g.key}>
              <Typography
                variant="overline"
                color="text.secondary"
                component="h3"
                sx={{ px: 1.5, display: 'block' }}
              >
                {g.title}
              </Typography>
              {g.empty && !g.value && (
                <Typography variant="body2" color="text.secondary" sx={{ px: 1.5, pb: 0.5 }}>
                  {g.empty}
                </Typography>
              )}
              <List disablePadding>
                {g.options.map((o) => {
                  const on = o.value === g.value
                  return (
                    <ListItemButton
                      key={o.value}
                      selected={on}
                      aria-pressed={on}
                      onClick={() => pick(g, o.value)}
                      data-choice={o.value}
                      sx={{ minHeight: 48, borderRadius: 2, alignItems: 'flex-start', py: 1.25 }}
                    >
                      <ListItemIcon sx={{ minWidth: 36, mt: 0.25 }}>
                        {on && <CheckIcon color="primary" />}
                      </ListItemIcon>
                      <ListItemText
                        primary={o.label}
                        sx={{ m: 0 }}
                        slotProps={{
                          primary: {
                            sx: { fontSize: '1rem', lineHeight: 1.45, overflowWrap: 'anywhere' },
                          },
                        }}
                      />
                    </ListItemButton>
                  )
                })}
              </List>
            </Box>
          ))}
        </Box>
      </SwipeableDrawer>
    </>
  )
}
