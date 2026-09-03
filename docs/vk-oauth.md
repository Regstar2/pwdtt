# VK ID OAuth для генерации хешей

Автогенерация VK-хешей в Windows использует системный браузер, Authorization Code + PKCE и локальный callback. Пароль VK не передаётся в PWDTT.

## Требование к VK ID приложению

Нужен отдельный VK ID `app_id`. В настройках приложения должен быть разрешён redirect URI:

```text
http://127.0.0.1:53682/vk-oauth/callback
```

`app_id` не является секретом. Секрет приложения desktop-клиенту не требуется и не должен добавляться в репозиторий.

Пока `app_id` не настроен, ручной ввод VK-хешей работает как раньше, а блок автогенерации показывает, что OAuth недоступен.

## Локальная проверка

Перед запуском Windows-сборки задайте `app_id`:

```powershell
$env:PWDTT_VK_APP_ID = '<VK_ID_APP_ID>'
.\build\bin\pwdtt-windows-amd64.exe
```

Если для VK ID приложения зарегистрирован другой loopback redirect, его можно временно задать через `PWDTT_VK_REDIRECT_URI`. Поддерживается только `http://127.0.0.1:<port>/...` или `http://localhost:<port>/...` с фиксированным портом.

Для production-сборки `vkOAuthClientID` можно заполнить публичным `app_id` в Windows build configuration. Не добавляйте в код client secret, access token, refresh token или cookies.

## Хранение сессии

Access/refresh token остаются только в Go backend. Сессия сериализуется и шифруется Windows DPAPI для текущего пользователя, после чего хранится в пользовательском config-каталоге PWDTT как `vk_session.bin`.

Frontend получает только статус авторизации, прогресс и готовые хеши. Токены, OAuth-коды и cookies не отправляются в frontend и не логируются.
