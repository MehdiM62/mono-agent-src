package apipkg

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSendsAnnotationIsChecked(t *testing.T) {
	base := func(sends string) string {
		return `{"$schema":"apipkg.v1","id":"x","title":"X","base_url":"https://x.test","operations":[{"resource":"","operation":"send","title":"Send","method":"POST","path":"/m",
			"fields":[{"key":"to","label":"To","type":"text"},{"key":"body","label":"Body","type":"text"}]` + sends + `}]}`
	}
	for sends, want := range map[string]string{
		`,"sends":{"action":"email","to":["to"],"text":["body"]}`: "",
		`,"sends":false,"sends_reason":"a queue"`:                 "",
		`,"sends":false`: "needs a sends_reason",
		`,"sends":{"action":"shout","to":["to"]}`:                                "action",
		`,"sends":{"action":"email","to":["nobody"]}`:                            `"nobody"`,
		`,"sends_reason":"why"`:                                                  "sends_reason without",
		`,"sends":{"action":"email","text":["body.reply"],"if":["to","body.x"]}`: "",
		`,"sends":{"action":"email","if":["to","nobody.x"]}`:                     `"nobody.x"`,
		`,"sends":true`: "sends",
	} {
		_, err := Parse([]byte(base(sends)), "test")
		switch {
		case want == "" && err != nil:
			t.Errorf("%s: %v", sends, err)
		case want != "" && (err == nil || !strings.Contains(err.Error(), want)):
			t.Errorf("%s: err %v, want it to mention %q", sends, err, want)
		}
	}
}

func TestSendsRoundTrips(t *testing.T) {
	for _, raw := range []string{`false`, `{"action":"dm","to":["to"],"text":["message"],"if":"go"}`, `{"action":"invite","if":["a","b.c"]}`} {
		var s Sends
		if err := json.Unmarshal([]byte(raw), &s); err != nil {
			t.Fatal(err)
		}
		b, err := json.Marshal(s)
		if err != nil || string(b) != raw {
			t.Errorf("%s → %s, %v", raw, b, err)
		}
	}
}

// An op with "if" sends when one of the params it names is set: true, or
// any value but false, "" or an empty list, a template the run has yet to
// resolve included. A dotted name is an option of a collection param.
func TestSendsIf(t *testing.T) {
	op := Operation{Sends: &Sends{Action: "invite", If: Cond{"go", "additionalFields.attendees"}}}
	for _, c := range []struct {
		config map[string]any
		want   bool
	}{
		{map[string]any{}, false},
		{map[string]any{"go": false}, false},
		{map[string]any{"go": " False "}, false},
		{map[string]any{"go": true}, true},
		{map[string]any{"go": "{{ $json.send }}"}, true},
		{map[string]any{"additionalFields": map[string]any{"attendees": ""}}, false},
		{map[string]any{"additionalFields": map[string]any{"attendees": []any{}}}, false},
		{map[string]any{"additionalFields": map[string]any{"attendees": "ada@example.test"}}, true},
	} {
		if got := op.sends(c.config); got != c.want {
			t.Errorf("sends(%v) = %v, want %v", c.config, got, c.want)
		}
	}
}
