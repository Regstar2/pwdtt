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

PWDTT открывает отдельное окно WebView2 с `https://oauth.vk.com/authorize`. Пользователь входит на странице VK. PWDTT не рисует собственную форму логина и не получает пароль VK.

После успешной авторизации VK перенаправляет WebView2 на `https://oauth.vk.com/blank.html#access_token=...`. URL обрабатывается только Go backend: access token не передаётся в React и не выводится в лог.

Для WebView2 используется отдельный профиль `%LOCALAPPDATA%\PWDTT\vk-webview2`, поэтому cookies VK сохраняются между запусками. Это повторяет идею Android WebView-сессии qWDTT и позволяет VK переиспользовать уже выполненный вход.

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
