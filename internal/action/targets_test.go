package action

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/rs/zerolog"
)

// sendBot is a fake bot whose "send" method fails for the targets in fail
// and returns a result record for the others.
type sendBot struct {
	fail map[string]bool
	sent []string
}

func (b *sendBot) GetMethodByName(name string) (func(context.Context, ...interface{}) (interface{}, error), bool) {
	if name != "send" {
		return nil, false
	}
	return func(_ context.Context, args ...interface{}) (interface{}, error) {
		to := fmt.Sprint(args[1])
		if b.fail[to] {
			return nil, errors.New("send button never appeared")
		}
		b.sent = append(b.sent, to)
		return map[string]interface{}{"to": to, "status": "sent"}, nil
	}, true
}

func runTargets(t *testing.T, bot *sendBot, def *ActionDef, targets ...interface{}) (*ExecutionResult, error) {
	t.Helper()
	ae := NewActionExecutor(context.Background(), nil, nil, nil, nil, bot, zerolog.Nop())
	ae.SetVariable("selectedListItems", targets)
	ae.SetVariable("targets", targets)
	return ae.ExecuteDef(&StorageAction{ID: "a", TargetPlatform: "p", Type: "t"}, def)
}

func sendLoopDef(onErr *ErrorHandlerDef, extra ...StepDef) *ActionDef {
	steps := append([]StepDef{{ID: "send", Type: "call_bot_method", MethodName: "send",
		Args: []interface{}{"{{item}}"}, OnError: onErr, SideEffect: true}}, extra...)
	ids := make([]string, len(steps))
	for i, s := range steps {
		ids[i] = s.ID
	}
	return &ActionDef{ActionType: "t", SideEffects: "message", Steps: steps,
		Loops: []LoopDef{{ID: "each", Iterator: "selectedListItems", Steps: ids}}}
}

// A mark_failed send on the second of three targets is attributed to that
// target; the others are reported succeeded, with their records tagged.
func TestTargetFailureIsAttributed(t *testing.T) {
	bot := &sendBot{fail: map[string]bool{"b": true}}
	res, err := runTargets(t, bot, sendLoopDef(&ErrorHandlerDef{Action: ErrorActionMarkFailed}), "a", "b", "c")
	if err == nil || res == nil {
		t.Fatalf("a failed target must fail the action: res=%v err=%v", res, err)
	}
	if len(res.FailedItems) != 1 || !res.FailedItems[0].Target || res.FailedItems[0].Index != 1 {
		t.Fatalf("failed items = %+v, want target 1", res.FailedItems)
	}
	if !reflect.DeepEqual(res.SucceededTargets, []int{0, 2}) {
		t.Errorf("succeeded = %v, want [0 2]", res.SucceededTargets)
	}
	if !reflect.DeepEqual(res.ExtractedTargets, []int{0, 2}) || len(res.ExtractedItems) != 2 {
		t.Errorf("extracted = %v tagged %v", res.ExtractedItems, res.ExtractedTargets)
	}
}

// Retries exhausted with onFailure mark_failed, and an explicit mark_failed
// step inside a nested for_each, both name the target, not the inner index.
func TestTargetIndexOnEveryFailurePath(t *testing.T) {
	retry := &ErrorHandlerDef{Action: ErrorActionRetry, MaxRetries: 1, OnFailure: string(ErrorActionMarkFailed)}
	res, _ := runTargets(t, &sendBot{fail: map[string]bool{"c": true}}, sendLoopDef(retry), "a", "b", "c")
	if len(res.FailedItems) != 1 || !res.FailedItems[0].Target || res.FailedItems[0].Index != 2 {
		t.Fatalf("retry exhausted: failed items = %+v, want target 2", res.FailedItems)
	}

	inner := StepDef{ID: "inner", Type: "for_each", Items: "{{posts}}", As: "post", Steps: []StepDef{
		{ID: "flag", Type: "mark_failed", Text: "post gone"}}}
	def := sendLoopDef(nil)
	def.Steps = append(def.Steps, inner)
	def.Loops[0].Steps = []string{"send", "inner"}
	ae := NewActionExecutor(context.Background(), nil, nil, nil, nil, &sendBot{}, zerolog.Nop())
	ae.SetVariable("selectedListItems", []interface{}{"a", "b"})
	ae.SetVariable("posts", []interface{}{"p1", "p2", "p3"})
	res, _ = ae.ExecuteDef(&StorageAction{ID: "a", TargetPlatform: "p", Type: "t"}, def)
	for _, f := range res.FailedItems {
		if !f.Target || f.Index > 1 {
			t.Fatalf("mark_failed in a nested loop: %+v, want a target index", f)
		}
	}
	if len(res.FailedItems) != 6 || len(res.SucceededTargets) != 0 {
		t.Errorf("failed = %d, succeeded = %v", len(res.FailedItems), res.SucceededTargets)
	}
}

// An abort on target 1 leaves target 0 succeeded and the rest not done.
func TestTargetAbortStopsAtTheFailedTarget(t *testing.T) {
	bot := &sendBot{fail: map[string]bool{"b": true}}
	res, err := runTargets(t, bot, sendLoopDef(&ErrorHandlerDef{Action: ErrorActionAbort}), "a", "b", "c")
	if !errors.Is(err, ErrAbort) {
		t.Fatalf("err = %v, want abort", err)
	}
	if !reflect.DeepEqual(res.SucceededTargets, []int{0}) || !reflect.DeepEqual(bot.sent, []string{"a"}) {
		t.Errorf("succeeded = %v, sent = %v", res.SucceededTargets, bot.sent)
	}
}

// A for_each over the targets is tracked like a loop; a loop over anything
// else attributes nothing to targets.
func TestTargetTrackingOnlyForTargetLists(t *testing.T) {
	def := &ActionDef{ActionType: "t", SideEffects: "message", Steps: []StepDef{
		{ID: "each", Type: "for_each", Items: "{{targets}}", Steps: []StepDef{
			{ID: "send", Type: "call_bot_method", MethodName: "send", Args: []interface{}{"{{item}}"},
				OnError: &ErrorHandlerDef{Action: ErrorActionMarkFailed}, SideEffect: true}}}}}
	res, _ := runTargets(t, &sendBot{fail: map[string]bool{"a": true}}, def, "a", "b")
	if len(res.FailedItems) != 1 || !res.FailedItems[0].Target || res.FailedItems[0].Index != 0 ||
		!reflect.DeepEqual(res.SucceededTargets, []int{1}) {
		t.Fatalf("for_each over targets: failed %+v, succeeded %v", res.FailedItems, res.SucceededTargets)
	}

	other := sendLoopDef(&ErrorHandlerDef{Action: ErrorActionMarkFailed})
	other.Loops[0].Iterator = "posts"
	ae := NewActionExecutor(context.Background(), nil, nil, nil, nil, &sendBot{fail: map[string]bool{"p": true}}, zerolog.Nop())
	ae.SetVariable("posts", []interface{}{"p", "q"})
	res, _ = ae.ExecuteDef(&StorageAction{ID: "a", TargetPlatform: "p", Type: "t"}, other)
	if len(res.FailedItems) != 1 || res.FailedItems[0].Target || len(res.SucceededTargets) != 0 {
		t.Fatalf("non-target loop: failed %+v, succeeded %v", res.FailedItems, res.SucceededTargets)
	}
	if !reflect.DeepEqual(res.ExtractedTargets, []int{-1}) {
		t.Errorf("extracted targets = %v, want [-1]", res.ExtractedTargets)
	}
}
