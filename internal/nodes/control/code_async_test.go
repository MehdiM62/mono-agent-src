package control

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

func runCodeWith(t *testing.T, in workflow.NodeInput, code string) ([]workflow.Item, error) {
	t.Helper()
	out, err := (&CodeNode{}).Execute(context.Background(), in, map[string]interface{}{"code": code})
	if err != nil {
		return nil, err
	}
	return out[0].Items, nil
}

func TestCodeNode_AwaitsPromises(t *testing.T) {
	for name, code := range map[string]string{
		"async iife":     `(async () => { const v = await Promise.resolve(7); return [{ v }]; })()`,
		"top-level wait": `const v = await Promise.resolve(7); return [{ v }];`,
		"top-level ret":  `return [{ v: 7 }];`,
		"resolved":       `Promise.resolve({ v: 7 })`,
	} {
		items, err := runCodeWith(t, workflow.NodeInput{}, code)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(items) != 1 || fmt.Sprint(items[0].JSON["v"]) != "7" {
			t.Fatalf("%s: items %v", name, items)
		}
	}
}

func TestCodeNode_RejectedPromiseFails(t *testing.T) {
	_, err := runCodeWith(t, workflow.NodeInput{}, `(async () => { throw new Error("nope"); })()`)
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("err = %v, want the rejection", err)
	}
	_, err = runCodeWith(t, workflow.NodeInput{}, `new Promise(() => {})`)
	if err == nil || !strings.Contains(err.Error(), "never settled") {
		t.Fatalf("err = %v, want a pending-promise error", err)
	}
}

func TestCodeNode_SyntaxErrorStillReported(t *testing.T) {
	if _, err := runCodeWith(t, workflow.NodeInput{}, `[{ a: 1 }`); err == nil {
		t.Fatal("want a syntax error")
	}
}

func TestCodeNode_NodeAccessor(t *testing.T) {
	in := workflow.NodeInput{
		Items: []workflow.Item{{JSON: map[string]interface{}{"n": 1}}, {JSON: map[string]interface{}{"n": 2}}},
		NodeOutputs: map[string][]workflow.Item{
			"Fetch Rows": {{JSON: map[string]interface{}{"id": "a"}}, {JSON: map[string]interface{}{"id": "b"}}},
		},
	}
	items, err := runCodeWith(t, in, `[{
		all: $('Fetch Rows').all().length,
		first: $('Fetch Rows').first().id,
		last: $('Fetch Rows').last().id,
		item: $('Fetch Rows').item.id,
		inFirst: $input.first().n, inLast: $input.last().n, inItem: $input.item.n,
	}]`)
	if err != nil {
		t.Fatal(err)
	}
	got := fmt.Sprint(items[0].JSON)
	want := "map[all:2 first:a inFirst:1 inItem:1 inLast:2 item:a last:b]"
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
	if _, err := runCodeWith(t, in, `[{ x: $('Nope').all() }]`); err == nil || !strings.Contains(err.Error(), "Nope") {
		t.Fatalf("unknown node: err = %v", err)
	}
}
