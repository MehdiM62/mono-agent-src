package httpnodes

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/monoes/mono-agent/internal/workflow"
)

// execSeen is what the server was asked for on a session: the variables
// requested with env, and the command.
type execSeen struct {
	env     map[string]string
	command string
}

// envReply is how a test server answers an env request: as an sshd whose
// AcceptEnv takes the variable, as one whose AcceptEnv does not, or never.
type envReply int

const (
	envAccept envReply = iota
	envRefuse
	envNever
)

// startExecServer runs an in-process SSH server on 127.0.0.1 that takes
// user/secret, records each session's env requests and exec command, and
// answers the command with exit status 0, and env requests as env says.
func startExecServer(t *testing.T, env envReply) (int, <-chan execSeen) {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
		if c.User() == "user" && string(pass) == "secret" {
			return nil, nil
		}
		return nil, ssh.ErrNoAuth
	}}
	cfg.AddHostKey(signer)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	seen := make(chan execSeen, 4)
	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			go serveExecConn(nc, cfg, env, seen)
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port, seen
}

func serveExecConn(nc net.Conn, cfg *ssh.ServerConfig, env envReply, seen chan<- execSeen) {
	_, chans, reqs, err := ssh.NewServerConn(nc, cfg)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(reqs)
	for nch := range chans {
		ch, in, err := nch.Accept()
		if err != nil {
			return
		}
		go func() {
			s := execSeen{env: map[string]string{}}
			for req := range in {
				switch req.Type {
				case "env":
					if env == envNever {
						continue
					}
					var kv struct{ Name, Value string }
					if ssh.Unmarshal(req.Payload, &kv) == nil && env == envAccept {
						s.env[kv.Name] = kv.Value
						req.Reply(true, nil)
						continue
					}
					req.Reply(false, nil)
				case "exec":
					var c struct{ Command string }
					_ = ssh.Unmarshal(req.Payload, &c)
					s.command = c.Command
					req.Reply(true, nil)
					seen <- s
					_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
					ch.Close()
				default:
					req.Reply(false, nil)
				}
			}
		}()
	}
}

// Every command a workflow runs over ssh carries the run's marker: requested
// as a variable, and on this machine (here 127.0.0.1) exported in the command
// as well, for an sshd whose AcceptEnv drops it (C10).
func TestSSHCommandsCarryTheRunMarker(t *testing.T) {
	standInOS(t, "linux")
	const id = "exec-'7" // a quote, to be escaped
	want := "export " + workflow.EnvWorkflowRun + `='exec-'\''7'; echo hi`
	for _, env := range []envReply{envAccept, envRefuse} {
		accept := env == envAccept
		port, seen := startExecServer(t, env)
		cfg := map[string]interface{}{"host": "127.0.0.1", "port": float64(port), "username": "user",
			"password": "secret", "command": "echo hi", "timeout_seconds": float64(10)}
		out, err := (&SSHNode{}).Execute(context.Background(), workflow.NodeInput{ExecutionID: id}, cfg)
		if err != nil || out[0].Handle != "main" {
			t.Fatalf("accept env %v: %v %v", accept, out, err)
		}
		s := <-seen
		if s.command != want {
			t.Errorf("accept env %v: command %q, want %q", accept, s.command, want)
		}
		if got := s.env[workflow.EnvWorkflowRun]; accept && got != id || !accept && got != "" {
			t.Errorf("accept env %v: %s=%q", accept, workflow.EnvWorkflowRun, got)
		}
	}
	// Another host gets the variable requested and its command as written:
	// a Windows host's cmd.exe has no export.
	port, seen := startExecServer(t, envAccept)
	old := sshTargetIsLocal
	t.Cleanup(func() { sshTargetIsLocal = old })
	sshTargetIsLocal = func(context.Context, string) bool { return false }
	cfg := map[string]interface{}{"host": "127.0.0.1", "port": float64(port), "username": "user",
		"password": "secret", "command": "echo hi", "timeout_seconds": float64(10)}
	if _, err := (&SSHNode{}).Execute(context.Background(), workflow.NodeInput{ExecutionID: id}, cfg); err != nil {
		t.Fatal(err)
	}
	if s := <-seen; s.command != "echo hi" || s.env[workflow.EnvWorkflowRun] != id {
		t.Errorf("another host: command %q, %s=%q", s.command, workflow.EnvWorkflowRun, s.env[workflow.EnvWorkflowRun])
	}
	sshTargetIsLocal = old
	if got := markedCommand("", "true"); got != "export "+workflow.EnvWorkflowRun+"='node-run'; true" {
		t.Errorf("a node run outside an execution: %q", got)
	}
	// The export gives a POSIX shell the marker as it was.
	if runtime.GOOS == "windows" {
		return
	}
	got, err := exec.Command("sh", "-c", markedCommand(id, `printf %s "$`+workflow.EnvWorkflowRun+`"`)).Output()
	if err != nil || string(got) != id {
		t.Errorf("sh read %q (%v), want %q", got, err, id)
	}
}

// standInOS makes this test's machine run goos.
func standInOS(t *testing.T, name string) {
	t.Helper()
	old := goos
	t.Cleanup(func() { goos = old })
	goos = name
}

// sshNodeConfig is a command node's config for a test server on port.
func sshNodeConfig(port, timeoutSeconds int) map[string]interface{} {
	return map[string]interface{}{"host": "127.0.0.1", "port": float64(port), "username": "user",
		"password": "secret", "command": "echo hi", "timeout_seconds": float64(timeoutSeconds)}
}

// On Windows this machine's sshd runs commands in cmd.exe, which has no
// export: the command goes as written, and the variable is still requested.
func TestSSHOnWindowsKeepsTheCommandAsWritten(t *testing.T) {
	standInOS(t, "windows")
	port, seen := startExecServer(t, envAccept)
	if _, err := (&SSHNode{}).Execute(context.Background(), workflow.NodeInput{ExecutionID: "exec-7"}, sshNodeConfig(port, 10)); err != nil {
		t.Fatal(err)
	}
	if s := <-seen; s.command != "echo hi" || s.env[workflow.EnvWorkflowRun] != "exec-7" {
		t.Errorf("command %q, %s=%q", s.command, workflow.EnvWorkflowRun, s.env[workflow.EnvWorkflowRun])
	}
}

// A server that never answers the environment request holds the node no
// longer than its timeout, or its context.
func TestSSHEnvRequestObeysTheNodesTimeout(t *testing.T) {
	port, _ := startExecServer(t, envNever)
	run := func(ctx context.Context, timeoutSeconds int) error {
		t.Helper()
		done := make(chan error, 1)
		go func() {
			_, err := (&SSHNode{}).Execute(ctx, workflow.NodeInput{ExecutionID: "exec-7"}, sshNodeConfig(port, timeoutSeconds))
			done <- err
		}()
		select {
		case err := <-done:
			return err
		case <-time.After(10 * time.Second):
			t.Fatal("the node still waits on the environment request")
			return nil
		}
	}
	begin := time.Now()
	if err := run(context.Background(), 1); err == nil || !strings.Contains(err.Error(), "timed out after 1s") {
		t.Errorf("the timeout: %v", err)
	}
	if d := time.Since(begin); d > 5*time.Second {
		t.Errorf("took %v for a 1s timeout", d)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := run(ctx, 30); err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Errorf("the context: %v", err)
	}
}
