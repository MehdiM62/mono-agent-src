//go:build linux

package httpnodes

import (
	"context"
	"net"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/monoes/mono-agent/internal/workflow"
)

// blackhole listens on 127.0.0.2:port with its accept queue full, so a
// connect to it gets no answer (the kernel drops its SYN), like an address
// behind a broken route.
func blackhole(t *testing.T, port int) {
	t.Helper()
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Skip(err)
	}
	t.Cleanup(func() { syscall.Close(fd) })
	if err := syscall.Bind(fd, &syscall.SockaddrInet4{Port: port, Addr: [4]byte{127, 0, 0, 2}}); err != nil {
		t.Skip(err)
	}
	if err := syscall.Listen(fd, 0); err != nil {
		t.Skip(err)
	}
	addr := net.JoinHostPort("127.0.0.2", strconv.Itoa(port))
	for range 2 { // fill the queue
		if c, err := net.DialTimeout("tcp", addr, 300*time.Millisecond); err == nil {
			t.Cleanup(func() { c.Close() })
		}
	}
	if c, err := net.DialTimeout("tcp", addr, 300*time.Millisecond); err == nil {
		c.Close()
		t.Skip("the accept queue does not fill on this kernel")
	}
}

// A host whose first address never answers (a broken IPv6 route ahead of a
// working IPv4 one) still connects through the next address within
// timeout_seconds: each address gets a share of the connect deadline.
func TestSSHFallsBackFromAnAddressThatNeverAnswers(t *testing.T) {
	port, seen := startExecServer(t, envAccept)
	blackhole(t, port)
	old := lookupHost
	t.Cleanup(func() { lookupHost = old })
	lookupHost = func(context.Context, string) ([]string, error) { return []string{"127.0.0.2", "127.0.0.1"}, nil }
	const timeout = 4 * time.Second
	cfg := map[string]interface{}{"host": "dual.test", "port": float64(port), "username": "user",
		"password": "secret", "command": "echo hi", "timeout_seconds": timeout.Seconds()}
	start := time.Now()
	out, err := (&SSHNode{}).Execute(context.Background(), workflow.NodeInput{ExecutionID: "exec-1"}, cfg)
	if took := time.Since(start); err != nil || took > timeout {
		t.Fatalf("after %s: %v", took, err)
	}
	if out[0].Handle != "main" {
		t.Fatalf("handle %s", out[0].Handle)
	}
	select {
	case <-seen:
	case <-time.After(time.Second):
		t.Fatal("the command did not reach the working address")
	}
}
