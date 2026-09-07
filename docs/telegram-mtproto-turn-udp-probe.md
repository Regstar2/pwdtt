# Ordinary Telegram MTProto over VK TURN UDP

## Goal

Test the original no-VPS goal directly:

```text
Telegram client-side probe
  -> VK/OK TURN UDP relay
  -> Telegram MTProto DC
  -> MTProto response
  -> VK/OK TURN
  -> probe
```

This is not a Telegram VoIP test.

## Why this experiment exists

Telegram's current general MTProto documentation still lists UDP as an underlying transport and notes that, with UDP, a response may come from a different IP address. The dedicated transport page currently documents TCP, WebSocket/WSS and HTTP/HTTPS, but not the UDP wire format.

Because there is no current public UDP framing specification, the probe sends a real unauthenticated `req_pq_multi` using several plausible framings:

- raw MTProto plaintext payload;
- abridged, with and without the TCP-style `0xef` initializer;
- intermediate, with and without the `0xeeeeeeee` initializer;
- padded intermediate with `0xdddddddd` initializer;
- full MTProto transport envelope with sequence number and CRC32.

No Telegram account or auth key is required for `req_pq_multi`.

## Default Telegram target

The probe defaults to DC2 from Telegram Desktop's built-in DC list:

```text
149.154.167.51:443
```

An alternate DC can be supplied with `-target`.

## Run

```powershell
go run ./cmd/turn-telegram-mtproto-udp-probe `
  -hash "<VK_HASH>" `
  -target "149.154.167.51:443"
```

## Acceptance criterion

The probe prints `SUCCESS` only when a returned UDP datagram contains a valid plaintext MTProto `resPQ#05162463` whose nonce exactly matches the random nonce from the corresponding `req_pq_multi`.

A timeout is not by itself proof that Telegram has no UDP support: Telegram's general documentation notes that UDP responses may originate from a different server IP. TURN permissions are peer-IP scoped, so such a response can be filtered unless the alternate source IP also has a permission. A positive `resPQ`, however, is definitive proof that ordinary MTProto reaches a Telegram DC through VK TURN without a VPS.

## What success would unlock

If this probe succeeds, the next step is to implement a reliable local stream/datagram adapter around the working Telegram UDP transport and then connect Telegram-facing traffic to it.

If every DC and framing candidate times out, direct ordinary MTProto-over-UDP becomes unlikely and the next no-VPS path to test is Telegram HTTPS/WSS over a UDP-native QUIC carrier.
