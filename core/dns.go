package core

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
)

type dnsUnavailableError struct {
	kind string
}

func (e *dnsUnavailableError) Error() string {
	return "DNS/network unavailable (" + e.kind + ")"
}

func dnsFailureKind(err error) string {
	if err == nil {
		return "success"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var unavailable *dnsUnavailableError
	if errors.As(err, &unavailable) {
		return unavailable.kind
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		if dnsErr.IsNotFound {
			return "not_found"
		}
		if dnsErr.IsTimeout {
			return "timeout"
		}
		return "network"
	}
	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "timeout") || strings.Contains(lower, "timed out") {
		return "timeout"
	}
	return "network"
}

func isDNSFailure(err error) bool {
	if err == nil {
		return false
	}
	var unavailable *dnsUnavailableError
	var dnsErr *net.DNSError
	if errors.As(err, &unavailable) || errors.As(err, &dnsErr) {
		return true
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "no such host") ||
		strings.Contains(lower, "server misbehaving") ||
		(strings.Contains(lower, "lookup ") &&
			(strings.Contains(lower, "timeout") ||
				strings.Contains(lower, "temporary") ||
				strings.Contains(lower, "network") ||
				strings.Contains(lower, "refused")))
}

// sanitizeDNSFailure keeps DNS errors free of request URLs and hostnames.
func sanitizeDNSFailure(err error) error {
	if isDNSFailure(err) {
		return &dnsUnavailableError{kind: dnsFailureKind(err)}
	}
	return err
}

// resolvePeerUDPAddr uses the OS configuration without modifying net.DefaultResolver.
// The optional resolver parameter exists for isolated tests.
func resolvePeerUDPAddr(ctx context.Context, resolver *net.Resolver, address string) (*net.UDPAddr, error) {
	address = strings.TrimSpace(address)
	host, portString, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(portString)
	if err != nil || port < 0 || port > 65535 {
		return nil, fmt.Errorf("invalid UDP port")
	}
	if host == "" || net.ParseIP(host) != nil || strings.Contains(host, "%") {
		return net.ResolveUDPAddr("udp", address)
	}
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	addresses, err := resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(addresses) == 0 {
		return nil, &net.DNSError{Err: "no addresses returned", Name: host, IsNotFound: true}
	}
	return &net.UDPAddr{IP: addresses[0].IP, Port: port, Zone: addresses[0].Zone}, nil
}
