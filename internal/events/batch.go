package events

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/iodesystems/parasolve/internal/model"
)

// Op values an import writes. From holds the batch id, To the folder.
const (
	OpImport     = "import"
	OpImportUndo = "import_undo"
)

// Rec is one record of AppendMany: the same values as AppendUpdate takes.
type Rec struct {
	Op, Doc, From, To, Hash, Prev string
}

// AppendMany writes every record with one open and one fsync, all with one
// timestamp. A batch import of thousands of documents would pay one fsync per
// record with AppendUpdate. The values are checked as in AppendUpdate; one bad
// record writes nothing.
func (l *Log) AppendMany(recs []Rec) error {
	if len(recs) == 0 {
		return nil
	}
	if !installRe.MatchString(l.src.Install) {
		return fmt.Errorf("events: append many: install id %q is not set or not a file-name token", l.src.Install)
	}
	ts := l.src.Now().UTC()
	var buf []byte
	for _, r := range recs {
		if !opRe.MatchString(r.Op) || !docRe.MatchString(r.Doc) || !tokenRe.MatchString(r.From) || !tokenRe.MatchString(r.To) || !hashRe.MatchString(r.Hash) || !hashRe.MatchString(r.Prev) {
			return fmt.Errorf("events: append many: op %q: values must be ids, enums, or hashes", r.Op)
		}
		line, err := json.Marshal(model.Event{
			TS: ts, Install: l.src.Install, Actor: l.src.Actor, Surface: l.src.Surface, Vault: l.src.Vault,
			Op: r.Op, Doc: r.Doc, From: r.From, To: r.To, Hash: r.Hash, Prev: r.Prev,
		})
		if err != nil {
			return fmt.Errorf("events: append many: %w", err)
		}
		buf = append(append(buf, line...), '\n')
	}
	name := filepath.Join(l.dir, ts.Format("2006-01")+"."+l.src.Install+".jsonl")
	f, err := os.OpenFile(name, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("events: append many: %w", err)
	}
	defer func() { _ = f.Close() }()
	if err := trimPartial(f); err != nil {
		return fmt.Errorf("events: append many: %w", err)
	}
	if _, err := f.Write(buf); err != nil {
		return fmt.Errorf("events: append many: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("events: append many: %w", err)
	}
	return nil
}
