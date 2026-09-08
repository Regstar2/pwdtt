# Raw UDP direct-egress probe

## Purpose

This diagnostic answers one narrow question without assuming DNS, STUN, Telegram VoIP, or MTProto framing:

> Can the current VK/OK TURN allocation send one caller-supplied UDP datagram to a chosen external peer, and does any UDP response return through the same relay?

It does not start WireGuard, SOCKS, the GUI, or `wdtt-server`.

## Run

PowerShell:

~~~powershell
Set-Location "C:\base\projects\PWDTT"

$vk = Read-Host "VK call hash or full join link"

go run ./cmd/turn-udp-raw-probe `
  -hash $vk `
  -target "host.example:12345" `
  -payload-text "ping"
~~~

Hex payload:

~~~powershell
go run ./cmd/turn-udp-raw-probe `
  -hash $vk `
  -target "203.0.113.10:9999" `
  -payload-hex "01020304"
~~~

The probe sends exactly one datagram per VK/OK TURN endpoint. Payload size is capped at 1200 bytes.

## Stage reporting

Every TURN endpoint is reported independently:

~~~text
Attempt 1
  TURN endpoint: ...
  Target: ...
  Allocate: PASS (...)
  Send: PASS (4 bytes)
  Receive: TIMEOUT after 5s
~~~

This distinction is intentional:

- `Allocate: FAIL` means a usable UDP relay was not created;
- `Send: PASS` means Pion accepted the datagram for the external peer through the relay;
- `Receive: PASS` proves a datagram returned through TURN;
- `Receive: TIMEOUT` does not turn an allocation/send success into an allocation failure.

Exit codes:

- `0`: at least one response datagram was received;
- `3`: allocation/send succeeded but no response was received;
- `1`: allocation/send path was not confirmed;
- `2`: invalid CLI arguments.

## Why this exists

The existing probes answer protocol-specific questions:

- `turn-udp-probe`: DNS A request/response validation;
- `turn-udp-stun-probe`: STUN Binding validation;
- `turn-telegram-mtproto-udp-probe`: experimental MTProto UDP framing.

The raw probe is the protocol-neutral control. It is useful when testing an external UDP echo service, a service you control temporarily, or any protocol where you already know the exact request datagram.

A positive response only confirms the tested peer/port and current TURN endpoint policy. It does not imply unrestricted Internet UDP access.
