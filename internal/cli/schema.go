package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/iodesystems/parasolve/internal/server"
)

const defaultSchemaPath = "ui/gen/schema.graphql"

// schemaVersion stamps nothing into the SDL, so the committed file does not
// change with each build.
const schemaVersion = "v1"

func newSchemaCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "schema", Short: "API schema tools", Args: cobra.NoArgs}
	cmd.AddCommand(&cobra.Command{
		Use:   "dump [path]",
		Short: "Write the GraphQL SDL (default " + defaultSchemaPath + "); needs no vault and no listener",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := defaultSchemaPath
			if len(args) == 1 {
				path = args[0]
			}
			s, err := server.New(server.Options{Version: schemaVersion})
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return fmt.Errorf("schema: dump: %w", err)
			}
			if err := os.WriteFile(path, []byte(s.SDL()), 0o644); err != nil {
				return fmt.Errorf("schema: dump: %w", err)
			}
			_, err = fmt.Fprintln(cmd.ErrOrStderr(), "wrote", path)
			return err
		},
	})
	return cmd
}
