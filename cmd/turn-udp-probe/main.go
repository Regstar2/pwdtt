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
	target := flag.String("target", "1.1.1.1:53", "UDP DNS server in host:port form")
	name := flag.String("name", "example.com", "DNS A query name")
	timeout := flag.Duration("timeout", 45*time.Second, "overall probe timeout")
	flag.Parse()

	if strings.TrimSpace(*hash) == "" {
		fmt.Fprintln(os.Stderr, "error: -hash is required")
		flag.Usage()
		os.Exit(2)
	}

	fmt.Println("TURN UDP direct-egress probe")
	fmt.Printf("Target: %s\n", *target)
	fmt.Printf("DNS query: %s A\n", *name)
	fmt.Println("Obtaining VK TURN credentials, allocating UDP relay and sending DNS query...")

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	result, err := core.ProbeTurnUDP(ctx, core.TurnUDPProbeConfig{
		Hash:      *hash,
		Target:    *target,
		QueryName: *name,
		Timeout:   *timeout,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "FAILED: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("SUCCESS")
	fmt.Printf("TURN endpoint: %s\n", result.TurnAddress)
	fmt.Printf("Relayed address: %s\n", result.RelayAddress)
	fmt.Printf("Target: %s\n", result.Target)
	fmt.Printf("Response source: %s\n", result.Source)
	fmt.Printf("DNS answers: %d\n", result.AnswerCount)
	fmt.Printf("Response bytes: %d\n", result.ResponseBytes)
}
