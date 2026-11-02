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
			fs.StringVar(&in.Type, "type", "", "only this document type (task, project, note, policy ...)")
			fs.StringVar(&in.Status, "status", "", "only this status; open means any status that is not final")
			fs.StringVar(&in.Area, "area", "", "only documents linked to this area id")
			fs.StringVar(&in.Project, "project", "", "only documents linked to this project id")
			fs.StringVar(&in.Since, "since", "", "only documents updated since a date (YYYY-MM-DD) or a span (7d, 2w)")
		},
		Bind: func(args []string, in *ops.SearchInput) error {
			in.Query = strings.Join(args, " ")
			return nil
		},
		Text: func(w io.Writer, out ops.SearchOutput) error {
			if out.Degraded != "" {
				if _, err := fmt.Fprintf(w, "degraded: %s\n", out.Degraded); err != nil {
					return err
				}
			}
			for _, h := range out.Hits {
				label := h.Kind
				if h.Unverified {
					label += ", unverified"
				}
				if h.Match != "" {
					label += ", " + h.Match
				}
				if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t[%s]\n    %s\n", h.ID, h.Title, h.Path, label, h.Passage); err != nil {
					return err
				}
				if h.Via != "" {
					if _, err := fmt.Fprintf(w, "    text from: %s\n", h.Via); err != nil {
						return err
					}
				}
				if h.Caveat != "" {
					if _, err := fmt.Fprintf(w, "    caveat: %s\n", h.Caveat); err != nil {
						return err
					}
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
