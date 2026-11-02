package ops

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/iodesystems/paradize/internal/model"
	"github.com/iodesystems/paradize/internal/store"
)

// fakeStore proves ops need only the interface.
type fakeStore struct {
	store.Store
	created []model.Document
	err     error
}

func (f *fakeStore) Create(_ context.Context, d model.Document) (model.Document, error) {
	if f.err != nil {
		return d, f.err
	}
	f.created = append(f.created, d)
	d.ID, d.Rev = "inbox-x", "sha256:x"
	return d, nil
}

func TestSplitCapture(t *testing.T) {
	long := strings.Repeat("é", 100)
	cases := []struct {
		name, text, title, body string
		wantErr                 bool
	}{
		{"one line", "buy milk", "buy milk", "", false},
		{"outer space", "  buy milk \n", "buy milk", "", false},
		{"two lines", "title\n\nmore text\n", "title", "more text", false},
		{"crlf", "title\r\nmore", "title", "more", false},
		{"inner runs", "a   b\tc", "a b c", "", false},
		{"long line", long, strings.Repeat("é", 80) + "...", long, false},
		{"empty", "", "", "", true},
		{"blank", " \n ", "", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			title, body, err := splitCapture(c.text)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v", err)
			}
			if title != c.title || body != c.body {
				t.Fatalf("title %q body %q", title, body)
			}
		})
	}
}

func TestInboxAdd(t *testing.T) {
	s := &fakeStore{}
	out, err := InboxAdd.Run(context.Background(), Deps{Store: s}, InboxAddInput{Text: "buy milk"})
	if err != nil {
		t.Fatal(err)
	}
	if out.ID != "inbox-x" || len(s.created) != 1 || s.created[0].Folder != model.FolderInbox {
		t.Fatalf("out %+v created %+v", out, s.created)
	}
	if s.created[0].Meta.Header().Type != model.TypeInbox {
		t.Errorf("meta = %+v", s.created[0].Meta)
	}
}

func TestRunWrapsErrors(t *testing.T) {
	s := &fakeStore{err: model.ErrExists}
	_, err := InboxAdd.Run(context.Background(), Deps{Store: s}, InboxAddInput{Text: "x"})
	if !errors.Is(err, model.ErrExists) || !strings.HasPrefix(err.Error(), "inbox_add: ") {
		t.Fatalf("err = %v", err)
	}
}

func TestInvoke(t *testing.T) {
	cases := []struct {
		name, input string
		wantErr     bool
	}{
		{"valid", `{"text":"buy milk"}`, false},
		{"unknown field", `{"text":"x","nope":1}`, true},
		{"wrong type", `{"text":1}`, true},
		{"empty means zero input", ``, true}, // inbox_add refuses empty text
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := InboxAdd.Invoke(context.Background(), Deps{Store: &fakeStore{}}, json.RawMessage(c.input))
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v", err)
			}
			if err == nil {
				if _, ok := got.(InboxAddOutput); !ok {
					t.Fatalf("got %T", got)
				}
			}
		})
	}
}

func TestRegistry(t *testing.T) {
	all := All()
	var names []string
	for _, e := range all {
		names = append(names, e.Name())
		if e.Description() == "" {
			t.Errorf("%s has no description", e.Name())
		}
		for _, typ := range []reflect.Type{e.InputType(), e.OutputType()} {
			checkJSONTags(t, e.Name(), typ)
		}
	}
	if !sort.StringsAreSorted(names) {
		t.Errorf("All is not sorted: %v", names)
	}
	for _, want := range []string{"events_tail", "inbox_add", "lint", "reconcile"} {
		if !slices.Contains(names, want) {
			t.Errorf("op %s is not registered", want)
		}
	}
}

// Every exported field of an op's Input and Output needs a JSON tag: the JSON
// form is the contract between surfaces.
func checkJSONTags(t *testing.T, op string, typ reflect.Type) {
	t.Helper()
	if typ.Kind() != reflect.Struct {
		t.Errorf("%s: %s is not a struct", op, typ)
		return
	}
	for i := range typ.NumField() {
		f := typ.Field(i)
		if f.IsExported() && f.Tag.Get("json") == "" {
			t.Errorf("%s: %s.%s has no json tag", op, typ, f.Name)
		}
	}
}

func TestDefinePanicsOnBadNames(t *testing.T) {
	fn := func(context.Context, Deps, struct{}) (struct{}, error) { return struct{}{}, nil }
	for _, name := range []string{"Bad", "has space", "inbox_add", "dotted.name"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%q: want panic", name)
				}
			}()
			define(name, "d", fn)
		}()
	}
}

func TestLintOutputFailed(t *testing.T) {
	cases := []struct {
		name     string
		findings []model.Finding
		failed   bool
	}{
		{"clean", nil, false},
		{"warning", []model.Finding{{Severity: "warn"}}, false},
		{"error", []model.Finding{{Severity: "warn"}, {Severity: "error"}}, true},
	}
	for _, c := range cases {
		out := NewLintOutput(c.findings)
		if (out.Failed() != nil) != c.failed {
			t.Errorf("%s: Failed = %v", c.name, out.Failed())
		}
		if out.Findings == nil {
			t.Errorf("%s: nil findings encode as null", c.name)
		}
	}
}
