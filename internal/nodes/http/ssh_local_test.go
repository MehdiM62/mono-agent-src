package httpnodes

import (
	"context"
	"errors"
	"net"
	"testing"
)

// resolveAs makes names resolve to the given addresses for the test; any
// other name does not resolve, and no name has a canonical one.
func resolveAs(t *testing.T, names map[string][]string) {
	t.Helper()
	oldHost, oldCNAME := lookupHost, lookupCNAME
	t.Cleanup(func() { lookupHost, lookupCNAME = oldHost, oldCNAME })
	lookupHost = func(_ context.Context, h string) ([]string, error) {
		if a, ok := names[h]; ok {
			return a, nil
		}
		return nil, errors.New("no such host")
	}
	lookupCNAME = func(context.Context, string) (string, error) { return "", errors.New("no canonical name") }
}

// An ssh host is this machine when it is, or resolves to, a loopback
// address, one of this machine's addresses or its name; anything else, a
// name that does not resolve included, is another host (C10).
func TestSSHTargetIsLocal(t *testing.T) {
	ctx := context.Background()
	standIn(t, "studio.example.lan", "192.168.1.20/24", "fe80::1/64")
	resolveAs(t, map[string][]string{
		"alias.test":  {"127.0.0.1"},
		"lan.test":    {"203.0.113.9", "192.168.1.20"},
		"remote.test": {"203.0.113.5"},
	})
	for host, want := range map[string]bool{
		"localhost": true, "LOCALHOST.": true, "app.localhost": true, "127.0.0.1": true, "127.1.2.3": true,
		"::1": true, "[::1]": true, "0.0.0.0": true, "192.168.1.20": true, "fe80::1%eth0": true,
		"studio.example.lan": true, "studio": true, "alias.test": true, "lan.test": true,
		"192.0.2.10": false, "203.0.113.5": false, "remote.test": false, "missing.test": false, "": false,
	} {
		if got := isThisMachine(ctx, host); got != want {
			t.Errorf("%q: local %v, want %v", host, got, want)
		}
	}
	// The machine's canonical name.
	lookupCNAME = func(_ context.Context, name string) (string, error) {
		if name == "studio.example.lan" {
			return "box.corp.example.", nil
		}
		return "", errors.New("no canonical name")
	}
	if !isThisMachine(ctx, "box.corp.example") {
		t.Error("the machine's canonical name is not local")
	}
}

// The real machine: localhost and its own addresses are local.
func TestSSHTargetIsLocalOnThisMachine(t *testing.T) {
	ctx := context.Background()
	if !isThisMachine(ctx, "localhost") {
		t.Error("localhost is not local")
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		t.Skip(err)
	}
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && !n.IP.IsLoopback() {
			if !isThisMachine(ctx, n.IP.String()) {
				t.Errorf("this machine's address %s is not local", n.IP)
			}
		}
	}
	if isThisMachine(ctx, "192.0.2.10") {
		t.Error("a TEST-NET address is local")
	}
}
