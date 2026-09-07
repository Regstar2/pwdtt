# Telegram VoIP reflector через VK TURN UDP

## Что проверяется

Этот probe проверяет UDP relay для Telegram-звонков, а не обычный MTProto messaging.

Актуальный Telegram Desktop объявляет для звонков `udp_p2p` и `udp_reflector`. Для обычного `phoneConnection` сервер возвращает `ip`, `port` и 16-байтовый `peer_tag`.

Текущий tgcalls фактически использует только первые 12 байт `peer_tag`: последние 4 байта заменяются случайным локальным tag перед отправкой reflector hello. Поэтому probe принимает либо 12-байтовый prefix, либо полный 16-байтовый peer tag.

## Автоматическое получение target и peer-tag-prefix на Windows

Не подставляйте примеры вида `<TELEGRAM_REFLECTOR_IP:PORT>` буквально.

Встроенный Windows Packet Monitor (`pktmon`) может записать короткий активный Telegram-звонок. Запускайте PowerShell от администратора.

Из корня репозитория:

```powershell
pktmon stop 2>$null
pktmon filter remove 2>$null
pktmon start --capture --pkt-size 0 --file-name telegram-call.etl
```

После запуска capture начните обычный 1:1 звонок в Telegram Desktop, дождитесь соединения и подержите звонок несколько секунд. Затем:

```powershell
pktmon stop
pktmon etl2pcap telegram-call.etl --out telegram-call.pcapng

go run ./cmd/telegram-reflector-discover -pcap ".\telegram-call.pcapng"
```

Discovery ищет точную 40-байтовую сигнатуру reflector hello из текущего tgcalls и выводит:

```text
Target: 149.x.x.x:port
Peer tag prefix: 24_hex_characters
Probe arguments:
  -target "..." -peer-tag-prefix "..."
```

Если найдено несколько кандидатов, сначала проверяйте каждый UDP target по очереди.

## Запуск probe

```powershell
go run ./cmd/turn-telegram-reflector-probe `
  -hash "<VK_HASH>" `
  -target "<REAL_TARGET_FROM_DISCOVERY>" `
  -peer-tag-prefix "<24_HEX_PREFIX_FROM_DISCOVERY>"
```

Старый параметр `-peer-tag` с полным 32-символьным hex также поддерживается.

## Критерий успеха

`SUCCESS` выводится только если:

1. VK/OK TURN UDP allocation создан;
2. tgcalls-compatible reflector hello отправлен к указанному Telegram endpoint;
3. ответ получен именно от этого `ip:port`;
4. первые 12 байт peer tag в ответе совпадают с обнаруженным prefix.

Успех подтвердит, что Telegram VoIP UDP reflector доступен через VK TURN без `wdtt-server`/VPS.

## Ограничения

- endpoint и peer-tag-prefix динамические и относятся к конкретному активному звонку;
- capture содержит сетевые метаданные звонка, поэтому не публикуйте `.etl`/`.pcapng`;
- probe не инициирует Telegram-звонок и не получает `phone.getCall` самостоятельно;
- probe не передаёт голос/видео;
- обычный MTProto messaging по-прежнему остаётся stream-транспортом.
