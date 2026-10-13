package dbnodes

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/lib/pq"

	"github.com/monoes/mono-agent/internal/workflow"
)

// pgListener is the part of *pq.Listener the trigger uses; tests fake it.
type pgListener interface {
	Listen(channel string) error
	NotificationChannel() <-chan *pq.Notification
	Close() error
}

var pgOpenListener = func(dsn string) pgListener {
	return pq.NewListener(dsn, time.Second, time.Minute, nil)
}

// pgExecStatements runs setup/teardown SQL; tests swap it.
var pgExecStatements = func(ctx context.Context, dsn string, stmts []string) error {
	db, err := openPingedSQL(ctx, "trigger.postgres", "postgres", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	for _, s := range stmts {
		if _, err := db.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("%s: %w", s, err)
		}
	}
	return nil
}

var pgNameSanitize = regexp.MustCompile(`[^a-z0-9_]+`)

// postgresTriggerPlan returns the channel to LISTEN on plus, in
// createTrigger mode, the statements that install the notify function and
// row trigger and the ones that remove them again.
func postgresTriggerPlan(cfg map[string]interface{}) (channel string, setup, teardown []string, err error) {
	mode := cfgString(cfg, "trigger_mode")
	if mode == "" {
		mode = "createTrigger"
	}
	channel = cfgString(cfg, "channel_name")
	if mode == "listenTrigger" {
		if channel == "" {
			return "", nil, nil, fmt.Errorf("'channel_name' is required in listenTrigger mode")
		}
		return channel, nil, nil, nil
	}
	if mode != "createTrigger" {
		return "", nil, nil, fmt.Errorf("'trigger_mode' must be createTrigger or listenTrigger")
	}
	table := cfgString(cfg, "table")
	if table == "" {
		return "", nil, nil, fmt.Errorf("'table' is required in createTrigger mode")
	}
	schema := cfgString(cfg, "schema")
	if schema == "" {
		schema = "public"
	}
	event := strings.ToUpper(cfgString(cfg, "fires_on"))
	if event == "" {
		event = "INSERT"
	}
	if event != "INSERT" && event != "UPDATE" && event != "DELETE" {
		return "", nil, nil, fmt.Errorf("'fires_on' must be INSERT, UPDATE or DELETE")
	}
	base := pgNameSanitize.ReplaceAllString(strings.ToLower(table+"_"+event), "_")
	if channel == "" {
		channel = "monoagent_" + base
	}
	fn := cfgString(cfg, "function_name")
	if fn == "" {
		fn = "monoagent_notify_" + base
	}
	trg := cfgString(cfg, "trigger_name")
	if trg == "" {
		trg = "monoagent_trg_" + base
	}
	d := postgresDialect
	qTable, err := d.quoteTable(schema, table)
	if err != nil {
		return "", nil, nil, err
	}
	qFn, _ := d.quoteIdent(fn)
	qTrg, _ := d.quoteIdent(trg)
	row := "NEW"
	if event == "DELETE" {
		row = "OLD"
	}
	create := "CREATE FUNCTION "
	if cfgBool(cfg, "replace_if_exists", false) {
		create = "CREATE OR REPLACE FUNCTION "
	}
	lit := "'" + strings.ReplaceAll(channel, "'", "''") + "'"
	setup = []string{
		create + qFn + "() RETURNS trigger LANGUAGE plpgsql AS $mono$ BEGIN PERFORM pg_notify(" + lit + ", row_to_json(" + row + ")::text); RETURN " + row + "; END; $mono$",
		"DROP TRIGGER IF EXISTS " + qTrg + " ON " + qTable,
		"CREATE TRIGGER " + qTrg + " AFTER " + event + " ON " + qTable + " FOR EACH ROW EXECUTE FUNCTION " + qFn + "()",
	}
	teardown = []string{
		"DROP TRIGGER IF EXISTS " + qTrg + " ON " + qTable,
		"DROP FUNCTION IF EXISTS " + qFn + "()",
	}
	return channel, setup, teardown, nil
}

// SubscribePostgres LISTENs on a channel and calls emit per notification
// (JSON payloads are decoded). In createTrigger mode (the default) it first
// installs a row trigger on 'table' for 'fires_on' that notifies with the
// row as JSON, and removes it again when ctx is cancelled.
// Keys: connection_string, trigger_mode, schema, table, fires_on,
// channel_name, function_name, trigger_name, replace_if_exists.
func SubscribePostgres(ctx context.Context, cfg map[string]interface{}, emit func(workflow.Item)) error {
	dsn := cfgString(cfg, "connection_string")
	if dsn == "" {
		return fmt.Errorf("trigger.postgres: 'connection_string' is required")
	}
	channel, setup, teardown, err := postgresTriggerPlan(cfg)
	if err != nil {
		return fmt.Errorf("trigger.postgres: %w", err)
	}
	if len(setup) > 0 {
		if err := pgExecStatements(ctx, dsn, setup); err != nil {
			return fmt.Errorf("trigger.postgres: install trigger: %w", err)
		}
		defer func() {
			cctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = pgExecStatements(cctx, dsn, teardown)
		}()
	}
	l := pgOpenListener(dsn)
	defer l.Close()
	if err := l.Listen(channel); err != nil {
		return fmt.Errorf("trigger.postgres: listen: %w", err)
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case n, ok := <-l.NotificationChannel():
			if !ok {
				return fmt.Errorf("trigger.postgres: listener closed")
			}
			if n == nil { // reconnected; notifications in between may be lost
				continue
			}
			var payload map[string]interface{}
			if json.Unmarshal([]byte(n.Extra), &payload) != nil {
				payload = map[string]interface{}{"payload": n.Extra}
			}
			emit(workflow.NewItem(payload))
		}
	}
}
