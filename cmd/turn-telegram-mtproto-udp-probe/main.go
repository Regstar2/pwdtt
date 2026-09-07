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
	target := flag.String("target", "149.154.167.51:443", "Telegram DC UDP target in ip:port form")
	timeout := flag.Duration("timeout", 45*time.Second, "overall probe timeout")
	flag.Parse()

	if strings.TrimSpace(*hash) == "" {
		fmt.Fprintln(os.Stderr, "error: -hash is required")
		flag.Usage()
		os.Exit(2)
	}
	if strings.Contains(*target, "<") || strings.Contains(*target, ">") {
		fmt.Fprintln(os.Stderr, "error: replace the -target placeholder with a real Telegram DC ip:port")
		os.Exit(2)
	}

	fmt.Println("Telegram MTProto via VK TURN UDP probe")
	fmt.Printf("Telegram DC: %s\n", *target)
	fmt.Println("Request: unauthenticated req_pq_multi")
	fmt.Println("Framing candidates: raw, abridged, abridged-init, intermediate, intermediate-init, padded-intermediate-init, full")
	fmt.Println("Obtaining VK TURN credentials and probing Telegram directly over UDP...")

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	result, err := core.ProbeTelegramMTProtoUDP(ctx, core.TelegramMTProtoUDPProbeConfig{
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
	fmt.Printf("Telegram DC: %s\n", result.Target)
	fmt.Printf("Response source: %s\n", result.ResponseSource)
	fmt.Printf("MTProto UDP framing: %s\n", result.Mode)
	fmt.Printf("Response bytes: %d\n", result.ResponseBytes)
	fmt.Println("Validated: resPQ with matching nonce")
}
