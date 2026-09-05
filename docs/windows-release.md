# Windows release: installer, Authenticode и checksums

## Официальные Windows-артефакты

Для Windows x64 публикуются два payload-файла:

- `pwdtt-windows-amd64-setup.exe` — рекомендуемый per-user installer;
- `pwdtt-windows-amd64.exe` — portable-вариант без установки.

Installer устанавливает `PWDTT.exe` в `%LOCALAPPDATA%\Programs\PWDTT`, создаёт ярлык в меню «Пуск» и регистрирует штатный uninstaller. Сам installer не требует постоянного admin-контекста. Существующий UAC flow PWDTT не меняется: повышенные права запрашивает только минимальный helper, когда приложению нужно настроить WireGuard, маршруты или firewall.

Installer использует постоянный `AppId`, поэтому повторный запуск более новой версии обновляет существующую установку. Пользовательские настройки не относятся к installer payload и при upgrade/uninstall не удаляются.

## Installer toolchain

CI использует GitHub-hosted `windows-2025` и Inno Setup `6.7.1`. Workflow проверяет фактическую версию `ISCC.exe` и падает при неожиданном изменении toolchain, чтобы обновление packaging tool было явным.

Перед публикацией CI выполняет smoke:

1. устанавливает baseline installer в изолированную временную директорию;
2. проверяет наличие `PWDTT.exe` и uninstaller;
3. устанавливает текущий installer поверх baseline;
4. проверяет сохранение существующего файла в каталоге установки;
5. запускает uninstall;
6. проверяет удаление `PWDTT.exe` и uninstaller.

Smoke не запускает само VPN-приложение и не меняет сетевую конфигурацию.

## Authenticode signing

Private key и сертификат не хранятся в репозитории.

Для release signing нужно настроить два GitHub Actions secrets:

- `WINDOWS_SIGNING_PFX_BASE64` — содержимое PFX в Base64;
- `WINDOWS_SIGNING_PFX_PASSWORD` — пароль PFX.

Пример локального получения Base64 без добавления PFX в Git:

```powershell
[Convert]::ToBase64String([IO.File]::ReadAllBytes("codesign.pfx"))
```

Signing secrets используются только для `v*` tag, созданного владельцем репозитория. Pull request, обычный push и tag от другого actor не получают release signing path.

При подписи CI:

1. декодирует PFX во временный файл на ephemeral GitHub-hosted Windows runner;
2. импортирует сертификат во временное хранилище `CurrentUser\My`;
3. подписывает portable `.exe` SHA-256 с RFC 3161 timestamp;
4. проверяет `Get-AuthenticodeSignature` и требует статус `Valid`;
5. собирает installer уже с подписанным portable `.exe`;
6. подписывает и проверяет installer;
7. удаляет импортированный сертификат и временный PFX.

Если оба signing secrets отсутствуют, tag release использует документированный unsigned fallback. Если настроен только один из двух secrets, workflow завершается ошибкой. Статус подписи добавляется в release notes.

CI отдельно проверяет signing helper с временным self-signed code-signing certificate; release certificate для этого теста не используется.

## SHA-256

После получения только финальных Linux, Windows и macOS payload-файлов `scripts/release.ps1` вычисляет SHA-256 и создаёт `SHA256SUMS`.

Manifest содержит hashes для:

- `pwdtt-linux-amd64`;
- `pwdtt-windows-amd64.exe`;
- `pwdtt-windows-amd64-setup.exe`;
- `PWDTT-macos.zip`.

На Windows downloaded artifact можно проверить так:

```powershell
Get-FileHash .\pwdtt-windows-amd64-setup.exe -Algorithm SHA256
```

Полученное значение должно совпасть со строкой в `SHA256SUMS`.

## Release protection

GitHub Release публикуется только для `v*` tag от владельца репозитория и только после успешных CI/build/package jobs.

Перед публикацией workflow:

- проверяет, что checkout, `GITHUB_SHA` и commit, на который указывает tag, совпадают;
- отказывается продолжать, если GitHub Release с таким tag уже существует;
- проверяет наличие всех четырёх финальных payload-файлов;
- генерирует `SHA256SUMS` и release notes из фактического `dist/`;
- передаёт `gh release create --verify-tag` только явный список финальных assets.

Raw Windows artifact между build и packaging остаётся внутренним workflow artifact с коротким retention и никогда не публикуется в GitHub Release.
