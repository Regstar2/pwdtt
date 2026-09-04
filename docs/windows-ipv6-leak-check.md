# Проверка IPv6 leak protection на Windows

Этот сценарий проверяет исправление Issue #8 на Windows-системе, где физический интерфейс имеет рабочий глобальный IPv6.

## До подключения

Запусти PowerShell от имени администратора и убедись, что IPv6 действительно работает напрямую:

```powershell
curl.exe -6 https://www.cloudflare.com/cdn-cgi/trace
```

Ответ должен содержать исходный IPv6 провайдера. До подключения PWDTT правило защиты отсутствует:

```powershell
Get-NetFirewallRule -Name PWDTT-IPv6-Leak-Protection -ErrorAction SilentlyContinue
```

## Во время full-tunnel

Подключи PWDTT к зарубежному серверу и выполни:

```powershell
curl.exe -4 https://www.cloudflare.com/cdn-cgi/trace
curl.exe -6 https://www.cloudflare.com/cdn-cgi/trace
Get-NetFirewallRule -Name PWDTT-IPv6-Leak-Protection
Test-NetConnection google.com -Port 443
```

Ожидается:

- IPv4 продолжает выходить через `wg-turn` и показывает IP/страну PWDTT-сервера;
- `curl.exe -6` не получает прямой ответ через провайдера;
- временное правило `PWDTT-IPv6-Leak-Protection` существует и активно;
- правило блокирует весь исходящий IPv6 на время активного IPv4-only full-tunnel;
- исходный IPv6 провайдера не используется для пользовательского трафика.

Защита включается только когда `AllowedIPs` содержит IPv4 default route `0.0.0.0/0`. Она не привязана к именам сетевых интерфейсов: это исключает ошибки с локализованными/нестандартными alias и закрывает обход через другие адаптеры. Для split-tunnel конфигураций защита не применяется.

## После Disconnect

Отключи PWDTT и повтори:

```powershell
Get-NetFirewallRule -Name PWDTT-IPv6-Leak-Protection -ErrorAction SilentlyContinue
curl.exe -6 https://www.cloudflare.com/cdn-cgi/trace
```

Ожидается:

- правило PWDTT отсутствует;
- исходная IPv6-связность физического интерфейса восстановлена.

## Проверка закрытия приложения

1. Подключи full-tunnel PWDTT и убедись, что правило существует.
2. Закрой приложение обычным способом, не нажимая Disconnect.
3. Проверь:

```powershell
Get-NetFirewallRule -Name PWDTT-IPv6-Leak-Protection -ErrorAction SilentlyContinue
curl.exe -6 https://www.cloudflare.com/cdn-cgi/trace
```

Ожидается, что правило удалено через Wails `OnShutdown`, а исходная IPv6-связность восстановлена.

## Проверка аварийного завершения

1. Подключи full-tunnel PWDTT и убедись, что правило существует.
2. Аварийно заверши процесс PWDTT.
3. Запусти PWDTT снова.
4. До нового подключения проверь:

```powershell
Get-NetFirewallRule -Name PWDTT-IPv6-Leak-Protection -ErrorAction SilentlyContinue
curl.exe -6 https://www.cloudflare.com/cdn-cgi/trace
```

При старте приложение удаляет stale-правило предыдущего запуска. После запуска правило должно отсутствовать, а исходная IPv6-связность должна работать.

## Ограничение проверки

Автоматический CI может проверить Go-код и кросс-компиляцию Windows backend, но не может подтвердить фактическое отсутствие IPv6 leak без реального Windows-хоста с глобальным IPv6 и активным Windows Firewall. Этот пункт проверяется вручную по сценарию выше.
