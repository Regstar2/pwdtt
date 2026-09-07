package core

import (
	"bytes"
	"encoding/binary"
	"net"
	"testing"
)

func TestDiscoverTelegramReflectorFromPCAPNG(t *testing.T) {
	const prefixHex = "00112233445566778899aabb"
	payload := []byte{
		0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb,
		0x01, 0x02, 0x03, 0x04,
	}
	for i := 0; i < 12; i++ {
		payload = append(payload, 0xff)
	}
	payload = append(payload, 0xfe, 0xff, 0xff, 0xff)
	var ping [8]byte
	binary.BigEndian.PutUint64(ping[:], 123)
	payload = append(payload, ping[:]...)

	packet := makeIPv4UDPPacket(
		net.IPv4(10, 0, 0, 2),
		net.IPv4(149, 154, 167, 50),
		40000,
		599,
		payload,
	)
	pcap := makePCAPNG(packet)

	candidates, err := DiscoverTelegramReflectorFromPCAPNG(bytes.NewReader(pcap))
	if err != nil {
		t.Fatalf("DiscoverTelegramReflectorFromPCAPNG failed: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidates=%d, want 1", len(candidates))
	}
	if candidates[0].Target != "149.154.167.50:599" {
		t.Fatalf("target=%q", candidates[0].Target)
	}
	if candidates[0].PeerTagPrefixHex != prefixHex {
		t.Fatalf("prefix=%q, want %q", candidates[0].PeerTagPrefixHex, prefixHex)
	}
}

func TestDiscoverTelegramReflectorFromPCAPNGNoMatch(t *testing.T) {
	packet := makeIPv4UDPPacket(
		net.IPv4(10, 0, 0, 2),
		net.IPv4(1, 1, 1, 1),
		40000,
		53,
		[]byte("not a reflector packet"),
	)
	candidates, err := DiscoverTelegramReflectorFromPCAPNG(bytes.NewReader(makePCAPNG(packet)))
	if err != nil {
		t.Fatalf("DiscoverTelegramReflectorFromPCAPNG failed: %v", err)
	}
	if len(candidates) != 0 {
		t.Fatalf("candidates=%d, want 0", len(candidates))
	}
}

func makeIPv4UDPPacket(sourceIP, targetIP net.IP, sourcePort, targetPort int, payload []byte) []byte {
	udpLen := 8 + len(payload)
	totalLen := 20 + udpLen
	packet := make([]byte, totalLen)
	packet[0] = 0x45
	binary.BigEndian.PutUint16(packet[2:4], uint16(totalLen))
	packet[8] = 64
	packet[9] = 17
	copy(packet[12:16], sourceIP.To4())
	copy(packet[16:20], targetIP.To4())

	udp := packet[20:]
	binary.BigEndian.PutUint16(udp[0:2], uint16(sourcePort))
	binary.BigEndian.PutUint16(udp[2:4], uint16(targetPort))
	binary.BigEndian.PutUint16(udp[4:6], uint16(udpLen))
	copy(udp[8:], payload)
	return packet
}

func makePCAPNG(packet []byte) []byte {
	var out bytes.Buffer

	writeBlock := func(blockType uint32, body []byte) {
		blockLen := uint32(12 + len(body))
		padding := (4 - (blockLen % 4)) % 4
		blockLen += padding

		_ = binary.Write(&out, binary.LittleEndian, blockType)
		_ = binary.Write(&out, binary.LittleEndian, blockLen)
		out.Write(body)
		out.Write(make([]byte, padding))
		_ = binary.Write(&out, binary.LittleEndian, blockLen)
	}

	sectionBody := make([]byte, 16)
	binary.LittleEndian.PutUint32(sectionBody[0:4], 0x1a2b3c4d)
	binary.LittleEndian.PutUint16(sectionBody[4:6], 1)
	binary.LittleEndian.PutUint16(sectionBody[6:8], 0)
	for i := 8; i < 16; i++ {
		sectionBody[i] = 0xff
	}
	writeBlock(0x0a0d0d0a, sectionBody)

	idb := make([]byte, 8)
	binary.LittleEndian.PutUint16(idb[0:2], 1)
	binary.LittleEndian.PutUint32(idb[4:8], 65535)
	writeBlock(0x00000001, idb)

	epb := make([]byte, 20+len(packet))
	binary.LittleEndian.PutUint32(epb[12:16], uint32(len(packet)))
	binary.LittleEndian.PutUint32(epb[16:20], uint32(len(packet)))
	copy(epb[20:], packet)
	writeBlock(0x00000006, epb)

	return out.Bytes()
}
