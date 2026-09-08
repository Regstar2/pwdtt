package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	core "wg-turn-client"
)

func main() {
	hash := flag.String("hash", "", "VK call hash (the part after /call/join/)")
	target := flag.String("target", "example.com:80", "TCP target in host:port form")
	mode := flag.String("mode", "connect", "probe mode: connect or http")
	timeout := flag.Duration("timeout", 45*time.Second, "overall probe timeout")
	flag.Parse()

	if strings.TrimSpace(*hash) == "" {
		fmt.Fprintln(os.Stderr, "error: -hash is required")
		flag.Usage()
		os.Exit(2)
	}

	payload, err := probePayload(*mode, *target)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(2)
	}

	fmt.Println("TURN TCP RFC 6062 probe")
	fmt.Printf("Target: %s\n", *target)
	fmt.Printf("Mode: %s\n", strings.ToLower(strings.TrimSpace(*mode)))
	fmt.Println("Obtaining VK TURN credentials and trying TCP allocation...")

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	result, err := core.ProbeTurnTCP(ctx, core.TurnTCPProbeConfig{
		Hash:    *hash,
		Target:  *target,
		Payload: payload,
		Timeout: *timeout,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "FAILED: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("SUCCESS")
	fmt.Printf("TURN endpoint: %s\n", result.TurnAddress)
	fmt.Printf("Relayed address: %s\n", result.RelayAddress)
	fmt.Printf("Target: %s\n", result.Target)

	if result.ResponsePreview != "" {
		fmt.Println("Response preview:")
		fmt.Print(result.ResponsePreview)
		if !strings.HasSuffix(result.ResponsePreview, "\n") {
			fmt.Println()
		}
	}
}

func probePayload(mode, target string) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "connect":
		return nil, nil
	case "http":
		host, port, err := net.SplitHostPort(strings.TrimSpace(target))
		if err != nil {
			return nil, fmt.Errorf("parse target: %w", err)
		}
		if port != "80" {
			return nil, fmt.Errorf("http mode requires target port 80, got %s", port)
		}
		host = strings.Trim(host, "[]")
		if host == "" {
			return nil, fmt.Errorf("target host is empty")
		}
		return []byte(fmt.Sprintf(
			"GET / HTTP/1.1\r\nHost: %s\r\nConnection: close\r\nUser-Agent: PWDTT-TURN-TCP-Probe/1\r\n\r\n",
			host,
		)), nil
	default:
		return nil, fmt.Errorf("unsupported -mode %q; use connect or http", mode)
	}
}
