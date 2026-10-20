package core

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/dtls/v3"
	"github.com/pion/dtls/v3/pkg/crypto/selfsign"
)

// issue51WrappedPacketConn simulates the WRAP portion of a TURN data path.
// It uses real localhost UDP and the production obfs encoder/decoder; it does
// not contact VK, a public TURN relay, or the user's remote server.
type issue51WrappedPacketConn struct {
	net.PacketConn
	key      []byte
	config   *ObfsConfig
	state    *ObfsState
	outbound atomic.Int64
	inbound  atomic.Int64
	rejected atomic.Int64
}

func issue51WrapConn(conn net.PacketConn, key []byte, mode string) *issue51WrappedPacketConn {
	return &issue51WrappedPacketConn{
		PacketConn: conn,
		key:        key,
		config:     NewObfsConfig(mode),
		state:      NewObfsState(),
	}
}

func (c *issue51WrappedPacketConn) WriteTo(payload []byte, peer net.Addr) (int, error) {
	wire, err := obfsWrapPacket(c.key, payload, c.config, c.state)
	if err != nil {
		return 0, err
	}
	_, err = c.PacketConn.WriteTo(wire, peer)
	if err != nil {
		return 0, err
	}
	c.outbound.Add(1)
	return len(payload), nil
}

func (c *issue51WrappedPacketConn) ReadFrom(dst []byte) (int, net.Addr, error) {
	wire := make([]byte, readBufSize+80)
	for {
		n, peer, err := c.PacketConn.ReadFrom(wire)
		if err != nil {
			return 0, nil, err
		}
		if !obfsIsRTPPacket(wire[:n]) {
			c.rejected.Add(1)
			continue
		}
		plainLen, err := obfsUnwrapPacket(c.key, wire[:n], dst)
		if err != nil {
			c.rejected.Add(1)
			continue
		}
		c.inbound.Add(1)
		return plainLen, peer, nil
	}
}

func issue51DTLSConfig(t *testing.T) *dtls.Config {
	t.Helper()
	cert, err := selfsign.GenerateSelfSigned()
	if err != nil {
		t.Fatal(err)
	}
	return &dtls.Config{
		Certificates:          []tls.Certificate{cert},
		InsecureSkipVerify:    true, // loopback fixture only, same as current client
		ExtendedMasterSecret:  dtls.RequireExtendedMasterSecret,
		CipherSuites:          []dtls.CipherSuiteID{dtls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256},
		ConnectionIDGenerator: dtls.OnlySendCIDGenerator(),
		FlightInterval:        50 * time.Millisecond,
	}
}

func issue51ListenUDP(t *testing.T) net.PacketConn {
	t.Helper()
	conn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func TestIssue51DTLSHandshakeOverWRAP(t *testing.T) {
	for _, mode := range []string{"audio", "video"} {
		t.Run(mode, func(t *testing.T) {
			key := bytes.Repeat([]byte{0x2a}, wrapKeyLen)
			client := issue51WrapConn(issue51ListenUDP(t), key, mode)
			server := issue51WrapConn(issue51ListenUDP(t), key, mode)
			cfg := issue51DTLSConfig(t)

			serverDTLS, err := dtls.Server(server, client.LocalAddr(), cfg)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = serverDTLS.Close() })
			clientDTLS, err := dtls.Client(client, server.LocalAddr(), cfg)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = clientDTLS.Close() })

			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			serverResult := make(chan error, 1)
			go func() { serverResult <- serverDTLS.HandshakeContext(ctx) }()
			if err := clientDTLS.HandshakeContext(ctx); err != nil {
				t.Fatalf("client DTLS handshake over %s WRAP: %v", mode, err)
			}
			if err := <-serverResult; err != nil {
				t.Fatalf("server DTLS handshake over %s WRAP: %v", mode, err)
			}

			message := []byte("issue-51-bidirectional-dtls")
			if _, err := clientDTLS.Write(message); err != nil {
				t.Fatal(err)
			}
			_ = serverDTLS.SetReadDeadline(time.Now().Add(2 * time.Second))
			got := make([]byte, 256)
			n, err := serverDTLS.Read(got)
			if err != nil || !bytes.Equal(got[:n], message) {
				t.Fatalf("server received %q: %v", got[:n], err)
			}
			if _, err := serverDTLS.Write(message); err != nil {
				t.Fatal(err)
			}
			_ = clientDTLS.SetReadDeadline(time.Now().Add(2 * time.Second))
			n, err = clientDTLS.Read(got)
			if err != nil || !bytes.Equal(got[:n], message) {
				t.Fatalf("client received %q: %v", got[:n], err)
			}
			if client.outbound.Load() == 0 || client.inbound.Load() == 0 ||
				server.outbound.Load() == 0 || server.inbound.Load() == 0 {
				t.Fatalf("missing two-way wrapped traffic: client=%d/%d server=%d/%d",
					client.outbound.Load(), client.inbound.Load(),
					server.outbound.Load(), server.inbound.Load())
			}
			if client.rejected.Load() != 0 || server.rejected.Load() != 0 {
				t.Fatalf("unexpected WRAP rejects: client=%d server=%d",
					client.rejected.Load(), server.rejected.Load())
			}
		})
	}
}

