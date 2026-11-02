package model

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// SetExtraNode sets a frontmatter key that Meta does not declare, keeping the
// node's form (a date stays a plain date). The key is written after the typed
// fields, in sorted order.
func (d *Document) SetExtraNode(key string, n yaml.Node) {
	if d.extra == nil {
		d.extra = map[string]yaml.Node{}
	}
	d.extra[key] = n
}

// SetExtra encodes v and sets it as an undeclared frontmatter key.
func (d *Document) SetExtra(key string, v any) error {
	var n yaml.Node
	if err := n.Encode(v); err != nil {
		return fmt.Errorf("model: set extra %s: %w", key, err)
	}
	d.SetExtraNode(key, n)
	return nil
}
