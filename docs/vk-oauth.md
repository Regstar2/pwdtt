# VK OAuth для генерации хешей в Windows

Автогенерация VK-хешей повторяет рабочий flow qWDTT, адаптированный под Windows/Wails. Собственный VK ID `app_id` не требуется.

Используются те же публичные параметры, что и в qWDTT:

```text
client_id=6287487
redirect_uri=https://oauth.vk.com/blank.html
response_type=token
scope=messages
state=wdtt
v=5.199
```

## Авторизация

PWDTT не начинает вход напрямую с OAuth URL. Как и qWDTT, сначала создаётся обычная VK-сессия в отдельном WebView2-профиле.

Последовательность:

```text
https://m.vk.ru/login
        ↓
VK ID login
        ↓
remixsid в WebView2 cookies
        ↓
https://oauth.vk.com/authorize
        ↓
https://oauth.vk.com/blank.html#access_token=...
        ↓
calls.start
```

Пользователь вводит пароль только на странице VK. PWDTT не рисует собственную форму логина и не получает пароль VK.

qWDTT содержит обход ошибки VK ID `Unknown method passed`. В Windows-реализацию перенесена та же стратегия повторов:

1. `https://m.vk.ru/login`;
2. после ошибки очистить cookies и открыть `https://m.vk.ru/`;
3. после повторной ошибки очистить cookies и открыть `https://vk.ru/login` с desktop User-Agent `Chrome/131`.

На первых двух попытках используется Android WebView-подобный User-Agent, чтобы поведение было ближе к qWDTT. Текст страницы отслеживается только для обнаружения `Unknown method`; содержимое формы, пароль, cookies и токены не логируются.

Успешный вход определяется не по адресу страницы, а по наличию cookie `remixsid`, как в qWDTT. Пока `remixsid` не появился и VK ID login-flow не завершён, PWDTT не запускает OAuth получения токена.

После появления VK-сессии открывается legacy OAuth URL. VK перенаправляет WebView2 на `https://oauth.vk.com/blank.html#access_token=...`. URL обрабатывается только Go backend: access token не передаётся в React и не выводится в лог.

Для WebView2 используется отдельный профиль `%LOCALAPPDATA%\PWDTT\vk-webview2`, поэтому cookies VK сохраняются между запусками.

## Изоляция WebView2

Окно VK запускается не внутри основного Wails-процесса, а в отдельном helper-процессе того же `PWDTT.exe`.

Это необходимо потому, что используемый Wails fork `go-webview2` завершает процесс через `os.Exit(1)` при внутренней ошибке WebView2. Если запускать второе WebView2-окно непосредственно внутри PWDTT, такая ошибка завершает всё приложение и не может быть обработана обычным Go `error`.

Основной процесс запускает:

```text
PWDTT.exe --pwdtt-vk-auth-helper login
```

Helper создаёт WebView2 и возвращает результат родительскому процессу через захваченный stdout. Access token не передаётся через аргументы командной строки и не выводится пользователю. При отмене операции дочерний процесс завершается через `CommandContext`.

Logout аналогично запускает helper в режиме `clear`, чтобы очистка WebView2 cookies также не могла аварийно завершить основной PWDTT.

Если helper/WebView2 завершится аварийно, основной PWDTT остаётся запущенным и получает безопасный текст ошибки вместо завершения всего приложения.

## Создание звонков

После получения токена backend вызывает тот же метод, что qWDTT:

```text
GET https://api.vk.ru/method/calls.start?access_token=<token>&v=5.199
```

Из `response.join_link` извлекается хеш после `/call/join/`. Между последовательными запросами используется пауза 2 секунды.

## Хранение и logout

Полученный access token сериализуется только в Go backend, шифруется Windows DPAPI для текущего пользователя и хранится как `vk_session.bin` в пользовательском config-каталоге PWDTT.

`VKLogout` удаляет DPAPI-сессию и через изолированный helper очищает cookies отдельного VK WebView2-профиля. Токен, cookies и OAuth URL с токеном не логируются и не отправляются во frontend.

## Ограничение

Flow зависит от legacy OAuth-поведения VK, которое использует qWDTT. Если VK изменит или отключит этот сценарий для `client_id=6287487`, потребуется обновить реализацию вслед за qWDTT.
