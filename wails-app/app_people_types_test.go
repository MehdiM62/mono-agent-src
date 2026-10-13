package main

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/monoes/mono-agent/internal/storage"
)

// The app's message and status types are the CLI's JSON passed through: what
// the CLI prints for a storage record reaches the page unchanged.
func TestPersonTypesPassTheCLIShapeThrough(t *testing.T) {
	read := time.Date(2026, 9, 26, 11, 0, 0, 123, time.UTC)
	msg := storage.PersonMessage{
		ID: "m1", PersonID: "p1", Source: "outlook", ExternalID: "x1", Direction: "inbound",
		Sender: "a@b", Subject: "s", Body: "b", Metadata: `{"k":1}`, Status: "sent",
		CreatedAt: time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC), ReadAt: &read,
	}
	for _, c := range []struct {
		name string
		cli  any
		app  any
	}{
		{"message", msg, &PersonMessage{}},
		{"message unread, zero sent_at", storage.PersonMessage{ID: "m2", Direction: "outbound", Status: "draft"}, &PersonMessage{}},
		{"message with person", storage.PersonMessageWithPerson{PersonMessage: msg, PersonFullName: "A B", PersonPlatformUsername: "ab", PersonPlatform: "email"}, &PersonMessageWithPerson{}},
		{"status", storage.PersonStatusUpdate{ID: "s1", PersonID: "p1", Text: "hi", CreatedAt: read}, &PersonStatusUpdate{}},
	} {
		want, _ := json.Marshal(c.cli)
		if err := json.Unmarshal(want, c.app); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		got, _ := json.Marshal(c.app)
		var w, g any
		_ = json.Unmarshal(want, &w)
		_ = json.Unmarshal(got, &g)
		if !reflect.DeepEqual(w, g) {
			t.Errorf("%s:\n cli %s\n app %s", c.name, want, got)
		}
	}
}
