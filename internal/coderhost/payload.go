package coderhost

import (
	"encoding/json"
	"strings"
)

// HostMade says whether a turn id is one the host makes on its own (a
// check-in), not a person's send. Such a turn never carries a mission
// confirmation.
func HostMade(turnID string) bool { return strings.HasPrefix(turnID, "checkin-") }

// payload is the structured form of the attachments column once a command has
// something besides files to carry; a plain JSON array (every older row, every
// send without a mission) is still read.
type payload struct {
	Files   []string `json:"files"`
	Mission bool     `json:"mission,omitempty"`
}

func encodePayload(files []string, mission bool) (string, error) {
	var b []byte
	var err error
	if mission {
		b, err = json.Marshal(payload{Files: append([]string{}, files...), Mission: true})
	} else {
		b, err = json.Marshal(append([]string{}, files...))
	}
	return string(b), err
}

func decodePayload(raw string) (files []string, mission bool) {
	if strings.HasPrefix(strings.TrimSpace(raw), "{") {
		var p payload
		_ = json.Unmarshal([]byte(raw), &p)
		return p.Files, p.Mission
	}
	_ = json.Unmarshal([]byte(raw), &files)
	return files, false
}
