package core

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

func fakeDNSResolver(t *testing.T, mode string) *net.Resolver {
	t.Helper()
	server, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	go func() {
		buf := make([]byte, 1500)
		for {
			n, remote, readErr := server.ReadFrom(buf)
			if readErr != nil {
				return
			}
			if mode == "silent" || n < 17 {
				continue
			}
			query := append([]byte(nil), buf[:n]...)
			end := 12
			for end < len(query) {
				labelLength := int(query[end])
				end++
				if labelLength == 0 {
					break
				}
				end += labelLength
			}
			if end+4 > len(query) {
				continue
			}
			qType := binary.BigEndian.Uint16(query[end : end+2])
			end += 4
			reply := make([]byte, 12)
			copy(reply[:2], query[:2])
			flags := uint16(0x8180)
			if mode == "nxdomain" {
				flags = 0x8183
			}
			binary.BigEndian.PutUint16(reply[2:4], flags)
			binary.BigEndian.PutUint16(reply[4:6], 1)
			reply = append(reply, query[12:end]...)
			if mode == "success" && qType == 1 {
				binary.BigEndian.PutUint16(reply[6:8], 1)
				reply = append(reply, 0xc0, 0x0c, 0, 1, 0, 1, 0, 0, 0, 30, 0, 4, 127, 0, 0, 42)
			}
			_, _ = server.WriteTo(reply, remote)
		}
	}()
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "udp", server.LocalAddr().String())
		},
	}
}

func TestDNSSystemDefaultIsNotReplaced(t *testing.T) {
	original := net.DefaultResolver
	resolver := fakeDNSResolver(t, "success")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	peer, err := resolvePeerUDPAddr(ctx, resolver, "example.test.:443")
	if err != nil {
		t.Fatal(err)
	}
	if peer.IP.String() != "127.0.0.42" || peer.Port != 443 {
		t.Fatalf("peer = %s", peer)
	}
	if net.DefaultResolver != original {
		t.Fatal("global default resolver was replaced")
	}
}

func TestDNSNXDOMAIN(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := resolvePeerUDPAddr(ctx, fakeDNSResolver(t, "nxdomain"), "missing.test.:443")
	var dnsErr *net.DNSError
	if !errors.As(err, &dnsErr) || !dnsErr.IsNotFound {
		t.Fatalf("expected DNS not found, got %v", err)
	}
	if got := dnsFailureKind(err); got != "not_found" {
		t.Fatalf("kind=%q", got)
	}
}

func TestDNSNoResponseRespectsDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := resolvePeerUDPAddr(ctx, fakeDNSResolver(t, "silent"), "silent.test.:443")
	if err == nil || dnsFailureKind(err) != "timeout" {
		t.Fatalf("expected timeout, got %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("DNS lookup ignored context deadline")
	}
}

func TestDNSCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := resolvePeerUDPAddr(ctx, fakeDNSResolver(t, "silent"), "cancel.test.:443")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}

func TestDNSLiteralDoesNotRequireResolver(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	peer, err := resolvePeerUDPAddr(ctx, fakeDNSResolver(t, "silent"), "192.0.2.1:51820")
	if err != nil || peer.IP.String() != "192.0.2.1" {
		t.Fatalf("literal peer=%v err=%v", peer, err)
	}
}

func TestDNSFailureRedactsHostnameAndDoesNotInvalidateHash(t *testing.T) {
	raw := &net.DNSError{Err: "i/o timeout", Name: "api.vk.me", IsTimeout: true}
	safe := sanitizeDNSFailure(raw)
	if !isDNSFailure(safe) || dnsFailureKind(safe) != "timeout" {
		t.Fatalf("unexpected DNS classification: %v", safe)
	}
	if strings.Contains(safe.Error(), "api.vk.me") {
		t.Fatalf("DNS error exposed hostname: %v", safe)
	}
	result := classifyHashProbeCredentialError(safe, time.Now())
	if result.Status != HashProbeError || result.ErrorType != HashProbeErrorNetwork {
		t.Fatalf("status=%q type=%q", result.Status, result.ErrorType)
	}
}
