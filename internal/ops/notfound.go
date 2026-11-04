package ops

import (
	"errors"
	"fmt"

	"github.com/iodesystems/para/internal/model"
)

// notFoundError is the refusal of a link argument that names no document. It
// names the field and the way to find a real id. It unwraps to
// model.ErrNotFound.
type notFoundError struct {
	field, id, hint string
}

func (e *notFoundError) Error() string {
	return fmt.Sprintf("%s %q not found; %s", e.field, e.id, e.hint)
}

func (e *notFoundError) Unwrap() error { return model.ErrNotFound }

// notFoundHints says, per document kind, which tools find an id.
var notFoundHints = map[string]string{
	"project":  "list goals with project_list or search with similar",
	"area":     "list areas with area_list or search with similar",
	"task":     "list tasks with task_list or search with similar",
	"incident": "list incidents with incident_list or search with similar",
	"question": "list questions with question_list or search with similar",
	"rfx":      "list requests with rfx_list or search with similar",
}

// missing turns a not-found error from a store read into the notFoundError of
// kind. Any other error passes through. It is the one place link arguments
// get this message: the get<Kind> helpers call it.
func missing(kind, id string, err error) error {
	if err == nil || !errors.Is(err, model.ErrNotFound) {
		return err
	}
	return &notFoundError{field: kind, id: id, hint: notFoundHints[kind]}
}

// linkErr wraps an error from reading the document that field names. A
// not-found error says field instead of the document kind.
func linkErr(field, id string, err error) error {
	var nf *notFoundError
	if errors.As(err, &nf) {
		return &notFoundError{field: field, id: nf.id, hint: nf.hint}
	}
	if errors.Is(err, model.ErrNotFound) {
		hint, ok := notFoundHints[field]
		if !ok {
			hint = "search with similar"
		}
		return &notFoundError{field: field, id: id, hint: hint}
	}
	return fmt.Errorf("%s %s: %w", field, id, err)
}
