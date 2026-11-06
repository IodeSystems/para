import {
  createContext,
  useContext,
  useState,
  type Dispatch,
  type ReactNode,
  type SetStateAction,
} from 'react'

// ItemDraft is what the owner has typed in an item's composer, shared with the item's buttons: Done
// and Investigate near the composer take its words and files as a note. A row with no composer has
// no draft, and its buttons act with no note.

export type Draft = {
  id: string
  text: string
  setText: Dispatch<SetStateAction<string>>
  files: File[]
  setFiles: Dispatch<SetStateAction<File[]>>
  clear: () => void
}

const Ctx = createContext<Draft | null>(null)

export function ItemDraftProvider({ id, children }: { id: string; children: ReactNode }) {
  const [text, setText] = useState('')
  const [files, setFiles] = useState<File[]>([])
  const clear = () => {
    setText('')
    setFiles([])
  }
  return (
    <Ctx.Provider value={{ id, text, setText, files, setFiles, clear }}>{children}</Ctx.Provider>
  )
}

// useItemDraft is the draft of the item with this id, or null when none is above.
export function useItemDraft(id: string): Draft | null {
  const d = useContext(Ctx)
  return d && d.id === id ? d : null
}
