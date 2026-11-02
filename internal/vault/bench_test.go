package vault

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// seed writes n valid inbox documents directly and takes the baseline.
func seed(b *testing.B, n int) (*Vault, string) {
	b.Helper()
	v, root := newVault(b)
	for i := range n {
		body := fmt.Sprintf("---\ntype: inbox\ncreated: 2026-11-01\n---\n\n# Synthetic %d\n\nBody text %d.\n", i, i)
		p := filepath.Join(root, "inbox", fmt.Sprintf("inbox-seed%06d.md", i))
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			b.Fatal(err)
		}
	}
	if _, err := v.Reconcile(ctx); err != nil {
		b.Fatal(err)
	}
	return v, root
}

// BenchmarkWrite measures one Create or Update per op on a vault of n
// documents. Run with a fixed count so the vault is seeded once per case:
//
//	go test ./internal/vault -run xxx -bench Write -benchtime 30x
func BenchmarkWrite(b *testing.B) {
	for _, n := range []int{1000, 10000} {
		b.Run(fmt.Sprintf("create/%d", n), func(b *testing.B) {
			v, _ := seed(b, n)
			b.ResetTimer()
			for range b.N {
				if _, err := v.Create(ctx, inbox("new", "x")); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(fmt.Sprintf("update/%d", n), func(b *testing.B) {
			v, _ := seed(b, n)
			d, err := v.Get(ctx, "inbox-seed000500")
			if err != nil {
				b.Fatal(err)
			}
			b.ResetTimer()
			for i := range b.N {
				d.Body = fmt.Sprintf("edit %d", i)
				if d, err = v.Update(ctx, d); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