// A successful local UDP send cannot prove that the peer responds with DTLS.
// The test makes a raw UDP listener silently consume the ClientHello.
func TestIssue51SilentPeerYieldsDTLSTimeout(t *testing.T) {
	key := bytes.Repeat([]byte{0x2a}, wrapKeyLen)
	client := issue51WrapConn(issue51ListenUDP(t), key, "audio")
	silentPeer := issue51ListenUDP(t)
	seen := make(chan bool, 1)
	go func() {
		packet := make([]byte, 1800)
		n, _, err := silentPeer.ReadFrom(packet)
		seen <- err == nil && obfsIsRTPPacket(packet[:n])
	}()
	cfg := issue51DTLSConfig(t)
	clientDTLS, err := dtls.Client(client, silentPeer.LocalAddr(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = clientDTLS.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 450*time.Millisecond)
	defer cancel()
	if err := clientDTLS.HandshakeContext(ctx); !isDTLSTimeout(err) {
		t.Fatalf("silent peer should give a DTLS timeout, got %v", err)
	}
	if !<-seen {
		t.Fatal("the peer never received a valid WRAP-encoded DTLS ClientHello")
	}
	if client.outbound.Load() == 0 || client.inbound.Load() != 0 {
		t.Fatalf("expected outbound-only packets, got sent=%d inbound=%d",
			client.outbound.Load(), client.inbound.Load())
	}
}

func TestIssue51MismatchedWRAPKeyCannotEstablishDTLS(t *testing.T) {
	clientKey := bytes.Repeat([]byte{0x2a}, wrapKeyLen)
	serverKey := bytes.Repeat([]byte{0x3b}, wrapKeyLen)
	client := issue51WrapConn(issue51ListenUDP(t), clientKey, "video")
	peer := issue51ListenUDP(t)
	unwrapResult := make(chan error, 1)
	go func() {
		packet := make([]byte, 1800)
		n, _, err := peer.ReadFrom(packet)
		if err != nil {
			unwrapResult <- err
			return
		}
		out := make([]byte, 1800)
		_, err = obfsUnwrapPacket(serverKey, packet[:n], out)
		unwrapResult <- err
	}()
	clientDTLS, err := dtls.Client(client, peer.LocalAddr(), issue51DTLSConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = clientDTLS.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 450*time.Millisecond)
	defer cancel()
	if err := clientDTLS.HandshakeContext(ctx); !isDTLSTimeout(err) {
		t.Fatalf("mismatched WRAP key should time out DTLS handshake, got %v", err)
	}
	if err := <-unwrapResult; err == nil || !errors.Is(err, errors.ErrUnsupported) && err.Error() == "" {
		t.Fatalf("incorrect WRAP key unexpectedly accepted: %v", err)
	}
}
