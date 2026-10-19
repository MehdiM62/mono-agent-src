package action

import "testing"

// With SetStopAtActions, safe mode stops before the first step that touches
// a page at all, though no step marks it as a side effect, and in a loop
// body too: the marks are the recording's or the draft's, which whoever
// wrote the file chose, and even a GET or a read runs with the user's
// cookies. Steps that touch no page run.
func TestStopAtActionsStopsAtTheFirstPageAction(t *testing.T) {
	for _, typ := range []string{"click", "press_key", "select_option", "call_bot_method", "navigate", "refresh", "type", "hover",
		"scroll", "save_data", "assert", "wait_for", "find_element", "extract_text", "extract_json", "http_fetch_in_page"} {
		ae := newPkgExecutor(nil, nil)
		ae.SetSafeMode(true)
		ae.SetStopAtActions(true)
		def := &ActionDef{ActionType: "t", SideEffects: "read", Steps: []StepDef{
			{ID: "fill", Type: "set_variable", Variable: "x", Value: "1"},
			{ID: "act", Type: typ, Selector: "#save", Key: "Enter", MethodName: "send", URL: "https://example.test/logout", Method: "GET"},
			{ID: "after", Type: "log", Text: "after"},
		}}
		if _, err := ae.ExecuteDef(&StorageAction{ID: "a", TargetPlatform: "p", Type: "t"}, def); err != nil {
			t.Fatalf("%s: err = %v", typ, err)
		}
		if s := ae.SafeStopped(); s == nil || s.StepID != "act" {
			t.Errorf("%s: SafeStopped = %+v, want a stop before it", typ, s)
		}
		if ae.execCtx.GetStepResult("fill") == nil || ae.execCtx.GetStepResult("after") != nil {
			t.Errorf("%s: the steps around the stop ran wrong", typ)
		}
	}

	ae := newPkgExecutor(nil, nil)
	ae.SetSafeMode(true)
	ae.SetStopAtActions(true)
	ae.SetVariable("rows", []interface{}{"a", "b"})
	def := &ActionDef{ActionType: "t", SideEffects: "read", Steps: []StepDef{
		{ID: "each", Type: "for_each", Items: "{{rows}}", Steps: []StepDef{
			{ID: "mark", Type: "set_variable", Variable: "seen", Value: true},
			{ID: "press", Type: "press_key", Key: "Enter"},
		}},
	}}
	if _, err := ae.ExecuteDef(&StorageAction{ID: "a", TargetPlatform: "p", Type: "t"}, def); err != nil {
		t.Fatalf("loop: err = %v", err)
	}
	if s := ae.SafeStopped(); s == nil || s.StepID != "press" {
		t.Errorf("loop: SafeStopped = %+v, want a stop before the key press", s)
	}
}

// The steps that touch no page run in a stop-at-actions run, a plain wait
// included; a wait for an element reads the page and stops it.
func TestStopAtActionsRunsWhatTouchesNoPage(t *testing.T) {
	ae := newPkgExecutor(nil, nil)
	ae.SetSafeMode(true)
	ae.SetStopAtActions(true)
	def := &ActionDef{ActionType: "t", SideEffects: "read", Steps: []StepDef{
		{ID: "v", Type: "set_variable", Variable: "x", Value: "1"},
		{ID: "l", Type: "log", Text: "hi"},
		{ID: "w", Type: "wait", Duration: 0.01},
		{ID: "we", Type: "wait", Selector: "#ready"},
	}}
	if _, err := ae.ExecuteDef(&StorageAction{ID: "a", TargetPlatform: "p", Type: "t"}, def); err != nil {
		t.Fatalf("err = %v", err)
	}
	if s := ae.SafeStopped(); s == nil || s.StepID != "we" {
		t.Fatalf("SafeStopped = %+v, want a stop before the wait for an element", s)
	}
	for _, id := range []string{"v", "l", "w"} {
		if ae.execCtx.GetStepResult(id) == nil {
			t.Errorf("%s touches no page and did not run", id)
		}
	}
}
