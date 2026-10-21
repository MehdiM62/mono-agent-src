package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/zalando/go-keyring"

	"github.com/monoes/mono-agent/internal/connections"
	"github.com/monoes/mono-agent/internal/vault"
)

// hostProbe records its config. Its destination keys are comm.email_send's
// and service.mastodon's.
type hostProbe struct{ got *[]map[string]interface{} }

func (hostProbe) Type() string { return "test.hostprobe" }
func (p hostProbe) Execute(_ context.Context, in NodeInput, config map[string]interface{}) ([]NodeOutput, error) {
	*p.got = append(*p.got, config)
	return []NodeOutput{{Handle: "main", Items: in.Items}}, nil
}
func (hostProbe) CredentialDestinationKeys() [][]string {
	return [][]string{{"smtp_host"}, {"smtp_port"}, {"instance", "instance_url"}}
}

// credHostFixture runs workflows over a mail connection that names its
// relay ("mail") and a token connection that names no host ("token").
type credHostFixture struct {
	ctx   context.Context
	store *SQLiteWorkflowStore
	conns *connections.Store
	mail  string
	token string
}

func newCredHostFixture(t *testing.T) credHostFixture {
	t.Helper()
	keyring.MockInit()
	db, store := approvalsTestDB(t)
	ctx := vault.ContextWithProfileID(vault.ContextWithDB(context.Background(), db.DB), "default")
	conns := connections.NewStore(db.DB)
	mail := &connections.Connection{ProfileID: "default", Platform: "smtp", Method: connections.MethodAppPass, Label: "Work mail",
		Data: map[string]interface{}{"username": "me@mail.example", "password": "mail-pass", "smtp_host": "smtp.mail.example", "smtp_port": float64(587)}}
	token := &connections.Connection{ProfileID: "default", Platform: "generic_api", Method: connections.MethodAPIKey, Label: "Token",
		Data: map[string]interface{}{"access_token": "tok-1"}}
	for _, c := range []*connections.Connection{mail, token} {
		if err := conns.Save(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	return credHostFixture{ctx, store, conns, mail.ID, token.ID}
}

// run saves nodes behind a manual trigger, approves the workflow when
// approved is set, and runs it with input.
func (f credHostFixture) run(t *testing.T, reg *NodeTypeRegistry, approved bool, input map[string]interface{}, nodes []WorkflowNode, conns ...WorkflowConnection) error {
	t.Helper()
	id := "wf-" + uuid.NewString()[:8]
	wf := &Workflow{ID: id, Name: id, ProfileID: "default", Nodes: []WorkflowNode{{ID: id + "-t", WorkflowID: id, Type: "trigger.manual", Name: "Start"}}}
	for _, n := range nodes {
		n.ID, n.WorkflowID = id+"-"+n.ID, id
		wf.Nodes = append(wf.Nodes, n)
	}
	wf.Connections = append([]WorkflowConnection{{SourceNodeID: "t", SourceHandle: "main", TargetNodeID: nodes[0].ID, TargetHandle: "main"}}, conns...)
	for i := range wf.Connections {
		c := &wf.Connections[i]
		c.ID, c.WorkflowID, c.SourceNodeID, c.TargetNodeID = fmt.Sprintf("%s-c%d", id, i), id, id+"-"+c.SourceNodeID, id+"-"+c.TargetNodeID
	}
	createSQLWorkflow(t, f.store, wf)
	if approved {
		if err := f.store.ApproveDefinition(f.ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	dag, err := BuildDAG(wf.Nodes, wf.Connections)
	if err != nil {
		t.Fatal(err)
	}
	exec := &WorkflowExecution{ID: uuid.NewString(), WorkflowID: id, ProfileID: "default", TriggerType: "trigger.manual", TriggerNodeID: id + "-t", TriggerData: input}
	return RunExecution(f.ctx, exec, wf, dag, reg, f.store, f.conns, NewExpressionEngine(), zerolog.Nop())
}

// A saved connection's secret goes only where the connection says, or
// where the user's own approved workflow says when the connection names no
// host: a relay or instance a workflow the user never approved sets (an
// agent's), one taken from the run's data, or one that differs from the
// connection's is refused, naming the field and the connection's host,
// before the step runs.
func TestCredentialGoesOnlyToTheHostsItsConnectionOrTheWorkflowNames(t *testing.T) {
	f := newCredHostFixture(t)
	cases := []struct {
		name     string
		approved bool
		cred     string
		config   map[string]interface{}
		input    map[string]interface{}
		refused  string // "" runs
		check    func(map[string]interface{}) bool
	}{
		{name: "an unapproved workflow's relay of its own", cred: "mail",
			config:  map[string]interface{}{"smtp_host": "attacker.test", "to": "ada@x.test"},
			refused: `"smtp_host": the saved connection "Work mail" sends its secret to smtp.mail.example, not attacker.test`},
		{name: "the user's approved workflow, a relay of its own", approved: true, cred: "mail",
			config:  map[string]interface{}{"smtp_host": "attacker.test"},
			refused: `"smtp_host": the saved connection "Work mail" sends its secret to smtp.mail.example, not attacker.test`},
		{name: "a port of its own", approved: true, cred: "mail",
			config:  map[string]interface{}{"smtp_port": 2525},
			refused: `"smtp_port": the saved connection "Work mail" sends its secret to 587, not 2525`},
		{name: "a relay from the run's data", approved: true, cred: "mail",
			config: map[string]interface{}{"smtp_host": "{{ $json.relay }}"}, input: map[string]interface{}{"relay": "attacker.test"},
			refused: `"smtp_host": the saved connection "Work mail" sends its secret to smtp.mail.example, not attacker.test`},
		{name: "the connection's relay, written another way", approved: true, cred: "mail",
			config: map[string]interface{}{"smtp_host": "SMTP.Mail.Example.", "smtp_port": 587},
			check:  func(c map[string]interface{}) bool { return c["password"] == "mail-pass" }},
		{name: "an unapproved workflow leaving the relay to the connection", cred: "mail",
			config: map[string]interface{}{"to": "ada@x.test"},
			check: func(c map[string]interface{}) bool {
				return c["smtp_host"] == "smtp.mail.example" && c["password"] == "mail-pass"
			}},
		{name: "an unapproved workflow's instance, which the connection doesn't name", cred: "token",
			config:  map[string]interface{}{"instance": "https://attacker.test"},
			refused: `"instance": attacker.test:443 isn't a host the saved connection "Token" names`},
		{name: "an unapproved workflow's instance_url", cred: "token",
			config:  map[string]interface{}{"instance_url": "https://attacker.test/"},
			refused: `"instance_url": attacker.test:443 isn't a host the saved connection "Token" names`},
		{name: "the user's approved workflow with its instance written in", approved: true, cred: "token",
			config: map[string]interface{}{"instance": "https://fosstodon.example"},
			check: func(c map[string]interface{}) bool {
				return c["instance"] == "https://fosstodon.example" && c["access_token"] == "tok-1"
			}},
		{name: "the user's approved workflow taking the instance from data", approved: true, cred: "token",
			config: map[string]interface{}{"instance": "https://{{ $json.host }}"}, input: map[string]interface{}{"host": "attacker.test"},
			refused: `"instance": the host the saved connection "Token"'s secret goes to must be written in the workflow, not taken from its data`},
		{name: "a path from data on the written instance", approved: true, cred: "token",
			config: map[string]interface{}{"instance": "https://fosstodon.example/{{ $json.p }}"}, input: map[string]interface{}{"p": "@attacker.test"},
			check: func(c map[string]interface{}) bool {
				return c["instance"] == "https://fosstodon.example/@attacker.test"
			}},
		{name: "an unapproved workflow with no instance", cred: "token",
			config: map[string]interface{}{},
			check:  func(c map[string]interface{}) bool { return c["access_token"] == "tok-1" }},
		{name: "no connection: the step's own host", approved: false,
			config: map[string]interface{}{"instance": "https://attacker.test"},
			check:  func(c map[string]interface{}) bool { return c["instance"] == "https://attacker.test" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got []map[string]interface{}
			reg := NewNodeTypeRegistry()
			reg.Register("test.hostprobe", func() NodeExecutor { return hostProbe{&got} })
			cfg := map[string]interface{}{}
			for k, v := range c.config {
				cfg[k] = v
			}
			switch c.cred {
			case "mail":
				cfg["credential_id"] = f.mail
			case "token":
				cfg["credential_id"] = f.token
			}
			input := c.input
			if input == nil {
				input = map[string]interface{}{}
			}
			err := f.run(t, reg, c.approved, input, []WorkflowNode{{ID: "s", Type: "test.hostprobe", Name: "Send", Config: cfg}})
			if c.refused != "" {
				if !errors.Is(err, ErrCredentialDestination) || !strings.Contains(err.Error(), c.refused) || len(got) != 0 {
					t.Fatalf("run: %v; the step ran with %v; want it refused with %q", err, got, c.refused)
				}
				return
			}
			if err != nil || len(got) != 1 || !c.check(got[0]) {
				t.Fatalf("run: %v, the step ran with %v", err, got)
			}
		})
	}
}

// The other paths a config takes to Execute hold the rule too: a field
// resolved once per item, a sub-node's config and an ai_tool's, whose host
// the model's arguments would name.
func TestCredentialHostsOnEveryConfigPath(t *testing.T) {
	f := newCredHostFixture(t)
	t.Run("once per item", func(t *testing.T) {
		var got []map[string]interface{}
		reg := NewNodeTypeRegistry()
		reg.Register("test.emit", func() NodeExecutor {
			return emitNode{[]map[string]interface{}{{"h": "smtp.mail.example"}, {"h": "attacker.test"}}}
		})
		reg.Register("test.hostprobe", func() NodeExecutor { return hostProbe{&got} })
		err := f.run(t, reg, true, map[string]interface{}{}, []WorkflowNode{
			{ID: "e", Type: "test.emit", Name: "Emit"},
			{ID: "s", Type: "test.hostprobe", Name: "Send", Config: map[string]interface{}{
				"credential_id": f.mail, "smtp_host": "{{ $json.h }}", "execute_once_per_item": true}},
		}, WorkflowConnection{SourceNodeID: "e", SourceHandle: "main", TargetNodeID: "s", TargetHandle: "main"})
		if !errors.Is(err, ErrCredentialDestination) {
			t.Errorf("run %v, want the second item refused", err)
		}
		for _, cfg := range got {
			if cfg["smtp_host"] != "smtp.mail.example" {
				t.Errorf("the step ran with %v, want only the first item, the connection's relay", cfg)
			}
		}
	})
	t.Run("a sub-node", func(t *testing.T) {
		var got []map[string]interface{}
		reg := NewNodeTypeRegistry()
		reg.Register("test.subroot", func() NodeExecutor { return subRoot{got: &got} })
		reg.Register("test.hostprobe", func() NodeExecutor { return hostProbe{&[]map[string]interface{}{}} })
		err := f.run(t, reg, true, map[string]interface{}{"h": "attacker.test"}, []WorkflowNode{
			{ID: "r", Type: "test.subroot", Name: "Root"},
			{ID: "m", Type: "test.hostprobe", Name: "Model", Config: map[string]interface{}{"credential_id": f.mail, "smtp_host": "{{ $json.h }}"}},
		}, WorkflowConnection{SourceNodeID: "m", SourceHandle: "ai_languageModel", TargetNodeID: "r", TargetHandle: "ai_languageModel"})
		if !errors.Is(err, ErrCredentialDestination) || !strings.Contains(err.Error(), "-m (Model)") || len(got) != 0 {
			t.Errorf("run %v, the root got %v", err, got)
		}
	})
	t.Run("an ai_tool its root calls", func(t *testing.T) {
		var got []map[string]interface{}
		reg := NewNodeTypeRegistry()
		reg.Register("test.subroot", func() NodeExecutor { return subRoot{call: true, got: &got} })
		reg.Register("test.hostprobe", func() NodeExecutor { return hostProbe{&[]map[string]interface{}{}} })
		run := func(msg string) error {
			return f.run(t, reg, true, map[string]interface{}{"msg": msg}, []WorkflowNode{
				{ID: "r", Type: "test.subroot", Name: "Root"},
				{ID: "w", Type: "test.hostprobe", Name: "Tool", Config: map[string]interface{}{"credential_id": f.token, "instance": "{{ $json.q }}"}},
			}, WorkflowConnection{SourceNodeID: "w", SourceHandle: "ai_tool", TargetNodeID: "r", TargetHandle: "ai_tool"})
		}
		if err := run("attacker.test"); !errors.Is(err, ErrCredentialDestination) || !strings.Contains(err.Error(), `tool "Tool"`) || len(got) != 0 {
			t.Errorf("run %v, the tool gave %v", err, got)
		}
	})
}

// Hosts compare as where they send: case, a trailing dot, userinfo, a path
// and the scheme's default port don't count; a port both name does; a list
// of brokers is a set.
func TestCheckDestinationKeysComparesHosts(t *testing.T) {
	use := &CredentialUse{Name: "c", Platform: "kafka", Data: map[string]interface{}{
		"url": "https://Api.Example.com/v1", "brokers": "b1:9092,b2:9092", "host": "mq.example"}}
	keys := [][]string{{"url"}, {"brokers"}, {"host"}, {"credential_platform"}}
	for cfg, ok := range map[string]bool{
		`{"url": "https://user@api.example.com./v2?x=1"}`:    true,
		`{"url": "http://api.example.com:8443/"}`:            false,
		`{"url": "https://api.example.com:443/x"}`:           true,
		`{"url": "http://api.example.com/x"}`:                false,
		`{"url": "https://api.example.com.attacker.test"}`:   false,
		`{"url": "https://attacker.test\\@api.example.com"}`: false,
		`{"brokers": ["b2:9092", "b1:9092"]}`:                true,
		`{"brokers": "b1:9092, evil:9092"}`:                  false,
		`{"host": "mq.example:1883"}`:                        true,
		`{"credential_platform": "kafka"}`:                   true,
		`{"credential_platform": "openai"}`:                  false,
		`{"url": "/relative/path"}`:                          true,
	} {
		var resolved map[string]interface{}
		if err := json.Unmarshal([]byte(cfg), &resolved); err != nil {
			t.Fatal(err)
		}
		err := CheckDestinationKeys(keys, use, true, resolved, resolved)
		if (err == nil) != ok {
			t.Errorf("%s: %v, want allowed %v", cfg, err, ok)
		}
	}
}

// A queue trigger's consumer resolves its connection when it activates:
// brokers the connection doesn't name come only from a workflow the user
// approved, and a host other than the connection's is refused either way.
func TestTriggerCredentialsKeepTheirHosts(t *testing.T) {
	f := newCredHostFixture(t)
	e := &WorkflowEngine{store: f.store, connStore: f.conns, logger: zerolog.Nop()}
	wf := &Workflow{ID: "wf-trig", Name: "trig", ProfileID: "default", Nodes: []WorkflowNode{{ID: "wf-trig-k", WorkflowID: "wf-trig", Type: "trigger.kafka", Name: "Kafka",
		Config: map[string]interface{}{"credential_id": f.token, "brokers": "attacker.test:9092"}}}}
	createSQLWorkflow(t, f.store, wf)
	keys := [][]string{{"brokers"}, {"smtp_host"}}
	resolve := func(cfg map[string]interface{}) error {
		_, err := e.ResolveTriggerCredentials(f.ctx, wf, "trigger.kafka", keys, cfg)
		return err
	}
	if err := resolve(map[string]interface{}{"credential_id": f.token, "brokers": "attacker.test:9092"}); !errors.Is(err, ErrCredentialDestination) {
		t.Errorf("brokers of a workflow the user hasn't approved: %v", err)
	}
	if err := f.store.ApproveDefinition(f.ctx, wf.ID); err != nil {
		t.Fatal(err)
	}
	if err := resolve(map[string]interface{}{"credential_id": f.token, "brokers": "kafka.example:9092"}); err != nil {
		t.Errorf("the user's own brokers: %v", err)
	}
	if err := resolve(map[string]interface{}{"credential_id": f.mail, "smtp_host": "attacker.test"}); !errors.Is(err, ErrCredentialDestination) {
		t.Errorf("a host other than the connection's: %v", err)
	}
}

// A connection that names a bare host (no scheme, no port) is for that
// host over https on the default port: a URL of the workflow naming http://
// or another port of it is not the connection's destination.
func TestBareHostConnectionMeansHTTPSDefaultPort(t *testing.T) {
	bare := &CredentialUse{Name: "c", Data: map[string]interface{}{"base_url": "api.example.com"}}
	stated := &CredentialUse{Name: "c", Data: map[string]interface{}{"base_url": "http://api.example.com:8080"}}
	keys := [][]string{{"base_url"}}
	for _, tc := range []struct {
		use *CredentialUse
		url string
		ok  bool
	}{
		{bare, "https://api.example.com/v1", true},
		{bare, "https://api.example.com:443/v1", true},
		{bare, "api.example.com", true},
		{bare, "http://api.example.com/v1", false},
		{bare, "https://api.example.com:8443/v1", false},
		{stated, "http://api.example.com:8080/v1", true},
		{stated, "https://api.example.com:8080/v1", true},
	} {
		cfg := map[string]interface{}{"base_url": tc.url}
		if err := CheckDestinationKeys(keys, tc.use, true, cfg, cfg); (err == nil) != tc.ok {
			t.Errorf("%v with %s: %v, want allowed %v", tc.use.Data["base_url"], tc.url, err, tc.ok)
		}
	}
}
