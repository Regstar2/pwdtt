# Эксперимент: TURN TCP / RFC 6062 без VPS

## Гипотеза

Проверить, разрешает ли используемая PWDTT инфраструктура VK/OK TURN:

1. TCP allocation по RFC 6062;
2. исходящий TCP `CONNECT` к произвольному peer;
3. двустороннюю передачу данных без `wdtt-server` и без VPS.

Эксперимент не меняет обычный режим PWDTT. Он запускается отдельной CLI-командой.

## Запуск

Нужен рабочий VK call hash, который PWDTT уже умеет использовать для получения TURN credentials.

Из корня репозитория:

```powershell
go run ./cmd/turn-tcp-probe -hash "<VK_HASH>" -target "example.com:80" -mode http
```

Успешный результат должен содержать:

```text
SUCCESS
TURN endpoint: ...
Relayed address: ...
Target: ...
Response preview:
HTTP/1.1 ...
```

Это подтверждает одновременно TCP allocation, исходящий TCP peer и двустороннюю передачу данных через TURN.

Для проверки только установки TCP-соединения:

```powershell
go run ./cmd/turn-tcp-probe -hash "<VK_HASH>" -target "telegram.org:443" -mode connect
```

## Интерпретация ошибок

- ошибка на `AllocateTCP` — сервер не поддерживает или политикой запрещает TCP allocation;
- ошибка на `DialTCP` / `CreatePermission` / `Connect` — allocation создан, но peer или исходящее TCP-соединение запрещены/недоступны;
- `SUCCESS` в режиме `connect` — TCP-соединение к peer через TURN установлено;
- `SUCCESS` + HTTP response в режиме `http` — подтверждена двусторонняя передача прикладных данных.

CLI перебирает TURN endpoints, полученные для указанного VK hash, и выводит ошибки без username/password.

## Ограничения

Это диагностический Prototype, а не production proxy:

- не поднимает SOCKS5;
- не интегрирован в GUI;
- не маршрутизирует системный трафик;
- не гарантирует, что политика TURN одинакова для всех endpoint/аккаунтов/сетей;
- работа зависит от внешней инфраструктуры VK/OK и может измениться независимо от PWDTT.
