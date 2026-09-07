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
	target := flag.String("target", "", "Telegram phoneConnection UDP reflector in ip:port form")
	peerTag := flag.String("peer-tag", "", "Telegram phoneConnection peer_tag as 32 hex characters")
	timeout := flag.Duration("timeout", 45*time.Second, "overall probe timeout")
	flag.Parse()

	if strings.TrimSpace(*hash) == "" {
		fmt.Fprintln(os.Stderr, "error: -hash is required")
		flag.Usage()
		os.Exit(2)
	}
	if strings.TrimSpace(*target) == "" {
		fmt.Fprintln(os.Stderr, "error: -target is required")
		flag.Usage()
		os.Exit(2)
	}
	if strings.TrimSpace(*peerTag) == "" {
		fmt.Fprintln(os.Stderr, "error: -peer-tag is required")
		flag.Usage()
		os.Exit(2)
	}

	fmt.Println("Telegram reflector via VK TURN UDP probe")
	fmt.Printf("Telegram reflector: %s\n", *target)
	fmt.Println("Peer tag: supplied (hidden)")
	fmt.Println("Obtaining VK TURN credentials, allocating UDP relay and sending Telegram reflector hello...")

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	result, err := core.ProbeTelegramReflector(ctx, core.TelegramReflectorProbeConfig{
		Hash:       *hash,
		Target:     *target,
		PeerTagHex: *peerTag,
		Timeout:    *timeout,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "FAILED: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("SUCCESS")
	fmt.Printf("TURN endpoint: %s\n", result.TurnAddress)
	fmt.Printf("Relayed address: %s\n", result.RelayAddress)
	fmt.Printf("Telegram reflector: %s\n", result.Target)
	fmt.Printf("Response source: %s\n", result.ResponseSource)
	fmt.Printf("Response bytes: %d\n", result.ResponseBytes)
}
