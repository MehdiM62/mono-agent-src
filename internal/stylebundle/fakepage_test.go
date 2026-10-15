package stylebundle

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
)

// A recorded browser exchange: what live_test.go saw a real tab answer,
// replayed by fakePage so Build is tested without a browser.
type recordedCall struct {
	Key    string      `json:"key"`
	Result interface{} `json:"result,omitempty"`
	Error  string      `json:"error,omitempty"`
}

var (
	markerRe   = regexp.MustCompile(`^/\*stylebundle:([a-z]+)\*/`)
	selectorRe = regexp.MustCompile(`querySelector\(("(?:[^"\\]|\\.)*")\)`)
)

// callKey names a call independently of the JavaScript's exact source: CDP
// by method and parameters, an evaluation by its reader and selector.
func cdpKey(method string, params map[string]interface{}) string {
	if params == nil {
		params = map[string]interface{}{}
	}
	b, _ := json.Marshal(params)
	return "cdp " + method + " " + string(b)
}

func evalKey(expr string) string {
	name := "?"
	if m := markerRe.FindStringSubmatch(expr); m != nil {
		name = m[1]
	}
	sel := ""
	if m := selectorRe.FindStringSubmatch(expr); m != nil {
		sel = " " + m[1]
	}
	return "eval " + name + sel
}

// recorder wraps a live Page and keeps every exchange.
type recorder struct {
	Page
	mu    sync.Mutex
	calls []recordedCall
}

func (r *recorder) keep(key string, res interface{}, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c := recordedCall{Key: key, Result: res}
	if err != nil {
		c.Error = err.Error()
	}
	r.calls = append(r.calls, c)
}

func (r *recorder) CDP(method string, params map[string]interface{}) (map[string]interface{}, error) {
	res, err := r.Page.CDP(method, params)
	r.keep(cdpKey(method, params), res, err)
	return res, err
}

func (r *recorder) EvalCDP(js string) (interface{}, error) {
	res, err := r.Page.EvalCDP(js)
	r.keep(evalKey(js), res, err)
	return res, err
}

// save writes one call per line, so a re-recording diffs by call.
func (r *recorder) save(path string) error {
	var b []byte
	for i, c := range r.calls {
		line, err := json.Marshal(c)
		if err != nil {
			return err
		}
		sep := ",\n"
		if i == 0 {
			sep = "[\n"
		}
		b = append(append(b, sep...), line...)
	}
	return os.WriteFile(path, append(b, "\n]\n"...), 0o644)
}

// fakePage answers each key with its recorded results, in recorded order.
type fakePage struct {
	queues map[string][]recordedCall
	asked  []string
}

func newFakePage(calls []recordedCall) *fakePage {
	f := &fakePage{queues: map[string][]recordedCall{}}
	for _, c := range calls {
		f.queues[c.Key] = append(f.queues[c.Key], c)
	}
	return f
}

func loadReplay(t testing.TB, path string) *fakePage {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s (record it with live_test.go): %v", path, err)
	}
	var calls []recordedCall
	if err := json.Unmarshal(b, &calls); err != nil {
		t.Fatal(err)
	}
	return newFakePage(calls)
}

func (f *fakePage) next(key string) (interface{}, error) {
	f.asked = append(f.asked, key)
	q := f.queues[key]
	if len(q) == 0 {
		return nil, fmt.Errorf("fakePage: nothing recorded for %s", key)
	}
	c := q[0]
	if len(q) > 1 {
		f.queues[key] = q[1:]
	}
	if c.Error != "" {
		return nil, errors.New(c.Error)
	}
	return c.Result, nil
}

func (f *fakePage) CDP(method string, params map[string]interface{}) (map[string]interface{}, error) {
	res, err := f.next(cdpKey(method, params))
	if err != nil {
		return nil, err
	}
	m, _ := res.(map[string]interface{})
	if m == nil {
		m = map[string]interface{}{}
	}
	return m, nil
}

func (f *fakePage) EvalCDP(js string) (interface{}, error) { return f.next(evalKey(js)) }

func (f *fakePage) askedFor(prefix string) int {
	n := 0
	for _, k := range f.asked {
		if strings.HasPrefix(k, prefix) {
			n++
		}
	}
	return n
}
