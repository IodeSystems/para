package model

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestDocumentJSONRoundTrip(t *testing.T) {
	for _, in := range []Document{
		{ID: "rfx-one", Folder: FolderRFX, Title: "Ask", Body: "text\n", Rev: "sha256:ab",
			Meta: &RFX{Common: Common{Type: TypeRFX}, Direction: RFXOut, Status: RFXOpen, Route: "domain-a", SelfServeCheck: "wiki"}},
		{ID: "no-meta", Folder: FolderInbox, Title: "Bare"},
	} {
		b, err := json.Marshal(in)
		if err != nil {
			t.Fatal(err)
		}
		var out Document
		if err := json.Unmarshal(b, &out); err != nil {
			t.Fatalf("%s: %v", in.ID, err)
		}
		if !reflect.DeepEqual(in, out) {
			t.Errorf("%s:\n got %+v\nwant %+v", in.ID, out, in)
		}
	}
}

func TestDocumentJSONUnknownType(t *testing.T) {
	var d Document
	if err := json.Unmarshal([]byte(`{"id":"x","meta":{"type":"nope"}}`), &d); err == nil {
		t.Fatal("want an error for an unknown type")
	}
}
