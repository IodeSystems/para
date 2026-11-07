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
import ArrowDropDownIcon from '@mui/icons-material/ArrowDropDown'
import AttachFileIcon from '@mui/icons-material/AttachFile'
import MicIcon from '@mui/icons-material/Mic'
import PhotoCameraIcon from '@mui/icons-material/PhotoCamera'
import SupportAgentIcon from '@mui/icons-material/SupportAgent'
import type { Draft } from '@/components/ItemDraft'
import { ACCEPT, canSend, composerKey, fileLabel } from '@/lib/composer'

// Composer is the one box for telling an agent something: a rounded field that grows upward as the
// owner types (up to 40% of the screen, then it scrolls), + to attach files (on a phone: a photo or
// a recording too), and the send button. Enter sends, Shift-Enter starts a new line. A send needs
// words or a file. Other actions (Save as notes) sit in the menu beside the send button. The parent
// does the work: onSend gets the text and the files and says whether to clear the box.

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
  // chips sit under the box: a question's choices, or the documents to read.
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
      variant="outlined"
      data-composer
      sx={{ borderRadius: 4, px: 1, py: 0.75, bgcolor: 'background.paper' }}
    >
      {prompt && (
        <Typography
          variant="body2"
          color="text.secondary"
          data-composer-prompt
          sx={{ px: 1, pt: 0.5, pb: 0.75 }}
        >
          {prompt}
        </Typography>
      )}
      {files.length > 0 && (
        <Stack direction="row" sx={{ flexWrap: 'wrap', gap: 0.75, px: 0.5, pb: 0.75 }}>
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
      <Stack direction="row" sx={{ alignItems: 'flex-end', gap: 0.5 }}>
        {allowFiles && (
          <>
            <Tooltip title="Attach a file">
              <span>
                <IconButton
                  aria-label="Attach a file"
                  disabled={busy}
                  onClick={(e) => setAddAt(e.currentTarget)}
                  data-composer-add
                >
                  <AddIcon />
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
        <InputBase
          multiline
          fullWidth
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
            flex: 1,
            px: 1,
            py: 1,
            alignSelf: 'center',
            '& textarea': { maxHeight: '40vh', overflowY: 'auto !important' },
          }}
        />
        <Tooltip title={sendLabel}>
          <span>
            <IconButton
              color="primary"
              aria-label={sendLabel}
              disabled={!ok}
              onClick={() => void run(onSend)}
              data-composer-send
            >
              <SupportAgentIcon />
            </IconButton>
          </span>
        </Tooltip>
        {actions.length > 0 && (
          <>
            <Tooltip title="More ways to save it">
              <span>
                <IconButton
                  size="small"
                  aria-label="More ways to save it"
                  disabled={!okAct}
                  onClick={(e) => setMoreAt(e.currentTarget)}
                  data-composer-more
                  sx={{ alignSelf: 'center', ml: -0.5 }}
                >
                  <ArrowDropDownIcon />
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
      </Stack>
      {chips && (
        <Box sx={{ px: 0.5, pt: 0.5, display: 'flex', flexWrap: 'wrap', gap: 0.75 }}>{chips}</Box>
      )}
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
