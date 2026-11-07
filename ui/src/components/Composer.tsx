import { useEffect, useRef, useState, type ReactNode } from 'react'
import Box from '@mui/material/Box'
import Chip from '@mui/material/Chip'
import IconButton from '@mui/material/IconButton'
import InputBase from '@mui/material/InputBase'
import ListItemIcon from '@mui/material/ListItemIcon'
import ListItemText from '@mui/material/ListItemText'
import Menu from '@mui/material/Menu'
import MenuItem from '@mui/material/MenuItem'
import Paper from '@mui/material/Paper'
import Stack from '@mui/material/Stack'
import Tooltip from '@mui/material/Tooltip'
import Typography from '@mui/material/Typography'
import AddIcon from '@mui/icons-material/Add'
import ArrowUpwardIcon from '@mui/icons-material/ArrowUpwardRounded'
import AttachFileIcon from '@mui/icons-material/AttachFile'
import ExpandMoreIcon from '@mui/icons-material/ExpandMore'
import MicIcon from '@mui/icons-material/Mic'
import PhotoCameraIcon from '@mui/icons-material/PhotoCamera'
import type { Draft } from '@/components/ItemDraft'
import { ACCEPT, canSend, composerKey, fileLabel } from '@/lib/composer'

// Composer is the one box for telling an agent something: a large rounded card, the text on top
// that grows upward as the owner types (up to half the screen, then it scrolls), and a toolbar
// under it: + to attach files (on a phone: a photo or a recording too), the chips, the menu of
// other actions (Save as notes), and the round send button. Enter sends, Shift-Enter starts a new
// line. A send needs words or a file. The parent does the work: onSend gets the text and the
// files and says whether to clear the box.

export type ComposerAction = {
  label: string
  // hint is a second line, such as why the action is off.
  hint?: string
  disabled?: boolean
  run: (text: string, files: File[]) => Promise<boolean>
}

