# Telegram VoIP reflector через VK TURN UDP

## Что проверяется

Этот probe проверяет не MTProto-сообщения, а UDP relay для Telegram-звонков.

Актуальный Telegram Desktop объявляет для звонков `udp_p2p` и `udp_reflector`. Для обычного `phoneConnection` сервер возвращает:

- `ip`;
- `port`;
- `peer_tag` длиной 16 байт.

Текущий tgcalls использует эти данные для Telegram reflector. Probe воспроизводит его UDP hello поверх уже подтверждённого VK/OK TURN UDP allocation.

Схема:

```text
PWDTT probe
  -> VK/OK TURN UDP allocation
  -> Telegram VoIP reflector ip:port
  -> Telegram reflector response
  -> VK/OK TURN relay
  -> probe
```

## Запуск

Нужны данные одного UDP `phoneConnection` из активного Telegram-звонка:

- reflector `ip:port`;
- `peer_tag` как 32 hex-символа.

`peer_tag` чувствителен в рамках конкретного звонка: не публикуйте его. CLI не выводит его в лог.

```powershell
go run ./cmd/turn-telegram-reflector-probe `
  -hash "<VK_HASH>" `
  -target "<TELEGRAM_REFLECTOR_IP:PORT>" `
  -peer-tag "<32_HEX_PEER_TAG>"
```

## Критерий успеха

`SUCCESS` выводится только если:

1. VK/OK TURN UDP allocation создан;
2. tgcalls-compatible reflector hello отправлен к указанному Telegram endpoint;
3. ответ получен именно от этого `ip:port`;
4. первые 12 байт peer tag в ответе совпадают с активным звонком.

Успех подтвердит, что Telegram VoIP UDP reflector доступен через VK TURN без `wdtt-server`/VPS.

## Ограничения

- endpoint и peer_tag динамические и относятся к активному звонку;
- probe не инициирует Telegram-звонок и не получает `phone.getCall` самостоятельно;
- probe не передаёт голос/видео;
- обычный MTProto messaging по-прежнему остаётся TCP/HTTP stream-транспортом;
- peer_tag не следует сохранять в issue, CI logs или публичные артефакты.
