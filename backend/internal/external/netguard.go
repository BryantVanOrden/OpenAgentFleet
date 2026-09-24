package external

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"
)

// Where webhook and OpenClaw agents may be reached.
//
// The orchestrator POSTs a run's brief to a webhook agent's address and shows
// what comes back as the ticket's result, so an address is a request the
// orchestrator makes on someone's behalf from inside its own network. Left
// open, anyone who could add an agent could point one at the API itself, the
// database, or a cloud metadata service and read the answer. Loopback and
// link-local addresses (where metadata services live) are refused outright;
// a private-network address -- a gateway on the LAN is a normal thing to
// have -- only when an admin added the agent (AgentConnection.AllowPrivate).
//
// The host is resolved here and the checked address is the one dialled, so a
// name that resolves somewhere harmless at creation and somewhere else later
// is caught at the moment it matters.

// AllowLoopback lets a single-machine setup (and the tests) reach agents on
// 127.0.0.1. AGENTFLEET_EXTERNAL_ALLOW_LOOPBACK=1.
var AllowLoopback = os.Getenv("AGENTFLEET_EXTERNAL_ALLOW_LOOPBACK") == "1"

// ErrAddressRefused is returned when an agent's address is not one the
// orchestrator will connect to.
var ErrAddressRefused = errors.New("address refused")

var cgnat = mustCIDR("100.64.0.0/10") // Tailscale and carrier NAT: a private network in practice

func mustCIDR(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return n
}

// IsPrivate reports whether an address is on a private network.
func IsPrivate(ip net.IP) bool {
	return ip.IsPrivate() || cgnat.Contains(ip)
}

// refusal says why an address is never dialled, or "" when it may be.
func refusal(ip net.IP, allowPrivate bool) string {
	switch {
	case ip.IsLoopback():
		if AllowLoopback {
			return ""
		}
		return "it is this machine (loopback)"
	case ip.IsLinkLocalUnicast(), ip.IsLinkLocalMulticast():
		return "it is a link-local address, where cloud metadata services live"
	case ip.IsUnspecified(), ip.IsMulticast(), ip.IsInterfaceLocalMulticast():
		return "it is not a host address"
	case IsPrivate(ip) && !allowPrivate:
		return "it is on a private network, and only an admin can point an agent there"
	}
	return ""
}

type allowPrivateKey struct{}

// WithAllowPrivate marks a context whose dials may reach private networks.
func WithAllowPrivate(ctx context.Context, allow bool) context.Context {
	return context.WithValue(ctx, allowPrivateKey{}, allow)
}

func allowPrivateFrom(ctx context.Context) bool {
	v, _ := ctx.Value(allowPrivateKey{}).(bool)
	return v
}

var resolver = net.DefaultResolver

// guardedDial resolves the host, refuses any address it may not reach, and
// dials one it may.
func guardedDial(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := lookup(ctx, host)
	if err != nil {
		return nil, err
	}
	allow := allowPrivateFrom(ctx)
	d := net.Dialer{Timeout: 20 * time.Second}
	var last error
	for _, ip := range ips {
		if why := refusal(ip, allow); why != "" {
			last = fmt.Errorf("%w: not connecting to %s (%s): %s", ErrAddressRefused, host, ip, why)
			continue
		}
		c, err := d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return c, nil
		}
		last = err
	}
	if last == nil {
		last = fmt.Errorf("%s has no address", host)
	}
	return nil, last
}

func lookup(ctx context.Context, host string) ([]net.IP, error) {
	if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil {
		return []net.IP{ip}, nil
	}
	addrs, err := resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	ips := make([]net.IP, 0, len(addrs))
	for _, a := range addrs {
		ips = append(ips, a.IP)
	}
	return ips, nil
}

// CheckHost is the check made when an agent is added or its address edited:
// whether the host resolves anywhere it may never go, and whether it is on a
// private network. A host that does not resolve yet passes; it is checked
// again on every connection.
func CheckHost(ctx context.Context, host string) (private bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	ips, lerr := lookup(ctx, host)
	if lerr != nil {
		return false, nil
	}
	for _, ip := range ips {
		if why := refusal(ip, true); why != "" {
			return false, fmt.Errorf("%w: %s resolves to %s, and %s", ErrAddressRefused, host, ip, why)
		}
		if IsPrivate(ip) {
			private = true
		}
	}
	return private, nil
}
