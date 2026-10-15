package dbnodes

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/lib/pq"

	"github.com/monoes/mono-agent/internal/workflow"
)

func TestSubscribeRedisChannelsAndPatterns(t *testing.T) {
	mr := miniredis.RunT(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got := make(chan workflow.Item, 4)
	done := make(chan error, 1)
	go func() {
		done <- SubscribeRedis(ctx, map[string]interface{}{
			"connection_string": "redis://" + mr.Addr(), "channels": "orders, events.*", "json_parse_body": true,
		}, func(it workflow.Item) { got <- it })
	}()
	deadline := time.Now().Add(3 * time.Second)
	for len(mr.PubSubChannels("")) == 0 || mr.PubSubNumPat() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("subscriptions never registered")
		}
		time.Sleep(10 * time.Millisecond)
	}
	mr.Publish("orders", `{"id":1}`)
	mr.Publish("events.signup", "plain")
	var items []workflow.Item
	for len(items) < 2 {
		select {
		case it := <-got:
			items = append(items, it)
		case <-ctx.Done():
			t.Fatalf("got %d messages", len(items))
		}
	}
	byChannel := map[string]interface{}{}
	for _, it := range items {
		byChannel[it.JSON["channel"].(string)] = it.JSON["message"]
	}
	if m, _ := byChannel["orders"].(map[string]interface{}); m["id"] != float64(1) || byChannel["events.signup"] != "plain" {
		t.Errorf("items = %v", items)
	}
	cancel()
	if err := <-done; err != nil {
		t.Errorf("err = %v", err)
	}
}

func TestPostgresTriggerPlan(t *testing.T) {
	ch, setup, teardown, err := postgresTriggerPlan(map[string]interface{}{"table": "Orders", "fires_on": "delete", "replace_if_exists": true})
	if err != nil {
		t.Fatal(err)
	}
	if ch != "monoagent_orders_delete" || len(setup) != 3 || len(teardown) != 2 {
		t.Fatalf("plan = %q %v %v", ch, setup, teardown)
	}
	wantFn := `CREATE OR REPLACE FUNCTION "monoagent_notify_orders_delete"() RETURNS trigger LANGUAGE plpgsql AS $mono$ BEGIN PERFORM pg_notify('monoagent_orders_delete', row_to_json(OLD)::text); RETURN OLD; END; $mono$`
	if setup[0] != wantFn {
		t.Errorf("function = %s", setup[0])
	}
	if setup[2] != `CREATE TRIGGER "monoagent_trg_orders_delete" AFTER DELETE ON "public"."Orders" FOR EACH ROW EXECUTE FUNCTION "monoagent_notify_orders_delete"()` {
		t.Errorf("trigger = %s", setup[2])
	}
	ch, setup, _, err = postgresTriggerPlan(map[string]interface{}{"trigger_mode": "listenTrigger", "channel_name": "c"})
	if err != nil || ch != "c" || setup != nil {
		t.Errorf("listen plan = %q %v %v", ch, setup, err)
	}
	for _, bad := range []map[string]interface{}{{"trigger_mode": "listenTrigger"}, {}, {"table": "t", "fires_on": "TRUNCATE"}, {"trigger_mode": "x"}} {
		if _, _, _, err := postgresTriggerPlan(bad); err == nil {
			t.Errorf("%v: want error", bad)
		}
	}
}

type fakePGListener struct {
	ch       chan *pq.Notification
	listened string
}

func (f *fakePGListener) Listen(c string) error                        { f.listened = c; return nil }
func (f *fakePGListener) NotificationChannel() <-chan *pq.Notification { return f.ch }
func (f *fakePGListener) Close() error                                 { return nil }

func TestSubscribePostgresInstallsListensAndTearsDown(t *testing.T) {
	l := &fakePGListener{ch: make(chan *pq.Notification, 3)}
	var execs [][]string
	var mu sync.Mutex
	prevL, prevE := pgOpenListener, pgExecStatements
	pgOpenListener = func(string) pgListener { return l }
	pgExecStatements = func(_ context.Context, _ string, s []string) error {
		mu.Lock()
		defer mu.Unlock()
		execs = append(execs, s)
		return nil
	}
	defer func() { pgOpenListener, pgExecStatements = prevL, prevE }()

	l.ch <- nil // reconnect marker is skipped
	l.ch <- &pq.Notification{Extra: `{"id": 5}`}
	l.ch <- &pq.Notification{Extra: "not json"}
	ctx, cancel := context.WithCancel(context.Background())
	var got []workflow.Item
	err := SubscribePostgres(ctx, map[string]interface{}{"connection_string": "postgres://x", "table": "orders"}, func(it workflow.Item) {
		got = append(got, it)
		if len(got) == 2 {
			cancel()
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if l.listened != "monoagent_orders_insert" || got[0].JSON["id"] != float64(5) || got[1].JSON["payload"] != "not json" {
		t.Errorf("listened=%q got=%v", l.listened, got)
	}
	if len(execs) != 2 || !strings.HasPrefix(execs[1][0], "DROP TRIGGER") {
		t.Errorf("execs = %v", execs)
	}
}

func TestTriggerSourceRestartsAndStops(t *testing.T) {
	var calls int32
	src := &triggerSource{nodeType: "trigger.test", minWait: time.Millisecond, maxWait: 5 * time.Millisecond,
		sub: func(ctx context.Context, cfg map[string]interface{}, emit func(workflow.Item)) error {
			if atomic.AddInt32(&calls, 1) < 3 {
				return errors.New("broker down")
			}
			emit(workflow.NewItem(map[string]interface{}{"topic": cfg["topic"]}))
			<-ctx.Done()
			return nil
		},
		resolve: func(_ context.Context, cfg map[string]interface{}) (map[string]interface{}, error) {
			out := map[string]interface{}{"topic": "resolved"}
			for k, v := range cfg {
				out[k] = v
			}
			return out, nil
		},
	}
	var errs int32
	src.onError = func(string, error) { atomic.AddInt32(&errs, 1) }
	fired := make(chan []workflow.Item, 1)
	stop, err := src.Activate(&workflow.Workflow{}, &workflow.WorkflowNode{Config: map[string]interface{}{"credential_id": "c"}}, func(items []workflow.Item) { fired <- items })
	if err != nil {
		t.Fatal(err)
	}
	select {
	case items := <-fired:
		if items[0].JSON["topic"] != "resolved" {
			t.Errorf("fired %v", items)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("never fired")
	}
	stop()
	if atomic.LoadInt32(&errs) != 2 {
		t.Errorf("onError calls = %d", errs)
	}
	if _, err := NewTriggerSource("trigger.nope", nil, nil); err == nil {
		t.Error("unknown type: want error")
	}
	for typ := range TriggerSubscribers {
		if _, err := NewTriggerSource(typ, nil, nil); err != nil {
			t.Errorf("%s: %v", typ, err)
		}
	}
}
