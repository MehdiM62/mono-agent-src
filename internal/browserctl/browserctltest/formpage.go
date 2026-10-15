package browserctltest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"
	"sync"

	"github.com/monoes/mono-agent/internal/browserctl"
	"github.com/monoes/mono-agent/internal/extension"
)

// FormPage stands in for the submit gate's page scripts (probe, hold,
// trigger) on a page with one form: its digest is a hash of Fields, so a
// changed value changes it the way the real in-page digest does. Install
// it with fake.On("eval_cdp", fp.Handler()).
type FormPage struct {
	mu       sync.Mutex
	Action   string
	Method   string
	Selector string
	// Submit: the probed control is submit-like.
	Submit bool
	Fields []browserctl.FormField
	// Triggered counts submits the trigger script made.
	Triggered int
}

// Set changes one field's value.
func (fp *FormPage) Set(name, value string) {
	fp.mu.Lock()
	defer fp.mu.Unlock()
	for i := range fp.Fields {
		if fp.Fields[i].Name == name {
			fp.Fields[i].Value = value
		}
	}
}

func (fp *FormPage) digest() string {
	raw, _ := json.Marshal(fp.Fields)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (fp *FormPage) form() map[string]any {
	return map[string]any{"action": fp.Action, "method": fp.Method, "selector": fp.Selector, "fields": fp.Fields}
}

var expectRE = regexp.MustCompile(`const EXPECT = "([0-9a-f]*)";`)

// Handler answers eval_cdp for the three scripts; anything else gets null.
func (fp *FormPage) Handler() Handler {
	return func(c *extension.Command) (any, error) {
		fp.mu.Lock()
		defer fp.mu.Unlock()
		expr, _ := c.Params["expression"].(string)
		var res any
		switch {
		case strings.HasPrefix(expr, "/*mono:probe*/"):
			why := ""
			if fp.Submit {
				why = "type=submit"
			}
			res = map[string]any{"found": true, "submit": fp.Submit, "why": why, "form": fp.form()}
		case strings.HasPrefix(expr, "/*mono:hold*/"):
			res = map[string]any{"found": true, "form": fp.form(), "digest": fp.digest()}
		case strings.HasPrefix(expr, "/*mono:trigger*/"):
			m := expectRE.FindStringSubmatch(expr)
			if m != nil && m[1] != "" && m[1] != fp.digest() {
				res = map[string]any{"changed": true}
			} else {
				fp.Triggered++
				res = map[string]any{"ok": true}
			}
		}
		return map[string]any{"result": res}, nil
	}
}
