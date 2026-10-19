package node

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/monoes/mono-agent/internal/connections"
	"github.com/monoes/mono-agent/internal/hilsend"
	"github.com/monoes/mono-agent/internal/workflow"
)

// fakeSMTP is an SMTP server that accepts any mail and records the
// password every AUTH PLAIN gave it.
type fakeSMTP struct {
	port      int
	mu        sync.Mutex
	passwords []string
	mails     int
}

func newFakeSMTP(t *testing.T, addr string) *fakeSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	s := &fakeSMTP{port: ln.Addr().(*net.TCPAddr).Port}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(c)
		}
	}()
	return s
}

func (s *fakeSMTP) serve(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	say := func(line string) { _, _ = c.Write([]byte(line + "\r\n")) }
	say("220 fake ESMTP")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			say("250-fake")
			say("250 AUTH PLAIN")
		case strings.HasPrefix(cmd, "AUTH PLAIN"):
			raw, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(strings.TrimSpace(line)[len("AUTH PLAIN"):]))
			parts := strings.Split(string(raw), "\x00")
			s.mu.Lock()
			s.passwords = append(s.passwords, parts[len(parts)-1])
			s.mu.Unlock()
			say("235 ok")
		case cmd == "DATA":
			say("354 go on")
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
			}
			s.mu.Lock()
			s.mails++
			s.mu.Unlock()
			say("250 queued")
		case cmd == "QUIT":
			say("221 bye")
			return
		default:
			say("250 ok")
		}
	}
}

func (s *fakeSMTP) got() ([]string, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.passwords...), s.mails
}

// emailDeps is `node run` with the real comm.email_send and its send gate,
// whose saved connection "mail-conn" names relay as its SMTP server.
func emailDeps(t *testing.T, relay *fakeSMTP) Deps {
	t.Helper()
	d := testDeps(nodeVaultTestDB(t), "default", true)
	d.ResolveCredential = func(_ context.Context, _ *connections.Store, key, _ string) (map[string]interface{}, error) {
		if key != "mail-conn" {
			return nil, errors.New("no such connection")
		}
		return map[string]interface{}{"username": "me@mail.test", "password": "conn-secret",
			"smtp_host": "localhost", "smtp_port": float64(relay.port)}, nil
	}
	return d
}

