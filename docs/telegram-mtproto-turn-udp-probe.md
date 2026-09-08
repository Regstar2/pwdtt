# Ordinary Telegram MTProto over VK TURN UDP

## Goal

Test the original no-VPS goal directly:

~~~text
client-side protocol probe
  -> VK/OK TURN UDP relay
  -> Telegram MTProto DC
  -> MTProto response
  -> VK/OK TURN
  -> probe
~~~

This is for ordinary Telegram MTProto, not Telegram VoIP.

## Probe

cmd/turn-telegram-mtproto-udp-probe sends an unauthenticated req_pq_multi directly to one Telegram DC through the VK/OK TURN UDP allocation.

Implemented framing candidates:

- raw MTProto plaintext payload;
- abridged;
- abridged with the TCP-style 0xef initializer;
- intermediate;
- intermediate with 0xeeeeeeee initializer;
- padded intermediate with 0xdddddddd initializer;
- full transport envelope with sequence number and CRC32.

No Telegram account, auth key or API ID is required for req_pq_multi.

## Single-DC run

~~~powershell
go run ./cmd/turn-telegram-mtproto-udp-probe -hash "<VK_HASH>" -target "149.154.167.51:443"
~~~

## Recommended no-VPS run

Use the aggregate runner instead of manually testing one DC:

~~~powershell
$vk = Read-Host "VK call hash or full join link"
go run ./cmd/turn-no-vps-probe -hash $vk
~~~

It first proves current bidirectional UDP egress with a public STUN Binding exchange and then checks all current built-in production IPv4 Telegram Desktop DC targets.

## Acceptance criterion

SUCCESS is printed only when the returned UDP datagram contains a valid plaintext MTProto resPQ#05162463 whose nonce exactly matches the random nonce from the corresponding req_pq_multi.

A timeout is not proof that Telegram has no UDP support. A positive matching resPQ, however, is definitive protocol-level evidence that ordinary MTProto can reach that Telegram DC through VK TURN without a VPS.

## Current interpretation

Manual STUN testing has already confirmed bidirectional direct external UDP egress through the tested VK/OK TURN endpoint.

Therefore, if the aggregate runner reports STUN PASS but no valid Telegram response across all tested DCs/framing candidates, the remaining obstacle is Telegram transport compatibility or peer policy rather than general TURN UDP egress.
