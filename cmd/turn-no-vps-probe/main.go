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

const defaultTelegramTargets = "149.154.175.50:443,149.154.167.51:443,95.161.76.100:443,149.154.175.100:443,149.154.167.91:443,149.154.171.5:443"

func main() {
	hashInput := flag.String("hash", "", "VK call hash or full https://vk.com/call/join/... link")
	stunTarget := flag.String("stun-target", "stun.cloudflare.com:3478", "public STUN control target")
	telegramTargets := flag.String("telegram-targets", defaultTelegramTargets, "comma-separated Telegram DC UDP targets")
	controlTimeout := flag.Duration("control-timeout", 45*time.Second, "UDP control probe timeout")
	telegramTimeout := flag.Duration("telegram-timeout", 30*time.Second, "timeout per Telegram DC")
	flag.Parse()

	hash := normalizeVKHash(*hashInput)
	if hash == "" {
		fmt.Fprintln(os.Stderr, "error: -hash is required")
		flag.Usage()
		os.Exit(2)
	}

	targets, err := parseTargets(*telegramTargets)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(2)
	}

	fmt.Println("PWDTT no-VPS direct Telegram verdict probe")
	fmt.Println("Stage 1/2: verify bidirectional external UDP through VK/OK TURN")

	controlCtx, cancelControl := context.WithTimeout(context.Background(), *controlTimeout)
	control, err := core.ProbeTurnUDPSTUN(controlCtx, core.TurnUDPSTUNProbeConfig{
		Hash:    hash,
		Target:  *stunTarget,
		Timeout: *controlTimeout,
	})
	cancelControl()
	if err != nil {
		fmt.Fprintf(os.Stderr, "UDP CONTROL FAILED: %v\n", err)
		fmt.Println()
		fmt.Println("VERDICT")
		fmt.Println("UDP egress: NOT CONFIRMED")
		fmt.Println("Ordinary Telegram MTProto over TURN UDP: NOT TESTED")
		fmt.Println("Direct no-VPS Telegram messaging: NOT CONFIRMED")
		os.Exit(1)
	}

	fmt.Println("UDP CONTROL PASS")
	fmt.Printf("  TURN endpoint: %s\n", control.TurnAddress)
	fmt.Printf("  Relay: %s\n", control.RelayAddress)
	fmt.Printf("  STUN target: %s\n", control.Target)
	fmt.Printf("  Response source: %s\n", control.ResponseSource)
	fmt.Printf("  Mapped address: %s\n", control.MappedAddress)
	fmt.Println()
	fmt.Println("Stage 2/2: probe ordinary unauthenticated MTProto over UDP")
	fmt.Printf("Testing %d Telegram DC target(s) with all implemented framing candidates.\n", len(targets))

	for i, target := range targets {
		fmt.Printf("[%d/%d] %s ...\n", i+1, len(targets), target)

		probeCtx, cancelProbe := context.WithTimeout(context.Background(), *telegramTimeout)
		result, probeErr := core.ProbeTelegramMTProtoUDP(probeCtx, core.TelegramMTProtoUDPProbeConfig{
			Hash:    hash,
			Target:  target,
			Timeout: *telegramTimeout,
		})
		cancelProbe()

		if probeErr != nil {
			fmt.Printf("  NO VALID resPQ: %v\n", probeErr)
			continue
		}

		fmt.Println("  MTProto UDP PASS")
		fmt.Printf("  TURN endpoint: %s\n", result.TurnAddress)
		fmt.Printf("  Relay: %s\n", result.RelayAddress)
		fmt.Printf("  Response source: %s\n", result.ResponseSource)
		fmt.Printf("  Framing: %s\n", result.Mode)
		fmt.Printf("  Response bytes: %d\n", result.ResponseBytes)
		fmt.Println()
		fmt.Println("VERDICT")
		fmt.Println("UDP egress: PASS")
		fmt.Println("Ordinary Telegram MTProto over TURN UDP: PASS")
		fmt.Println("Direct no-VPS Telegram messaging transport: CONFIRMED AT PROTOCOL-PROBE LEVEL")
		fmt.Println("Next step: implement a persistent client transport around the confirmed framing.")
		return
	}

	fmt.Println()
	fmt.Println("VERDICT")
	fmt.Println("UDP egress: PASS")
	fmt.Println("Ordinary Telegram MTProto over tested UDP framings/DCs: NO VALID RESPONSE")
	fmt.Println("Direct no-VPS Telegram messaging: NOT CONFIRMED")
	fmt.Println("The result does not prove impossibility; it shows that the tested direct MTProto-over-UDP variants did not work.")
	os.Exit(3)
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

func parseTargets(input string) ([]string, error) {
	parts := strings.Split(input, ",")
	result := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))

	for _, part := range parts {
		target := strings.TrimSpace(part)
		if target == "" {
			continue
		}
		host, port, err := net.SplitHostPort(target)
		if err != nil {
			return nil, fmt.Errorf("invalid Telegram target %q: %w", target, err)
		}
		if strings.Trim(host, "[]") == "" || port == "" {
			return nil, fmt.Errorf("invalid Telegram target %q", target)
		}
		if _, ok := seen[target]; ok {
			continue
		}
		seen[target] = struct{}{}
		result = append(result, target)
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("at least one Telegram target is required")
	}
	return result, nil
}
