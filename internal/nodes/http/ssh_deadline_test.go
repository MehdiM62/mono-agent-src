package httpnodes

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/monoes/mono-agent/internal/workflow"
)

// startStallServer runs an in-process SSH server on 127.0.0.1 that stalls:
// in the handshake (it never says a word), or after it (it never answers a
// channel open). Its connections close when the test ends.
func startStallServer(t *testing.T, inHandshake bool) int {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{NoClientAuth: true}
	cfg.AddHostKey(signer)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var conns []net.Conn
	t.Cleanup(func() {
		ln.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range conns {
			c.Close()
		}
	})
	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, nc)
			mu.Unlock()
			if inHandshake {
				continue // read nothing, write nothing
			}
			go func() {
				_, chans, reqs, err := ssh.NewServerConn(nc, cfg)
				if err != nil {
					return
				}
				go ssh.DiscardRequests(reqs)
				for range chans { // never accepted, never rejected
				}
			}()
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

// The handshake and the session open wait for the server within the node's
// timeout and context: a server that stalls in either can't hold the node.
func TestSSHStallingServerCantHoldTheNode(t *testing.T) {
	for _, tc := range []struct {
		name        string
		inHandshake bool
		extra       map[string]interface{}
		want        string
	}{
		{"handshake", true, nil, "waiting for the ssh handshake"},
		{"session", false, nil, "waiting for the server to open a session"},
		{"sftp session", false, map[string]interface{}{"resource": "file", "operation": "download", "path": "/etc/hostname"}, "waiting for the server to open an sftp session"},
	} {
		port := startStallServer(t, tc.inHandshake)
		cfg := map[string]interface{}{"host": "127.0.0.1", "port": float64(port), "username": "user",
			"password": "x", "command": "true", "timeout_seconds": float64(1)}
		for k, v := range tc.extra {
			cfg[k] = v
		}
		start := time.Now()
		_, err := (&SSHNode{}).Execute(context.Background(), workflow.NodeInput{ExecutionID: "e1"}, cfg)
		if took := time.Since(start); took > 5*time.Second {
			t.Errorf("%s: the node took %s with a 1 s timeout", tc.name, took)
		}
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v", tc.name, err)
		}
	}

	// Cancelling the run ends a stalled handshake at once.
	port := startStallServer(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	cfg := map[string]interface{}{"host": "127.0.0.1", "port": float64(port), "username": "user",
		"password": "x", "command": "true", "timeout_seconds": float64(30)}
	start := time.Now()
	_, err := (&SSHNode{}).Execute(ctx, workflow.NodeInput{ExecutionID: "e1"}, cfg)
	if took := time.Since(start); took > 5*time.Second || err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Errorf("cancelled handshake: %s %v", took, err)
	}
}
