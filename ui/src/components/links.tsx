import { createLink } from '@tanstack/react-router'
import BottomNavigationAction from '@mui/material/BottomNavigationAction'
import Button from '@mui/material/Button'
import Fab from '@mui/material/Fab'
import IconButton from '@mui/material/IconButton'
import Link from '@mui/material/Link'
import Tab from '@mui/material/Tab'

// MUI components that are router links. createLink gives them the router's typed `to`, `params`,
// and `search`, and keeps MUI's own props.
export const LinkButton = createLink(Button)
export const LinkBottomNavAction = createLink(BottomNavigationAction)
export const LinkFab = createLink(Fab)
export const LinkIconButton = createLink(IconButton)
export const LinkTab = createLink(Tab)
export const TextLink = createLink(Link)
