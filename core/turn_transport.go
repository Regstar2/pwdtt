package core

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/pion/turn/v5"
)

// dialTURNConn connects only the client-to-TURN leg. The relayed DTLS
// handshake remains datagram based regardless of the selected transport.
func dialTURNConn(ctx context.Context, address string, tcp bool) (net.PacketConn, error) {
	if tcp {
		dialer := net.Dialer{Timeout: 10 * time.Second}
		conn, err := dialer.DialContext(ctx, "tcp", address)
		if err != nil { return nil, fmt.Errorf("подключение TURN TCP: %w", err) }
		return turn.NewSTUNConn(conn), nil
	}

	resolved, err := net.ResolveUDPAddr("udp", address)
	if err != nil { return nil, fmt.Errorf("резолв TURN: %w", err) }
	conn, err := net.DialUDP("udp", nil, resolved)
	if err != nil { return nil, fmt.Errorf("подключение TURN UDP: %w", err) }
	_ = conn.SetReadBuffer(socketBufSize)
	_ = conn.SetWriteBuffer(socketBufSize)
	return &connectedUDPConn{conn}, nil
}