export function Composer({
  label,
  placeholder = 'Tell the agent what happened or what to do',
  prompt,
  sendLabel = 'Send to agent',
  onSend,
  actions = [],
  chips,
  actionsReady = false,
  files: allowFiles = true,
  autoFocus,
  onCancel,
  draft,
}: {
  label: string
  placeholder?: string
  // prompt is what the item asks of the owner, shown above the box.
  prompt?: string | null
  sendLabel?: string
  onSend: (text: string, files: File[]) => Promise<boolean>
  actions?: ComposerAction[]
  // chips sit in the toolbar under the text: a question's choices, or the documents to read.
  chips?: ReactNode
  // actionsReady lets the menu actions run with an empty box (a picked choice stands for the words).
  actionsReady?: boolean
  files?: boolean
  autoFocus?: boolean
  // onCancel runs on Escape.
  onCancel?: () => void
  // draft holds the words and files when the item's buttons share them (ItemDraft); without it
  // the box keeps its own.
  draft?: Draft | null
}) {
  const [ownText, setOwnText] = useState('')
  const [ownFiles, setOwnFiles] = useState<File[]>([])
  const text = draft ? draft.text : ownText
  const setText = draft ? draft.setText : setOwnText
  const files = draft ? draft.files : ownFiles
  const setFiles = draft ? draft.setFiles : setOwnFiles
  const [busy, setBusy] = useState(false)
  const [addAt, setAddAt] = useState<HTMLElement | null>(null)
  const [moreAt, setMoreAt] = useState<HTMLElement | null>(null)
  const picker = useRef<HTMLInputElement>(null)
  const ok = canSend(text, files.length, busy)

  const pick = (accept: string, capture?: string) => {
    setAddAt(null)
    const el = picker.current
    if (!el) return
    el.accept = accept
    if (capture) el.setAttribute('capture', capture)
    else el.removeAttribute('capture')
    el.click()
  }
  const okAct = ok || (actionsReady && !busy)
  const run = async (fn: (t: string, f: File[]) => Promise<boolean>, can = ok) => {
    if (!can) return
    setBusy(true)
    try {
      if (await fn(text.trim(), files)) {
        setText('')
        setFiles([])
      }
    } finally {
      setBusy(false)
    }
  }

  return (
    <Paper
      elevation={0}
      data-composer
      sx={(t) => {
        // Every theme has CSS variables; the channel lets the ring take the accent at a low alpha.
        const accent = t.vars?.palette.primary.mainChannel ?? '0 0 0'
        return {
          borderRadius: '24px',
          border: 1,
          borderColor: 'divider',
          boxShadow: '0 1px 2px rgba(0, 0, 0, 0.05), 0 8px 28px rgba(0, 0, 0, 0.09)',
          transition: 'border-color 120ms ease, box-shadow 120ms ease',
          ...t.applyStyles('dark', {
            boxShadow: '0 0 0 1px rgba(255, 255, 255, 0.03), 0 10px 36px rgba(0, 0, 0, 0.5)',
          }),
          // The card shows the focus; the text inside needs no ring of its own.
          '&:focus-within': {
            borderColor: `rgba(${accent} / 0.6)`,
            boxShadow: `0 0 0 3px rgba(${accent} / 0.16), 0 8px 28px rgba(0, 0, 0, 0.12)`,
          },
          '& textarea:focus-visible': { outline: 'none' },
          '@media (prefers-reduced-motion: reduce)': { transition: 'none' },
        }
      }}
    >
      {prompt && (
        <Typography
          variant="body2"
          color="text.secondary"
          data-composer-prompt
          sx={{ px: 2.25, pt: 1.5, overflowWrap: 'anywhere' }}
        >
          {prompt}
        </Typography>
      )}
      {files.length > 0 && (
        <Stack direction="row" sx={{ flexWrap: 'wrap', gap: 0.75, px: 1.75, pt: 1.25 }}>
          {files.map((f, i) => (
            <Chip
              key={`${f.name}-${f.size}-${f.lastModified}-${i}`}
              size="small"
              avatar={f.type.startsWith('image/') ? <Thumb file={f} /> : undefined}
              label={fileLabel(f)}
              onDelete={() => setFiles((l) => l.filter((_, j) => j !== i))}
              data-composer-file
              sx={{ maxWidth: 260 }}
            />
          ))}
        </Stack>
      )}
      {/* The text is on top and grows as lines are added, up to half the screen, then scrolls.
          The box is anchored at the bottom (sticky), so it grows upward. */}
      <InputBase
        multiline
        fullWidth
        minRows={1}
        autoFocus={autoFocus}
        value={text}
        placeholder={placeholder}
        inputProps={{ 'aria-label': label }}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Escape' && onCancel) return onCancel()
          if (
            composerKey({
              key: e.key,
              shiftKey: e.shiftKey,
              isComposing: e.nativeEvent.isComposing,
            })
          ) {
            e.preventDefault()
            void run(onSend)
          }
        }}
        sx={{
          display: 'block',
          px: 2.25,
          pt: 1.75,
          pb: 0.5,
          fontSize: '1rem',
          lineHeight: 1.5,
          '& textarea': {
            maxHeight: '50vh',
            overflowY: 'auto !important',
            transition: 'height 140ms ease-out',
            '@media (prefers-reduced-motion: reduce)': { transition: 'none' },
          },
          '& textarea::placeholder': { color: 'text.secondary', opacity: 0.85 },
        }}
      />
      <Stack
        direction="row"
        sx={{ alignItems: 'flex-end', gap: 0.5, px: 1, pb: 1, pt: 0.5, minWidth: 0 }}
      >
        {allowFiles && (
          <>
            <Tooltip title="Attach a file">
              <span>
                <IconButton
                  aria-label="Attach a file"
                  disabled={busy}
                  onClick={(e) => setAddAt(e.currentTarget)}
                  data-composer-add
                  sx={{ border: 1, borderColor: 'divider', width: 36, height: 36 }}
                >
                  <AddIcon fontSize="small" />
                </IconButton>
              </span>
            </Tooltip>
            <Menu anchorEl={addAt} open={!!addAt} onClose={() => setAddAt(null)}>
              <MenuItem onClick={() => pick(ACCEPT)}>
                <ListItemIcon>
                  <AttachFileIcon fontSize="small" />
                </ListItemIcon>
                <ListItemText primary="Files" secondary="Images, audio, video, PDF, text" />
              </MenuItem>
              <MenuItem onClick={() => pick('image/*,video/*', 'environment')}>
                <ListItemIcon>
                  <PhotoCameraIcon fontSize="small" />
                </ListItemIcon>
                <ListItemText primary="Camera" />
              </MenuItem>
              <MenuItem onClick={() => pick('audio/*', 'user')}>
                <ListItemIcon>
                  <MicIcon fontSize="small" />
                </ListItemIcon>
                <ListItemText primary="Record audio" />
              </MenuItem>
            </Menu>
            <input
              ref={picker}
              type="file"
              multiple
              hidden
              accept={ACCEPT}
              data-composer-input
              onChange={(e) => {
                const picked = Array.from(e.target.files ?? [])
                e.target.value = ''
                if (picked.length) setFiles((l) => [...l, ...picked])
              }}
            />
          </>
        )}
        {/* The chips sit in the toolbar: a question's choices, or the documents to read. */}
        <Box
          sx={{
            flex: 1,
            minWidth: 0,
            display: 'flex',
            flexWrap: 'wrap',
            alignItems: 'center',
            alignSelf: 'center',
            gap: 0.75,
            px: 0.5,
          }}
        >
          {chips}
        </Box>
        {actions.length > 0 && (
          <>
            <Tooltip title="More ways to save it">
              <span>
                <IconButton
                  aria-label="More ways to save it"
                  disabled={!okAct}
                  onClick={(e) => setMoreAt(e.currentTarget)}
                  data-composer-more
                  sx={{ width: 36, height: 36 }}
                >
                  <ExpandMoreIcon />
                </IconButton>
              </span>
            </Tooltip>
            <Menu anchorEl={moreAt} open={!!moreAt} onClose={() => setMoreAt(null)}>
              {actions.map((a) => (
                <MenuItem
                  key={a.label}
                  disabled={a.disabled}
                  data-composer-action={a.label}
                  onClick={() => {
                    setMoreAt(null)
                    void run(a.run, okAct)
                  }}
                >
                  <ListItemText primary={a.label} secondary={a.hint} />
                </MenuItem>
              ))}
            </Menu>
          </>
        )}
        <Tooltip title={sendLabel}>
          <span>
            <IconButton
              aria-label={sendLabel}
              disabled={!ok}
              onClick={() => void run(onSend)}
              data-composer-send
              sx={{
                width: 36,
                height: 36,
                bgcolor: 'primary.main',
                color: 'primary.contrastText',
                transition: 'background-color 120ms ease, transform 120ms ease',
                '&:hover': { bgcolor: 'primary.dark' },
                '&:active': { transform: 'scale(0.94)' },
                '&.Mui-disabled': {
                  bgcolor: 'action.disabledBackground',
                  color: 'action.disabled',
                },
                '@media (prefers-reduced-motion: reduce)': { transition: 'none' },
              }}
            >
              <ArrowUpwardIcon fontSize="small" />
            </IconButton>
          </span>
        </Tooltip>
      </Stack>
    </Paper>
  )
}

// Thumb is a picked image's preview, as a data: URL: the CSP allows data: images, not blob:.
function Thumb({ file }: { file: File }) {
  const [url, setUrl] = useState<string | null>(null)
  useEffect(() => {
    if (file.size > THUMB_MAX) return
    const r = new FileReader()
    r.onload = () => setUrl(typeof r.result === 'string' ? r.result : null)
    r.readAsDataURL(file)
    return () => r.abort()
  }, [file])
  return url ? <Box component="img" src={url} alt="" sx={{ objectFit: 'cover' }} /> : <span />
}

// THUMB_MAX: a larger photo shows no preview, so a phone does not hold it twice in memory.
const THUMB_MAX = 8 << 20
