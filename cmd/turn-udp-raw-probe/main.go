package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	core "wg-turn-client"
)

func main() {
	hashInput := flag.String("hash", "", "VK call hash or full https://vk.com/call/join/... link")
	target := flag.String("target", "", "UDP target in host:port form")
	payloadHex := flag.String("payload-hex", "", "one UDP payload as hex")
	payloadText := flag.String("payload-text", "", "one UDP payload as UTF-8 text")
	timeout := flag.Duration("timeout", 45*time.Second, "overall probe timeout")
	readTimeout := flag.Duration("read-timeout", 5*time.Second, "response wait per TURN endpoint")
	flag.Parse()

	hash := normalizeVKHash(*hashInput)
	if hash == "" {
		fmt.Fprintln(os.Stderr, "error: -hash is required")
		os.Exit(2)
	}
	if strings.TrimSpace(*target) == "" {
		fmt.Fprintln(os.Stderr, "error: -target is required")
		os.Exit(2)
	}

	payload, err := parsePayload(*payloadHex, *payloadText)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(2)
	}

	fmt.Println("TURN raw UDP direct-egress probe")
	fmt.Printf("Target: %s\n", *target)
	fmt.Printf("Payload bytes: %d\n", len(payload))
	fmt.Println("One datagram will be sent per VK/OK TURN endpoint.")
	fmt.Println()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	result, err := core.ProbeTurnUDPRaw(ctx, core.TurnUDPRawProbeConfig{
		Hash:        hash,
		Target:      *target,
		Payload:     payload,
		Timeout:     *timeout,
		ReadTimeout: *readTimeout,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "FAILED: %v\n", err)
		os.Exit(1)
	}

	for i, attempt := range result.Attempts {
		fmt.Printf("Attempt %d\n", i+1)
		fmt.Printf("  TURN endpoint: %s\n", attempt.TurnAddress)
		fmt.Printf("  Target: %s\n", attempt.Target)
		if attempt.Allocated {
			fmt.Printf("  Allocate: PASS (%s)\n", attempt.RelayAddress)
		} else {
			fmt.Println("  Allocate: FAIL")
		}
		if attempt.SentBytes > 0 {
			fmt.Printf("  Send: PASS (%d bytes)\n", attempt.SentBytes)
		} else {
			fmt.Println("  Send: NOT CONFIRMED")
		}
		switch {
		case attempt.ResponseBytes > 0:
			fmt.Printf("  Receive: PASS (%d bytes from %s)\n", attempt.ResponseBytes, attempt.ResponseSource)
			fmt.Printf("  Response hex preview: %s\n", attempt.ResponseHexPreview)
		case attempt.ReceiveTimedOut:
			fmt.Printf("  Receive: TIMEOUT after %s\n", readTimeout.String())
		default:
			fmt.Println("  Receive: NOT CONFIRMED")
		}
		if attempt.Error != "" {
			fmt.Printf("  Error: %s\n", attempt.Error)
		}
		fmt.Println()
	}

	fmt.Println("VERDICT")
	switch {
	case result.AnyResponse:
		fmt.Println("UDP allocation: PASS")
		fmt.Println("Outbound UDP send: PASS")
		fmt.Println("Bidirectional UDP response: PASS")
		fmt.Println("Direct UDP egress to this target through VK/OK TURN: CONFIRMED")
	case result.AnySent:
		fmt.Println("UDP allocation: PASS")
		fmt.Println("Outbound UDP send: PASS")
		fmt.Println("Bidirectional UDP response: NOT CONFIRMED")
		fmt.Println("The target did not reply through the relay; this is not equivalent to an allocation/send failure.")
		os.Exit(3)
	default:
		fmt.Println("UDP allocation/send path: NOT CONFIRMED")
		os.Exit(1)
	}
}

func parsePayload(hexValue, textValue string) ([]byte, error) {
	hexValue = strings.TrimSpace(hexValue)
	if hexValue != "" && textValue != "" {
		return nil, fmt.Errorf("use only one of -payload-hex or -payload-text")
	}
	if hexValue == "" && textValue == "" {
		return nil, fmt.Errorf("one of -payload-hex or -payload-text is required")
	}
	if hexValue != "" {
		payload, err := hex.DecodeString(hexValue)
		if err != nil {
			return nil, fmt.Errorf("decode -payload-hex: %w", err)
		}
		if len(payload) == 0 {
			return nil, fmt.Errorf("payload must not be empty")
		}
		return payload, nil
	}
	return []byte(textValue), nil
}

func normalizeVKHash(input string) string {
	value := strings.TrimSpace(input)
	if value == "" {
		return ""
	}
	const marker = "/call/join/"
	if index := strings.Index(value, marker); index >= 0 {
		value = value[index+len(marker):]
	}
	if index := strings.IndexAny(value, "?#/"); index >= 0 {
		value = value[:index]
	}
	value = strings.TrimSpace(value)
	if strings.ContainsAny(value, "<>") {
		return ""
	}
	return value
}
