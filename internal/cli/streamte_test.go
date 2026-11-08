package cli

// streamTEParityCases are the parity rows of stream TE: what hangs on a
// theme, and closing it. A tag nothing has changes nothing, so the JSON and
// the CLI runs agree.
func streamTEParityCases() map[string]parityCase {
	return map[string]parityCase{
		"theme_items": {`{"theme":"#te-none"}`, []string{"theme", "items", "#te-none"}, false},
		"theme_close": {`{"theme":"#te-none"}`, []string{"theme", "close", "#te-none"}, false},
	}
}
