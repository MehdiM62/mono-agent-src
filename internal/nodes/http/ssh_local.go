package httpnodes

import (
	"context"
	"net"
	"strings"
)

// sshTargetIsLocal reports whether an ssh node's host is this machine (a
// variable so tests can stand in a remote host).
var sshTargetIsLocal = isThisMachine

// lookupHost and lookupCNAME resolve a name (variables so tests can stand
// in a resolver).
var (
	lookupHost  = net.DefaultResolver.LookupHost
	lookupCNAME = net.DefaultResolver.LookupCNAME
)

// isThisMachine reports whether host is this machine: localhost, a loopback
// or unspecified address, one of its interface addresses, its name
// (thisMachine) or its canonical name, or a name any address of which is one
// of those. Unlike ownHost it resolves the name: an ssh session that lands
// here runs commands as the user, whatever name led to it. A name that does
// not resolve is not this machine.
func isThisMachine(ctx context.Context, host string) bool {
	h := strings.TrimSuffix(strings.Trim(strings.TrimSpace(host), "[]"), ".")
	if h == "" {
		return false
	}
	lower := strings.ToLower(h)
	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") {
		return true
	}
	local := func(ip net.IP) bool { return ip.IsLoopback() || thisMachine("", ip) }
	if ip := parseIP(h); ip != nil {
		return local(ip)
	}
	if thisMachine(h, nil) {
		return true
	}
	if name, err := hostname(); err == nil && name != "" {
		if fqdn, err := lookupCNAME(ctx, name); err == nil && strings.EqualFold(strings.TrimSuffix(fqdn, "."), h) {
			return true
		}
	}
	addrs, err := lookupHost(ctx, h)
	if err != nil {
		return false
	}
	for _, a := range addrs {
		if ip := parseIP(a); ip != nil && local(ip) {
			return true
		}
	}
	return false
}
