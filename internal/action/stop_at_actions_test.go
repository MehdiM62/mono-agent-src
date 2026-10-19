package action

import "testing"

// With SetStopAtActions, safe mode stops before the first click, key press,
// option pick or bot call, though no step marks it as a side effect, and in
// a loop body too: the marks are the recording's or the draft's, which
// whoever wrote the file chose.
func TestStopAtActionsStopsAtTheFirstPageAction(t *testing.T) {
	for _, typ := range []string{"click", "press_key", "select_option", "call_bot_method"} {
		ae := newPkgExecutor(nil, nil)
		ae.SetSafeMode(true)
		ae.SetStopAtActions(true)
		def := &ActionDef{ActionType: "t", SideEffects: "read", Steps: []StepDef{
			{ID: "fill", Type: "set_variable", Variable: "x", Value: "1"},
			{ID: "act", Type: typ, Selector: "#save", Key: "Enter", MethodName: "send"},
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
