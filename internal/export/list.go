package export

import "os/exec"

// Item is one format or transport and whether it works on this machine.
type Item struct {
	Name      string   `json:"name"`
	Available bool     `json:"available"`
	Needs     []string `json:"needs,omitempty"` // tools it uses, any one of them
	Found     string   `json:"found,omitempty"` // the tool detected on PATH
	Arg       string   `json:"arg,omitempty"`   // argument form, such as file:<path>
}

func firstOnPath(names ...string) string {
	for _, n := range names {
		if _, err := exec.LookPath(n); err == nil {
			return n
		}
	}
	return ""
}

func tool(name string, arg string, needs ...string) Item {
	it := Item{Name: name, Needs: needs, Arg: arg}
	if len(needs) == 0 {
		it.Available = true
		return it
	}
	it.Found = firstOnPath(needs...)
	it.Available = it.Found != ""
	return it
}

// AvailableFormats detects the formats this machine can produce.
func AvailableFormats() []Item {
	out := make([]Item, 0, len(Formats))
	for _, f := range Formats {
		switch f {
		case HTML, DOCX:
			out = append(out, tool(string(f), "", Pandoc))
		case PDF:
			it := tool(string(f), "", Pandoc)
			if it.Available {
				it.Found = pdfEngine()
				it.Available = it.Found != ""
			}
			it.Needs = append([]string{Pandoc}, pdfEngines...)
			out = append(out, it)
		default:
			out = append(out, tool(string(f), ""))
		}
	}
	return out
}

// AvailableTransports detects the transports this machine can use.
func AvailableTransports() []Item {
	return []Item{
		tool("stdout", ""),
		tool("file", "file:<path>"),
		tool("clipboard", "", "pbcopy", "wl-copy", "xclip", "clip.exe"),
		tool("osc52", ""),
		tool("tmux", "", "tmux"),
		tool("wormhole", "", "wormhole"),
		tool("qr", ""),
		tool("ssh", "ssh:<host>:<path>", "ssh"),
	}
}