func emailPayload(t *testing.T, config map[string]interface{}) string {
	t.Helper()
	raw, err := json.Marshal(map[string]interface{}{"config": config})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// An agent's `node run comm.email_send` with a relay of its own and the
// user's mail connection: the connection's password went to the agent's
// host, with no card at all once the user always-allowed the recipient.
// Now the step is refused before it is sent or queued, card or rule alike;
// the connection's own relay still works.
func TestNodeRunSendsTheConnectionsPasswordOnlyToItsRelay(t *testing.T) {
	relay := newFakeSMTP(t, "127.0.0.1:0")
	attacker := newFakeSMTP(t, "127.0.0.1:0")
	d := emailDeps(t, relay)
	db, err := d.OpenDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	st := hilsend.Store{DB: db.DB, Profile: "default"}
	if err := st.Allow(context.Background(), hilsend.Allow{Action: hilsend.ActionEmail, Platform: "email", Account: "me@mail.test", Recipient: "ada@x.test"}); err != nil {
		t.Fatal(err)
	}
	send := func(extra map[string]interface{}) (string, error) {
		config := map[string]interface{}{"credential_id": "mail-conn", "from": "me@mail.test", "to": "ada@x.test", "subject": "Hi", "body": "Hello"}
		for k, v := range extra {
			config[k] = v
		}
		return runNodeSubcmdStdin(t, d, emailPayload(t, config), "comm.email_send")
	}

	t.Setenv(hilsend.EnvAgentContext, "coder")
	out, err := send(map[string]interface{}{"smtp_host": "127.0.0.1", "smtp_port": float64(attacker.port)})
	if !errors.Is(err, workflow.ErrCredentialDestination) || !strings.Contains(err.Error(), `"smtp_host": the saved connection "mail-conn" sends its secret to localhost`) {
		t.Errorf("an agent's relay of its own: %v\n%s", err, out)
	}
	if pw, mails := attacker.got(); len(pw) != 0 || mails != 0 {
		t.Fatalf("the agent's relay got passwords %v and %d mail", pw, mails)
	}
	if items, _ := st.List(context.Background()); len(items) != 0 {
		t.Errorf("a refused send left a card: %+v", items)
	}

	// The agent's send to the connection's relay is always allowed: it goes.
	if out, err := send(nil); err != nil {
		t.Fatalf("the agent's send through the connection's relay: %v\n%s", err, out)
	}
	if pw, mails := relay.got(); len(pw) != 1 || pw[0] != "conn-secret" || mails != 1 {
		t.Errorf("the connection's relay got passwords %v and %d mail", pw, mails)
	}

	// The user's own terminal can't point the connection elsewhere either.
	t.Setenv(hilsend.EnvAgentContext, "")
	if _, err := send(map[string]interface{}{"smtp_host": "127.0.0.1", "smtp_port": float64(attacker.port)}); !errors.Is(err, workflow.ErrCredentialDestination) {
		t.Errorf("the operator's relay of its own with the connection: %v", err)
	}
	if pw, _ := attacker.got(); len(pw) != 0 {
		t.Fatalf("the other relay got passwords %v", pw)
	}
}

// The card of an agent's email names the relay it goes through.
func TestSendCardNamesTheRelay(t *testing.T) {
	relay := newFakeSMTP(t, "127.0.0.1:0")
	d := emailDeps(t, relay)
	t.Setenv(hilsend.EnvAgentContext, "coder")
	config := map[string]interface{}{"credential_id": "mail-conn", "from": "me@mail.test", "to": "bob@x.test", "subject": "Hi", "body": "Hello"}
	out, err := runNodeSubcmdStdin(t, d, emailPayload(t, config), "comm.email_send")
	if err != nil {
		t.Fatalf("node run: %v\n%s", err, out)
	}
	db, err := d.OpenDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	items, err := hilsend.Store{DB: db.DB, Profile: "default"}.List(context.Background())
	if err != nil || len(items) != 1 {
		t.Fatalf("cards: %v %+v\n%s", err, items, out)
	}
	if want := "localhost:" + strconv.Itoa(relay.port); items[0].Host != want {
		t.Errorf("the card's host = %q, want %q", items[0].Host, want)
	}
	if pw, mails := relay.got(); len(pw) != 0 || mails != 0 {
		t.Errorf("a queued send went out: %v %d", pw, mails)
	}
}

// An agent's `node run service.mastodon` finds the user's mastodon
// connection by platform; an instance of its own is refused before the
// token goes anywhere.
func TestNodeRunMastodonInstanceComesFromTheConnection(t *testing.T) {
	d := testDeps(nodeVaultTestDB(t), "default", true)
	d.ResolveCredential = func(_ context.Context, _ *connections.Store, key, _ string) (map[string]interface{}, error) {
		if key != "mastodon" {
			return nil, errors.New("no such connection")
		}
		return map[string]interface{}{"instance_url": "https://fosstodon.example", "access_token": "masto-secret"}, nil
	}
	t.Setenv(hilsend.EnvAgentContext, "coder")
	for _, key := range []string{"instance", "instance_url"} {
		config := map[string]interface{}{"operation": "get_account", key: "https://attacker.test"}
		out, err := runNodeSubcmdStdin(t, d, emailPayload(t, config), "service.mastodon")
		if !errors.Is(err, workflow.ErrCredentialDestination) || !strings.Contains(err.Error(), "fosstodon.example:443, not attacker.test:443") {
			t.Errorf("%s of its own: %v\n%s", key, err, out)
		}
	}
}
