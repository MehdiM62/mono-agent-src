package httpnodes

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"golang.org/x/net/dns/dnsmessage"

	"github.com/monoes/mono-agent/internal/workflow"
)

// useFakeDNS makes the system's resolver (net.DefaultResolver, what a dial
// by name asks) answer every A query with ip, for the test.
func useFakeDNS(t *testing.T, ip string) {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	a4 := [4]byte(net.ParseIP(ip).To4())
	go func() {
		buf := make([]byte, 1500)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			var p dnsmessage.Parser
			h, err := p.Start(buf[:n])
			if err != nil {
				continue
			}
			q, err := p.Question()
			if err != nil {
				continue
			}
			b := dnsmessage.NewBuilder(nil, dnsmessage.Header{ID: h.ID, Response: true, Authoritative: true, RecursionAvailable: true})
			_ = b.StartQuestions()
			_ = b.Question(q)
			_ = b.StartAnswers()
			if q.Type == dnsmessage.TypeA {
				_ = b.AResource(dnsmessage.ResourceHeader{Name: q.Name, Class: dnsmessage.ClassINET, TTL: 0}, dnsmessage.AResource{A: a4})
			}
			if msg, err := b.Finish(); err == nil {
				_, _ = pc.WriteTo(msg, from)
			}
		}
	}()
	old := net.DefaultResolver
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "udp", pc.LocalAddr().String())
	}}
	t.Cleanup(func() {
		net.DefaultResolver = old
		pc.Close()
	})
}

// The node resolves the host once, decides whether it is this machine on
// those addresses and dials them. A name that answers a remote address at
// the check and loopback after (DNS rebinding) never reaches this machine;
// one that answers loopback is refused as this machine in a workflow the
// user hasn't approved.
func TestSSHResolvesTheHostOnce(t *testing.T) {
	port, seen := startExecServer(t, envAccept)
	useFakeDNS(t, "127.0.0.1") // what any later lookup of the name says
	calls := 0
	old := lookupHost
	t.Cleanup(func() { lookupHost = old })
	lookupHost = func(_ context.Context, h string) ([]string, error) {
		calls++
		if calls == 1 {
			return []string{"192.0.2.10"}, nil // TEST-NET-1: another host
		}
		return []string{"127.0.0.1"}, nil
	}
	cfg := map[string]interface{}{"host": "rebind.test", "port": float64(port), "username": "user",
		"password": "secret", "command": "echo hi", "timeout_seconds": float64(1)}
	ctx := workflow.WithAIAuthored(context.Background())
	in := workflow.NodeInput{WorkflowID: "wf-1", ExecutionID: "exec-1"}
	_, err := (&SSHNode{}).Execute(ctx, in, cfg)
	select {
	case s := <-seen:
		t.Fatalf("a name checked as another host reached this machine and ran %q (err %v)", s.command, err)
	case <-time.After(300 * time.Millisecond):
	}
	if err == nil || errors.Is(err, workflow.ErrUnapprovedProgram) || calls != 1 {
		t.Fatalf("checked as another host: %v, %d lookups", err, calls)
	}

	// Loopback at the check: refused as this machine, before any dial.
	if _, err := (&SSHNode{}).Execute(ctx, in, cfg); !errors.Is(err, workflow.ErrUnapprovedProgram) {
		t.Fatalf("checked as this machine: %v", err)
	}
	select {
	case s := <-seen:
		t.Fatalf("the refused step reached the server: %q", s.command)
	case <-time.After(200 * time.Millisecond):
	}
}

// The connection goes to the checked address, and the host key is still
// checked against the host as configured: a known_hosts line for that name
// passes, one only for another name does not.
func TestSSHHostKeyIsCheckedAgainstTheConfiguredName(t *testing.T) {
	port, seen := startExecServer(t, envAccept)
	var hostKey ssh.PublicKey
	probe := &ssh.ClientConfig{User: "user", Auth: []ssh.AuthMethod{ssh.Password("secret")},
		HostKeyCallback: func(_ string, _ net.Addr, k ssh.PublicKey) error { hostKey = k; return nil }}
	if c, err := ssh.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), probe); err != nil {
		t.Fatal(err)
	} else {
		c.Close()
	}
	old := lookupHost
	t.Cleanup(func() { lookupHost = old })
	lookupHost = func(context.Context, string) ([]string, error) { return []string{"127.0.0.1"}, nil }
	run := func(knownName string) error {
		kh := filepath.Join(t.TempDir(), "known_hosts")
		line := knownhosts.Line([]string{knownhosts.Normalize(net.JoinHostPort(knownName, strconv.Itoa(port)))}, hostKey)
		if err := os.WriteFile(kh, []byte(line+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg := map[string]interface{}{"host": "box.test", "port": float64(port), "username": "user",
			"password": "secret", "command": "echo hi", "timeout_seconds": float64(5), "known_hosts": kh}
		_, err := (&SSHNode{}).Execute(context.Background(), workflow.NodeInput{ExecutionID: "exec-1"}, cfg)
		return err
	}
	if err := run("box.test"); err != nil {
		t.Fatalf("known_hosts names the configured host: %v", err)
	}
	if s := <-seen; !strings.HasSuffix(s.command, "echo hi") {
		t.Errorf("ran %q", s.command)
	}
	if err := run("other.test"); err == nil || !strings.Contains(err.Error(), "handshake") {
		t.Fatalf("known_hosts names another host only: %v", err)
	}
}
