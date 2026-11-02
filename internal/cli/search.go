package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/parasolve/internal/index"
	"github.com/iodesystems/parasolve/internal/ops"
)

func newIndexCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "index", Short: "The search index (a rebuildable cache)", Args: cobra.NoArgs}
	cmd.AddCommand(opCommand(l, ops.IndexRebuild, binding[ops.IndexRebuildInput, index.Stats]{
		Use:  "rebuild",
		Args: cobra.NoArgs,
		Text: func(w io.Writer, out index.Stats) error {
			_, err := fmt.Fprintf(w, "%d documents indexed\n", out.Documents)
			return err
		},
	}))
	return cmd
}

func newSearchCmd(l *loader) *cobra.Command {
	return opCommand(l, ops.Search, binding[ops.SearchInput, ops.SearchOutput]{
		Use:  "search <query>",
		Args: cobra.MinimumNArgs(1),
		Flags: func(fs *pflag.FlagSet, in *ops.SearchInput) {
			fs.IntVarP(&in.Limit, "limit", "n", 10, "number of hits")
		},
		Bind: func(args []string, in *ops.SearchInput) error {
			in.Query = strings.Join(args, " ")
			return nil
		},
		Text: func(w io.Writer, out ops.SearchOutput) error {
			for _, h := range out.Hits {
				label := h.Kind
				if h.Unverified {
					label += ", unverified"
				}
				if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t[%s]\n    %s\n", h.ID, h.Title, h.Path, label, h.Passage); err != nil {
					return err
				}
				if h.SourceURL != "" {
					if _, err := fmt.Fprintf(w, "    source: %s\n", h.SourceURL); err != nil {
						return err
					}
				}
			}
			return nil
		},
	})
}
