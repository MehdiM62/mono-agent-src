package httpnodes

import (
	"context"
	"errors"
	"fmt"
	"net"
	"runtime"
	"strings"
)

// sshAddrIsLocal reports whether an address an ssh node would dial is this
// machine (a variable so tests can stand in a remote host).
var sshAddrIsLocal = func(ip net.IP) bool { return ip.IsLoopback() || thisMachine("", ip) }

// goos is the OS this machine runs (a variable so tests can stand in
// Windows).
var goos = runtime.GOOS

// exportsMarker reports whether an ssh command to a target gets the run
// marker's export: on this machine, unless it runs Windows, whose OpenSSH
// runs commands in cmd.exe, which has no export.
func exportsMarker(t sshTarget) bool {
	return goos != "windows" && t.local
}

// lookupHost and lookupCNAME resolve a name (variables so tests can stand
// in a resolver).
var (
	lookupHost  = net.DefaultResolver.LookupHost
	lookupCNAME = net.DefaultResolver.LookupCNAME
)

// sshTarget is an ssh node's host resolved once: the addresses it dials,
// in order, and whether it is this machine. local is decided on the very
// addresses dialled, so a name can't resolve to another host for the check
// and to this machine for the dial (DNS rebinding).
type sshTarget struct {
	dial  []string // what to dial: the addresses, or the name itself when it names this machine
	local bool
}

// resolveSSHTarget resolves host once. It is this machine when it is
// localhost, a loopback or unspecified address, one of its interface
// addresses, its name (thisMachine) or its canonical name, or when any
// address the name resolves to is one of those; a name that names this
// machine is dialled as it is, since it is local whatever it resolves to.
// Unlike ownHost it resolves the name: an ssh session that lands here runs
// commands as the user, whatever name led to it.
func resolveSSHTarget(ctx context.Context, host string) (sshTarget, error) {
	h := strings.TrimSuffix(strings.Trim(strings.TrimSpace(host), "[]"), ".")
	if h == "" {
		return sshTarget{}, errors.New("no host")
	}
	if ip := parseIP(h); ip != nil {
		return sshTarget{dial: []string{h}, local: sshAddrIsLocal(ip)}, nil
	}
	lower := strings.ToLower(h)
	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") || thisMachine(h, nil) {
		return sshTarget{dial: []string{h}, local: true}, nil
	}
	if name, err := hostname(); err == nil && name != "" {
		if fqdn, err := lookupCNAME(ctx, name); err == nil && strings.EqualFold(strings.TrimSuffix(fqdn, "."), h) {
			return sshTarget{dial: []string{h}, local: true}, nil
		}
	}
	addrs, err := lookupHost(ctx, h)
	if err != nil {
		return sshTarget{}, err
	}
	t := sshTarget{}
	for _, a := range addrs {
		ip := parseIP(a)
		if ip == nil {
			continue
		}
		t.dial = append(t.dial, a)
		if sshAddrIsLocal(ip) {
			t.local = true
		}
	}
	if len(t.dial) == 0 {
		return sshTarget{}, fmt.Errorf("%s resolves to no address", h)
	}
	return t, nil
}

// isThisMachine reports whether host is this machine (resolveSSHTarget); a
// name that does not resolve is not.
func isThisMachine(ctx context.Context, host string) bool {
	t, err := resolveSSHTarget(ctx, host)
	return err == nil && t.local
}
