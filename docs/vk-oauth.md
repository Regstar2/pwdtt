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

PWDTT не начинает вход напрямую с OAuth URL. Как и qWDTT, сначала создаётся обычная VK-сессия, затем проверяется `remixsid`, и только после этого запрашивается legacy access token.

Последовательность:

```text
https://m.vk.ru/login
        ↓
VK ID login
        ↓
remixsid
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
2. после ошибки очистить отдельную VK-сессию и открыть `https://m.vk.ru/`;
3. после повторной ошибки очистить сессию и открыть `https://vk.ru/login` с desktop User-Agent `Chrome/131`.

На первых двух попытках используется Android WebView-подобный User-Agent, чтобы поведение было ближе к qWDTT. Текст страницы проверяется только для обнаружения `Unknown method`; содержимое формы, пароль, cookies и токены не логируются.

Успешный вход определяется по наличию cookie `remixsid`, как в qWDTT. Пока `remixsid` не появился и VK ID login-flow не завершён, PWDTT не запускает OAuth получения токена.

После появления VK-сессии открывается legacy OAuth URL. VK перенаправляет окно на `https://oauth.vk.com/blank.html#access_token=...`. URL обрабатывается только Go backend: access token не передаётся в React и не выводится в лог.

## Windows-хост авторизации

Первые версии использовали дополнительный `go-webview2` control внутри PWDTT, а затем в отдельном helper-процессе. На реальной Windows-проверке этот control аварийно завершался (`exit status 2`) ещё до устойчивого запуска VK login.

Чтобы убрать нестабильный raw COM/WebView2 control из VK-пути, helper теперь запускает установленный Microsoft Edge как отдельное app-окно с изолированным профилем `%LOCALAPPDATA%\PWDTT\vk-edge`.

Управление выполняется только локально через Chrome DevTools Protocol на `127.0.0.1` и случайном свободном порту. PWDTT использует CDP для трёх операций:

- получить текущий URL и обнаружить `Unknown method`;
- проверить наличие `remixsid`, включая HttpOnly cookies;
- после появления VK-сессии перейти на legacy OAuth URL и перехватить redirect с access token.

Microsoft Edge запускается с отдельным `--user-data-dir`, поэтому эта сессия не использует основной профиль браузера пользователя. При logout каталог отдельного VK-профиля удаляется.

VK-авторизация всё равно остаётся в дочернем процессе PWDTT:

```text
PWDTT.exe
  -> PWDTT.exe --pwdtt-vk-auth-helper login
  -> Microsoft Edge --app=<VK login> --user-data-dir=<PWDTT vk-edge>
  -> локальный CDP 127.0.0.1:<случайный порт>
  -> legacy OAuth result
  -> внутренний IPC
  -> основной PWDTT
```

Если helper или Edge завершается, основной PWDTT остаётся запущенным и получает обычную ошибку.

`github.com/wailsapp/go-webview2 v1.0.23` остаётся закреплённым для основного Wails WebView2. В `v1.0.22` был исправленный upstream баг `PutShouldDetectMonitorScaleChanges`, поэтому откат на `v1.0.22` не используется.

## Создание звонков

После получения токена backend вызывает тот же метод, что qWDTT:

```text
GET https://api.vk.ru/method/calls.start?access_token=<token>&v=5.199
```

Из `response.join_link` извлекается хеш после `/call/join/`. Между последовательными запросами используется пауза 2 секунды.

## Хранение и logout

Полученный access token сериализуется только в Go backend, шифруется Windows DPAPI для текущего пользователя и хранится как `vk_session.bin` в пользовательском config-каталоге PWDTT.

`VKLogout` удаляет DPAPI-сессию и отдельный VK-профиль Edge. Токен, cookies и OAuth URL с токеном не логируются и не отправляются во frontend.

## Ограничение

Flow зависит от legacy OAuth-поведения VK, которое использует qWDTT. Если VK изменит или отключит этот сценарий для `client_id=6287487`, потребуется обновить реализацию вслед за qWDTT.

Для автоматической Windows-авторизации требуется установленный Microsoft Edge. Он входит в стандартную поставку современных Windows, но при его ручном удалении PWDTT покажет явную ошибку `Microsoft Edge не найден`.
