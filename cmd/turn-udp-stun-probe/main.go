package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	core "wg-turn-client"
)

func main() {
	hash := flag.String("hash", "", "VK call hash (the part after /call/join/)")
	target := flag.String("target", "stun.cloudflare.com:3478", "public STUN server in host:port form")
	timeout := flag.Duration("timeout", 45*time.Second, "overall probe timeout")
	flag.Parse()

	if strings.TrimSpace(*hash) == "" {
		fmt.Fprintln(os.Stderr, "error: -hash is required")
		flag.Usage()
		os.Exit(2)
	}

	fmt.Println("TURN UDP STUN direct-egress probe")
	fmt.Printf("Target: %s\n", *target)
	fmt.Println("Obtaining VK TURN credentials, allocating UDP relay and sending STUN Binding request...")

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	result, err := core.ProbeTurnUDPSTUN(ctx, core.TurnUDPSTUNProbeConfig{
		Hash:    *hash,
		Target:  *target,
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
	fmt.Printf("Response source: %s\n", result.ResponseSource)
	fmt.Printf("STUN mapped address: %s\n", result.MappedAddress)
	fmt.Printf("Response bytes: %d\n", result.ResponseBytes)
}
