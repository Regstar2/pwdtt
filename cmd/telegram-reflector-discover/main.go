package main

import (
	"flag"
	"fmt"
	"os"

	core "wg-turn-client"
)

func main() {
	pcapPath := flag.String("pcap", "", "pcapng file produced by pktmon etl2pcap")
	flag.Parse()

	if *pcapPath == "" {
		fmt.Fprintln(os.Stderr, "error: -pcap is required")
		os.Exit(2)
	}

	file, err := os.Open(*pcapPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FAILED: open capture: %v\n", err)
		os.Exit(1)
	}
	defer file.Close()

	candidates, err := core.DiscoverTelegramReflectorFromPCAPNG(file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FAILED: %v\n", err)
		os.Exit(1)
	}
	if len(candidates) == 0 {
		fmt.Println("No Telegram reflector hello packets found.")
		fmt.Println("Capture an active 1:1 Telegram call with full packet payloads, then retry.")
		os.Exit(1)
	}

	fmt.Printf("Found %d Telegram reflector candidate(s):\n", len(candidates))
	for i, candidate := range candidates {
		fmt.Printf("\nCandidate %d\n", i+1)
		fmt.Printf("Source: %s\n", candidate.Source)
		fmt.Printf("Target: %s\n", candidate.Target)
		fmt.Printf("Peer tag prefix: %s\n", candidate.PeerTagPrefixHex)
		fmt.Println("Probe arguments:")
		fmt.Printf("  -target %q -peer-tag-prefix %q\n", candidate.Target, candidate.PeerTagPrefixHex)
	}
}
