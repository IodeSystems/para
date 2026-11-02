package model

import (
	"encoding/json"
	"fmt"
)

// UnmarshalJSON decodes a Document. Meta is an interface, so its concrete type
// comes from the meta object's type field.
func (d *Document) UnmarshalJSON(b []byte) error {
	var w struct {
		ID     string          `json:"id"`
		Folder Folder          `json:"folder"`
		Title  string          `json:"title"`
		Body   string          `json:"body"`
		Meta   json.RawMessage `json:"meta"`
		Rev    string          `json:"rev"`
	}
	if err := json.Unmarshal(b, &w); err != nil {
		return fmt.Errorf("document: %w", err)
	}
	*d = Document{ID: w.ID, Folder: w.Folder, Title: w.Title, Body: w.Body, Rev: w.Rev}
	if len(w.Meta) == 0 || string(w.Meta) == "null" {
		return nil
	}
	var head struct {
		Type Type `json:"type"`
	}
	if err := json.Unmarshal(w.Meta, &head); err != nil {
		return fmt.Errorf("document %s: meta: %w", w.ID, err)
	}
	meta, ok := NewMeta(head.Type)
	if !ok {
		return fmt.Errorf("document %s: unknown type %q", w.ID, head.Type)
	}
	if err := json.Unmarshal(w.Meta, meta); err != nil {
		return fmt.Errorf("document %s: meta: %w", w.ID, err)
	}
	d.Meta = meta
	return nil
}
