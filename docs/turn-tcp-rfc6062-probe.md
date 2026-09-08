# Эксперимент: прямой TURN egress без VPS

## Цель

Проверить исходную гипотезу PWDTT: можно ли исключить wdtt-server/VPS и передавать трафик через инфраструктуру VK/OK TURN непосредственно внешним peer.

Эксперимент не меняет обычный runtime PWDTT. Все проверки запускаются отдельными CLI-командами.

## Что уже подтверждено вручную

### TCP / RFC 6062

Оба полученных 2026-09-07 TURN endpoint приняли control/auth path, но отклонили TCP allocation:

~~~text
442: TCP Transport is not allowed by the TURN Server configuration
~~~

Следовательно, стандартный RFC 6062 TURN TCP relay на протестированной конфигурации выключен.

### UDP direct egress

DNS-проверка к 1.1.1.1:53 создавала UDP allocation и отправляла пакет, но ответ не возвращался.

Контрольная STUN-проверка затем успешно прошла через внешний peer:

~~~text
TURN endpoint: 91.231.135.154:19302
Relayed address: 91.231.135.154:42880
Target: 162.159.207.0:3478
Response source: 162.159.207.0:3478
STUN mapped address: 91.231.135.154:42880
Response bytes: 32
~~~

Это подтверждает двусторонний внешний UDP egress через протестированный VK/OK TURN без VPS. Таймаут DNS/53 не является доказательством общего запрета произвольного UDP.

## Основной no-VPS verdict runner

Для исходной задачи используйте:

~~~powershell
Set-Location "C:\base\projects\PWDTT"
$vk = Read-Host "VK call hash or full join link"
go run ./cmd/turn-no-vps-probe -hash $vk
~~~

Runner выполняет два этапа:

1. STUN control: подтверждает, что внешний двусторонний UDP через TURN работает в текущем запуске.
2. Ordinary Telegram MTProto UDP: пробует production IPv4 DC из встроенного списка Telegram Desktop и все реализованные framing-кандидаты.

Список DC по умолчанию:

~~~text
149.154.175.50:443
149.154.167.51:443
95.161.76.100:443
149.154.175.100:443
149.154.167.91:443
149.154.171.5:443
~~~

Список можно заменить:

~~~powershell
go run ./cmd/turn-no-vps-probe -hash $vk -telegram-targets "149.154.167.51:443,149.154.167.91:443"
~~~

Runner принимает как raw hash, так и полный https://vk.com/call/join/... URL и не печатает TURN credentials.

## Интерпретация verdict

### UDP control failed

~~~text
UDP egress: NOT CONFIRMED
Ordinary Telegram MTProto over TURN UDP: NOT TESTED
~~~

Сначала нужно восстановить рабочий VK hash/TURN allocation или выяснить изменение политики TURN.

### UDP PASS, Telegram no response

~~~text
UDP egress: PASS
Ordinary Telegram MTProto over tested UDP framings/DCs: NO VALID RESPONSE
Direct no-VPS Telegram messaging: NOT CONFIRMED
~~~

Это означает, что ограничение уже не в общем UDP egress. Оно находится на уровне Telegram transport/framing/peer policy. Это не доказывает математическую невозможность UDP transport, но протестированные варианты не дают обычный MTProto exchange.

### Telegram PASS

Успех принимается только после получения валидного plaintext resPQ#05162463 с nonce, совпадающим с отправленным req_pq_multi.

~~~text
UDP egress: PASS
Ordinary Telegram MTProto over TURN UDP: PASS
Direct no-VPS Telegram messaging transport: CONFIRMED AT PROTOCOL-PROBE LEVEL
~~~

После этого имеет смысл писать persistent transport adapter. До такого результата интегрировать эксперимент в GUI/WireGuard runtime не следует.

## Отдельные probes

DNS:

~~~powershell
go run ./cmd/turn-udp-probe -hash $vk -target "1.1.1.1:53" -name "example.com"
~~~

STUN:

~~~powershell
go run ./cmd/turn-udp-stun-probe -hash $vk
~~~

Один Telegram DC:

~~~powershell
go run ./cmd/turn-telegram-mtproto-udp-probe -hash $vk -target "149.154.167.51:443"
~~~

## Ограничения

Это diagnostic prototype:

- не поднимает SOCKS5;
- не интегрирован в GUI;
- не маршрутизирует системный трафик;
- RFC 6062 TCP relay на протестированных TURN endpoint выключен;
- обычный Telegram MTProto-over-UDP остаётся экспериментальной гипотезой до положительного resPQ;
- политика внешней инфраструктуры VK/OK может измениться независимо от PWDTT.
