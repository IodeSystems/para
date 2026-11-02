import { useId, useState } from 'react'
import { useNavigate } from '@tanstack/react-router'
import Button from '@mui/material/Button'
import IconButton from '@mui/material/IconButton'
import ListItemText from '@mui/material/ListItemText'
import Menu from '@mui/material/Menu'
import MenuItem from '@mui/material/MenuItem'
import ArrowDropDownIcon from '@mui/icons-material/ArrowDropDown'
import MoreHorizIcon from '@mui/icons-material/MoreHoriz'

export type RowMenuItem = {
  label: string
  // hint is the one-line explanation under the label.
  hint?: string
  // search is the URL overlay the item opens: a key and the row's id.
  search?: Record<string, string>
  onSelect?: () => void
  disabled?: boolean
  color?: 'error'
}

// RowMenu is the "more" button of a row. A menu holds the actions a row does not lead with. On a
// device with a pointer the button shows when the row is hovered or has focus; on touch it is
// always there. Put it inside an element with the class `row-with-menu`.
export function RowMenu({
  items,
  label,
  always,
  text,
  beforeOpen,
}: {
  items: RowMenuItem[]
  label: string
  // always keeps the button visible on pointer devices too.
  always?: boolean
  // text makes the button a labelled text button, not an icon.
  text?: string
  // beforeOpen runs on the press that opens the menu, before focus moves: a text selection can
  // still be read there.
  beforeOpen?: () => void
}) {
  const [anchor, setAnchor] = useState<HTMLElement | null>(null)
  const navigate = useNavigate()
  const id = useId()
  if (items.length === 0) return null
  const close = () => setAnchor(null)
  return (
    <>
      {text ? (
        <Button
          size="small"
          aria-label={label}
          aria-haspopup="menu"
          aria-expanded={anchor ? true : undefined}
          aria-controls={anchor ? id : undefined}
          endIcon={<ArrowDropDownIcon />}
          onPointerDown={beforeOpen}
          onClick={(e) => setAnchor(e.currentTarget)}
        >
          {text}
        </Button>
      ) : (
        <IconButton
          size="small"
          aria-label={label}
          aria-haspopup="menu"
          aria-expanded={anchor ? true : undefined}
          aria-controls={anchor ? id : undefined}
          onPointerDown={beforeOpen}
          onClick={(e) => setAnchor(e.currentTarget)}
          className="row-menu-button"
          sx={
            always
              ? undefined
              : {
                  '@media (hover: hover)': {
                    opacity: 0,
                    '.row-with-menu:hover &, .row-with-menu:focus-within &, &:focus-visible, &[aria-expanded="true"]':
                      { opacity: 1 },
                  },
                }
          }
        >
          <MoreHorizIcon fontSize="small" />
        </IconButton>
      )}
      <Menu id={id} anchorEl={anchor} open={!!anchor} onClose={close}>
        {items.map((it) => (
          <MenuItem
            key={it.label}
            disabled={it.disabled}
            onClick={() => {
              close()
              it.onSelect?.()
              const search = it.search
              if (search) void navigate({ to: '.', search: (p) => ({ ...p, ...search }) })
            }}
            sx={{
              maxWidth: 320,
              whiteSpace: 'normal',
              color: it.color ? `${it.color}.main` : undefined,
            }}
          >
            <ListItemText
              primary={it.label}
              secondary={it.hint}
              slotProps={{ secondary: { sx: { whiteSpace: 'normal' } } }}
            />
          </MenuItem>
        ))}
      </Menu>
    </>
  )
}
