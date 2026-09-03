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

## Изоляция WebView2 helper

VK WebView2 запускается не внутри основного Wails-процесса, а в отдельном дочернем процессе того же EXE:

```text
PWDTT.exe
  -> PWDTT.exe --pwdtt-vk-auth-helper login
  -> WebView2 / VK login / OAuth
  -> внутренний IPC через stdout
  -> основной PWDTT
```

Это необходимо потому, что `go-webview2` при некоторых внутренних ошибках завершает текущий процесс. При падении helper основной PWDTT остаётся запущенным и показывает ошибку.

Используется `github.com/wailsapp/go-webview2 v1.0.23`. В `v1.0.22` метод `PutShouldDetectMonitorScaleChanges` передавал в COM указатель на Go `bool` вместо значения Windows `BOOL`; `v1.0.23` исправляет этот вызов. Этот метод выполняется при создании WebView2 controller и мог приводить к аварийному завершению helper на Windows.

Если helper всё же завершается аварийно, PWDTT добавляет в сообщение безопасный сокращённый фрагмент stderr. Строки, похожие на access token, Authorization header или Cookie header, отбрасываются.

## Создание звонков

После получения токена backend вызывает тот же метод, что qWDTT:

```text
GET https://api.vk.ru/method/calls.start?access_token=<token>&v=5.199
```

Из `response.join_link` извлекается хеш после `/call/join/`. Между последовательными запросами используется пауза 2 секунды.

## Хранение и logout

Полученный access token сериализуется только в Go backend, шифруется Windows DPAPI для текущего пользователя и хранится как `vk_session.bin` в пользовательском config-каталоге PWDTT.

`VKLogout` удаляет DPAPI-сессию и очищает cookies отдельного VK WebView2-профиля. Токен, cookies и OAuth URL с токеном не логируются и не отправляются во frontend.

## Ограничение

Flow зависит от legacy OAuth-поведения VK, которое использует qWDTT. Если VK изменит или отключит этот сценарий для `client_id=6287487`, потребуется обновить реализацию вслед за qWDTT.
