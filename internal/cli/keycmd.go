package cli

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/iodesystems/para/internal/keys"
	"github.com/iodesystems/para/internal/netguard"
	"github.com/iodesystems/para/internal/server"
)

// newKeyCmd manages keys on this machine, in the vault's server directory. It
// needs no running server: a revoke applies to the server's next request.
func newKeyCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "key",
		Short: "Create, list, and revoke API keys of a vault (local only)",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(newKeyCreateCmd(l), newKeyListCmd(l), newKeyRevokeCmd(l))
	return cmd
}

func (l *loader) keyStore() (*keys.Store, string, error) {
	if err := l.load(); err != nil {
		return nil, "", err
	}
	sel, err := l.selectVault()
	if err != nil {
		return nil, "", err
	}
	return keys.New(server.ServerDir(l.install, sel.ID)), sel.ID, nil
}

// parseTTL reads a Go duration, or a number of days as 30d.
func parseTTL(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}
	if d, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.Atoi(d)
		if err != nil || n <= 0 {
			return 0, fmt.Errorf("key: bad --ttl %q", s)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	v, err := time.ParseDuration(s)
	if err != nil || v <= 0 {
		return 0, fmt.Errorf("key: bad --ttl %q: use 720h or 30d", s)
	}
	return v, nil
}

func newKeyCreateCmd(l *loader) *cobra.Command {
	var (
		scopes, allowIP []string
		ttl, base       string
		printURL        bool
	)
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a key; the token is printed once",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if base != "" {
				if err := l.load(); err != nil {
					return err
				}
				id, err := vaultForURL(l, base)
				if err != nil {
					return err
				}
				if l.vaultFlag != "" && l.vaultFlag != id {
					return fmt.Errorf("key: --url %s names vault %s, not --vault %s", base, id, l.vaultFlag)
				}
				l.vaultFlag = id
			}
			store, id, err := l.keyStore()
			if err != nil {
				return err
			}
			for _, s := range scopes {
				if !validScope(s) {
					return fmt.Errorf("key: unknown scope %q: one of %s", s, strings.Join(netguard.Scopes, ", "))
				}
			}
			d, err := parseTTL(ttl)
			if err != nil {
				return err
			}
			k, tok, err := store.Create(args[0], scopes, allowIP, d)
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			asJSON, _ := cmd.Flags().GetBool("json")
			if asJSON {
				enc := json.NewEncoder(w)
				enc.SetIndent("", "  ")
				return enc.Encode(struct {
					Name   string   `json:"name"`
					Scopes []string `json:"scopes"`
					Token  string   `json:"token"`
				}{k.Name, k.Scopes, tok})
			}
			if printURL {
				if base == "" {
					base = l.vaultBase(id)
				}
				_, err = fmt.Fprintln(w, signInURL(base, tok))
				return err
			}
			_, err = fmt.Fprintln(w, tok)
			return err
		},
	}
	addJSONFlag(cmd)
	f := cmd.Flags()
	f.StringArrayVar(&scopes, "scope", nil, "scope to grant (repeatable): "+strings.Join(netguard.Scopes, ", "))
	f.StringArrayVar(&allowIP, "allow-ip", nil, "address or CIDR this key can come from (repeatable)")
	f.StringVar(&ttl, "ttl", "", "life of the key: 720h or 30d (default: no expiry)")
	f.BoolVar(&printURL, "print-url", false, "print a sign-in URL with the token in its fragment")
	f.StringVar(&base, "url", "", "the vault's address, such as https://life.example.com: the key is for the vault its host names (and the base of --print-url)")
	return cmd
}

func validScope(s string) bool {
	for _, v := range netguard.Scopes {
		if v == s {
			return true
		}
	}
	return false
}

func newKeyListCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List keys; hashes and tokens are never shown",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, _, err := l.keyStore()
			if err != nil {
				return err
			}
			list, err := store.List()
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
				out := make([]keys.Key, len(list))
				for i, k := range list {
					k.Hash = ""
					out[i] = k
				}
				enc := json.NewEncoder(w)
				enc.SetIndent("", "  ")
				return enc.Encode(out)
			}
			p := &printer{w: w}
			for _, k := range list {
				state := "active"
				if k.Revoked != nil {
					state = "revoked"
				} else if k.Expires != nil {
					state = "expires " + k.Expires.Format(time.RFC3339)
				}
				p.f("%s\t%s\t%s\t%s\n", k.Name, strings.Join(k.Scopes, ","), strings.Join(k.AllowIP, ","), state)
			}
			return p.err
		},
	}
	addJSONFlag(cmd)
	return cmd
}

func newKeyRevokeCmd(l *loader) *cobra.Command {
	return &cobra.Command{
		Use:   "revoke <name>",
		Short: "Revoke a key and every session made from it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, _, err := l.keyStore()
			if err != nil {
				return err
			}
			if _, err := store.Revoke(args[0]); err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), "revoked", args[0])
			return err
		},
	}
}

// vaultForURL is the vault the server routes the host of base to, by
// net.base_hosts and the bare vault, as para serve does.
func vaultForURL(l *loader, base string) (string, error) {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("key: --url %q: want a URL such as https://life.example.com", base)
	}
	cfg, err := loadServeConfig(l, serveFlags{})
	if err != nil {
		return "", err
	}
	id, ok := netguard.NewRouter(cfg.bases, cfg.bare, nil).Route(u.Host)
	if !ok {
		return "", fmt.Errorf("key: --url %s: its host names no vault of this install (net.base_hosts: %s); pass --vault <id>", base, strings.Join(cfg.bases, ", "))
	}
	if _, err := l.install.Select(id, ""); err != nil {
		return "", fmt.Errorf("key: --url %s names vault %s: %w", base, id, err)
	}
	return id, nil
}
