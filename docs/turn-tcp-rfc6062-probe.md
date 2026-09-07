# Эксперимент: прямой TURN egress без VPS

## Цель

Проверить, может ли используемая PWDTT инфраструктура VK/OK TURN самостоятельно передавать трафик к внешним peer без `wdtt-server` и VPS.

Эксперимент не меняет обычный режим PWDTT. Все проверки запускаются отдельными CLI-командами.

## TCP / RFC 6062

Команда:

```powershell
go run ./cmd/turn-tcp-probe -hash "<VK_HASH>" -target "example.com:80" -mode http
```

Фактический результат проверки 2026-09-07: оба полученных TURN endpoint отклонили `AllocateTCP()`:

```text
442: TCP Transport is not allowed by the TURN Server configuration
```

Следовательно, протестированная конфигурация VK/OK TURN не предоставляет TCP relay по RFC 6062. До внешнего TCP peer выполнение не дошло.

## UDP direct egress

Следующая гипотеза проверяет обычный UDP allocation, который уже используется WDTT, но peer теперь является не `wdtt-server`, а публичным DNS-сервером.

Схема:

```text
PWDTT probe
  ↓
VK TURN Allocate()
  ↓
1.1.1.1:53
  ↓
DNS response
```

Запуск из корня репозитория:

```powershell
go run ./cmd/turn-udp-probe -hash "<VK_HASH>" -target "1.1.1.1:53" -name "example.com"
```

Успешный результат:

```text
TURN UDP direct-egress probe
...
SUCCESS
TURN endpoint: ...
Relayed address: ...
Target: 1.1.1.1:53
Response source: ...
DNS answers: ...
Response bytes: ...
```

Успех означает, что TURN relay передал DNS query непосредственно внешнему UDP peer и вернул валидный DNS response с тем же transaction ID. Это подтверждает arbitrary UDP egress для проверенного endpoint без VPS.

## Интерпретация ошибок UDP

- ошибка `Allocate UDP` — обычный UDP allocation не был создан;
- ошибка `send DNS query` / permission-related error — внешний peer запрещён или недоступен;
- timeout на `read DNS response` — запрос ушёл, но валидный ответ через relay не получен;
- ошибка validation — через relay пришёл пакет, но он не является ожидаемым валидным DNS response;
- `SUCCESS` — подтверждена двусторонняя UDP-передача к публичному peer.

CLI перебирают TURN endpoints, полученные для указанного VK hash, и не выводят TURN username/password.

## Ограничения

Это диагностический Prototype, а не production proxy:

- не поднимает SOCKS5;
- не интегрирован в GUI;
- не маршрутизирует системный трафик;
- UDP probe проверяет только один безопасный DNS-сценарий;
- результат одного endpoint не гарантирует одинаковую политику всех TURN-серверов;
- работа зависит от внешней инфраструктуры VK/OK и может измениться независимо от PWDTT.
