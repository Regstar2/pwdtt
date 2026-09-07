package core

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"strconv"
)

const maxTelegramReflectorCaptureSize = 256 << 20

type TelegramReflectorCaptureCandidate struct {
	Source           string
	Target           string
	PeerTagPrefixHex string
}

func DiscoverTelegramReflectorFromPCAPNG(r io.Reader) ([]TelegramReflectorCaptureCandidate, error) {
	if r == nil {
		return nil, fmt.Errorf("pcapng reader is required")
	}

	data, err := io.ReadAll(io.LimitReader(r, maxTelegramReflectorCaptureSize+1))
	if err != nil {
		return nil, fmt.Errorf("read pcapng: %w", err)
	}
	if len(data) > maxTelegramReflectorCaptureSize {
		return nil, fmt.Errorf("pcapng is too large; limit is %d MiB", maxTelegramReflectorCaptureSize>>20)
	}
	if len(data) < 12 {
		return nil, fmt.Errorf("pcapng is too short")
	}

	var order binary.ByteOrder
	var foundSection bool
	var results []TelegramReflectorCaptureCandidate
	seen := make(map[string]struct{})

	for offset := 0; offset+12 <= len(data); {
		if bytes.Equal(data[offset:offset+4], []byte{0x0a, 0x0d, 0x0d, 0x0a}) {
			if offset+12 > len(data) {
				break
			}
			switch {
			case bytes.Equal(data[offset+8:offset+12], []byte{0x4d, 0x3c, 0x2b, 0x1a}):
				order = binary.LittleEndian
			case bytes.Equal(data[offset+8:offset+12], []byte{0x1a, 0x2b, 0x3c, 0x4d}):
				order = binary.BigEndian
			default:
				return nil, fmt.Errorf("unsupported pcapng byte-order magic at offset %d", offset)
			}
			foundSection = true
		}

		if !foundSection || order == nil {
			return nil, fmt.Errorf("pcapng section header not found")
		}

		blockLen := int(order.Uint32(data[offset+4 : offset+8]))
		if blockLen < 12 || offset+blockLen > len(data) {
			return nil, fmt.Errorf("invalid pcapng block length %d at offset %d", blockLen, offset)
		}
		if int(order.Uint32(data[offset+blockLen-4:offset+blockLen])) != blockLen {
			return nil, fmt.Errorf("pcapng block length trailer mismatch at offset %d", offset)
		}

		blockType := order.Uint32(data[offset : offset+4])
		switch blockType {
		case 0x00000006:
			if blockLen >= 32 {
				capturedLen := int(order.Uint32(data[offset+20 : offset+24]))
				packetStart := offset + 28
				packetEnd := packetStart + capturedLen
				if packetEnd <= offset+blockLen-4 {
					appendTelegramReflectorPacketCandidates(data[packetStart:packetEnd], &results, seen)
				}
			}
		case 0x00000003:
			packetStart := offset + 12
			packetEnd := offset + blockLen - 4
			if packetEnd > packetStart {
				appendTelegramReflectorPacketCandidates(data[packetStart:packetEnd], &results, seen)
			}
		}

		offset += blockLen
	}

	if !foundSection {
		return nil, fmt.Errorf("pcapng section header not found")
	}
	return results, nil
}

func appendTelegramReflectorPacketCandidates(packet []byte, results *[]TelegramReflectorCaptureCandidate, seen map[string]struct{}) {
	for offset := 0; offset < len(packet); offset++ {
		if candidate, ok := parseIPv4TelegramReflectorHello(packet[offset:]); ok {
			addTelegramReflectorCandidate(candidate, results, seen)
			return
		}
		if candidate, ok := parseIPv6TelegramReflectorHello(packet[offset:]); ok {
			addTelegramReflectorCandidate(candidate, results, seen)
			return
		}
	}
}

func addTelegramReflectorCandidate(candidate TelegramReflectorCaptureCandidate, results *[]TelegramReflectorCaptureCandidate, seen map[string]struct{}) {
	key := candidate.Target + "|" + candidate.PeerTagPrefixHex
	if _, exists := seen[key]; exists {
		return
	}
	seen[key] = struct{}{}
	*results = append(*results, candidate)
}

func parseIPv4TelegramReflectorHello(packet []byte) (TelegramReflectorCaptureCandidate, bool) {
	if len(packet) < 20 || packet[0]>>4 != 4 {
		return TelegramReflectorCaptureCandidate{}, false
	}
	ihl := int(packet[0]&0x0f) * 4
	if ihl < 20 || len(packet) < ihl+8 || packet[9] != 17 {
		return TelegramReflectorCaptureCandidate{}, false
	}

	totalLen := int(binary.BigEndian.Uint16(packet[2:4]))
	if totalLen < ihl+8 || totalLen > len(packet) {
		return TelegramReflectorCaptureCandidate{}, false
	}

	udp := packet[ihl:totalLen]
	return parseUDPTelegramReflectorHello(
		net.IP(packet[12:16]),
		net.IP(packet[16:20]),
		udp,
	)
}

func parseIPv6TelegramReflectorHello(packet []byte) (TelegramReflectorCaptureCandidate, bool) {
	if len(packet) < 48 || packet[0]>>4 != 6 || packet[6] != 17 {
		return TelegramReflectorCaptureCandidate{}, false
	}

	payloadLen := int(binary.BigEndian.Uint16(packet[4:6]))
	totalLen := 40 + payloadLen
	if totalLen < 48 || totalLen > len(packet) {
		return TelegramReflectorCaptureCandidate{}, false
	}

	return parseUDPTelegramReflectorHello(
		net.IP(packet[8:24]),
		net.IP(packet[24:40]),
		packet[40:totalLen],
	)
}

func parseUDPTelegramReflectorHello(sourceIP, targetIP net.IP, udp []byte) (TelegramReflectorCaptureCandidate, bool) {
	if len(udp) < 8 {
		return TelegramReflectorCaptureCandidate{}, false
	}
	udpLen := int(binary.BigEndian.Uint16(udp[4:6]))
	if udpLen < 8 || udpLen > len(udp) {
		return TelegramReflectorCaptureCandidate{}, false
	}

	payload := udp[8:udpLen]
	if !isTelegramReflectorHelloPayload(payload) {
		return TelegramReflectorCaptureCandidate{}, false
	}

	sourcePort := int(binary.BigEndian.Uint16(udp[0:2]))
	targetPort := int(binary.BigEndian.Uint16(udp[2:4]))
	return TelegramReflectorCaptureCandidate{
		Source:           net.JoinHostPort(sourceIP.String(), strconv.Itoa(sourcePort)),
		Target:           net.JoinHostPort(targetIP.String(), strconv.Itoa(targetPort)),
		PeerTagPrefixHex: hex.EncodeToString(payload[:12]),
	}, true
}

func isTelegramReflectorHelloPayload(payload []byte) bool {
	if len(payload) != 40 {
		return false
	}
	for i := 16; i < 28; i++ {
		if payload[i] != 0xff {
			return false
		}
	}
	if payload[28] != 0xfe || payload[29] != 0xff || payload[30] != 0xff || payload[31] != 0xff {
		return false
	}
	return binary.BigEndian.Uint64(payload[32:40]) == 123
}
