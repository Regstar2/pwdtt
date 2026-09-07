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
	target := flag.String("target", "", "Telegram reflector in ip:port form")
	peerTagPrefix := flag.String("peer-tag-prefix", "", "first 12 bytes of Telegram peer_tag as 24 hex characters")
	peerTag := flag.String("peer-tag", "", "full Telegram peer_tag as 32 hex characters (legacy alternative)")
	timeout := flag.Duration("timeout", 45*time.Second, "overall probe timeout")
	flag.Parse()

	if strings.TrimSpace(*hash) == "" {
		fmt.Fprintln(os.Stderr, "error: -hash is required")
		flag.Usage()
		os.Exit(2)
	}

	tag := strings.TrimSpace(*peerTagPrefix)
	if tag == "" {
		tag = strings.TrimSpace(*peerTag)
	}

	if strings.TrimSpace(*target) == "" {
		fmt.Fprintln(os.Stderr, "error: -target is required; do not pass the <TELEGRAM_REFLECTOR_IP:PORT> example literally")
		os.Exit(2)
	}
	if strings.ContainsAny(*target, "<>") {
		fmt.Fprintln(os.Stderr, "error: -target still contains an example placeholder; discover a real reflector from an active Telegram call first")
		os.Exit(2)
	}
	if tag == "" {
		fmt.Fprintln(os.Stderr, "error: -peer-tag-prefix is required; use 24 hex characters discovered from an active call")
		os.Exit(2)
	}
	if strings.ContainsAny(tag, "<>") {
		fmt.Fprintln(os.Stderr, "error: peer tag still contains an example placeholder")
		os.Exit(2)
	}

	fmt.Println("Telegram reflector via VK TURN UDP probe")
	fmt.Printf("Telegram reflector: %s\n", *target)
	fmt.Println("Peer tag prefix: supplied (hidden)")
	fmt.Println("Obtaining VK TURN credentials, allocating UDP relay and sending Telegram reflector hello...")

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	result, err := core.ProbeTelegramReflector(ctx, core.TelegramReflectorProbeConfig{
		Hash:       *hash,
		Target:     *target,
		PeerTagHex: tag,
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
