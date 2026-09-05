<div align="center">

<img src="assets/icons/icon.png" width="128" alt="Логотип PWDTT">

# PWDTT

Десктопный клиент для туннелирования трафика через TURN/DTLS-инфраструктуру VK с локальным WireGuard-интерфейсом. Этот репозиторий — поддерживаемый форк [luminescq/PWDTT](https://github.com/luminescq/PWDTT) с исправлениями совместимости с актуальными qWDTT-серверами.

[![Release](https://img.shields.io/github/v/release/Regstar2/PWDTT?display_name=tag&sort=semver&style=for-the-badge&logo=github&label=release)](../../releases)
[![CI](https://img.shields.io/github/actions/workflow/status/Regstar2/PWDTT/build.yml?branch=main&style=for-the-badge&logo=githubactions&logoColor=white&label=CI)](../../actions/workflows/build.yml)
[![Platform](https://img.shields.io/badge/platform-Windows%20%7C%20Linux%20%7C%20macOS-0A7EA4?style=for-the-badge)](#требования)
[![License](https://img.shields.io/github/license/Regstar2/PWDTT?style=for-the-badge&label=license)](LICENSE)

[Быстрый старт](#быстрый-старт) ·
[Документация](#документация) ·
[Релизы](../../releases) ·
[Обратная связь](../../issues)

</div>

---

## О проекте

PWDTT поднимает локальный WireGuard-интерфейс и передаёт его трафик через TURN/DTLS-серверы VK, оборачивая пакеты в RTP с шифрованием ChaCha20-Poly1305. Для выхода в интернет используется настроенный пользователем `wdtt-server` на VPS.

```text
Приложение → WireGuard → ChaCha20-Poly1305/RTP → VK TURN/DTLS → wdtt-server → интернет
```

Проект предназначен для пользователей, которым нужен desktop-клиент для существующей инфраструктуры WDTT/qWDTT на Windows, Linux или macOS.

## Статус проекта

Репозиторий развивается как отдельный поддерживаемый форк `luminescq/PWDTT`. Актуальные опубликованные версии доступны в [GitHub Releases](../../releases).

### Что изменено в этом форке

В `v1.5.2` относительно базового upstream-состояния:

- добавлена команда `AUTH` для DTLS-воркеров, которые не запрашивают WireGuard-конфигурацию;
- восстановлена совместимость с более новыми qWDTT-серверами, которые требуют авторизацию каждого worker-соединения;
- исправлено отображение длинного списка сохранённых серверов: список прокручивается, а длинные названия не ломают разметку.

В текущей ветке `main` доступны автоматическая генерация VK call hashes, централизованное управление и функциональная проверка хешей, улучшенный connection dashboard, TURN failover и Windows-защита от IPv6 leak.

## Возможности

- локальный WireGuard-туннель;
- транспорт через VK TURN/DTLS;
- RTP-обёртка с ChaCha20-Poly1305;
- профили серверов и импорт `wdtt://`-ссылок;
- поддержка qWDTT-ссылок;
- до четырёх VK call hashes в профиле;
- автоматическое создание VK call hashes на Windows через авторизацию VK;
- сборка для Windows amd64, Linux amd64 и macOS Universal;
- встроенный отчёт с диагностической информацией и логами текущей сессии.

## Быстрый старт

Для Windows x64 готовые сборки публикуются на странице [Releases](../../releases).

1. Скачайте `pwdtt-windows-amd64.exe`.
2. Запустите приложение.
3. Добавьте сервер кнопкой `+`: вставьте `wdtt://`-ссылку или заполните параметры вручную.
4. В настройках Hash добавьте до четырёх хешей вручную либо на Windows войдите в VK и создайте их автоматически.
5. Выберите сервер и нажмите кнопку подключения. На Windows при настройке туннеля подтвердите штатный запрос UAC; вручную запускать всё приложение от имени администратора не требуется.

Для Linux x86_64 и macOS Universal артефакты также собираются GitHub Actions и публикуются для релизов, где они доступны.

## Требования

### Windows

- архитектура x86-64 для готовой Windows-сборки;
- доступ к настроенному WDTT/qWDTT-серверу;
- Microsoft Edge для автоматической авторизации VK;
- валидные VK call hashes либо возможность создать их из приложения;
- возможность подтвердить штатный запрос UAC при создании WireGuard-интерфейса и изменении маршрутов/firewall.

GUI PWDTT запускается без повышенных прав. Когда соединение доходит до настройки WireGuard, приложение запускает отдельный минимальный privileged helper через стандартный UAC flow Windows. При отказе от UAC подключение отменяется и приложение не продолжает работу с ложным статусом VPN. VK-авторизация, WebView2 и остальные обычные операции не повышаются до администратора.

Драйвер Wintun используется приложением для WireGuard-интерфейса.

### Linux

Для сборки и запуска нужны WireGuard tools и WebKitGTK 4.1. На Debian/Ubuntu:

```bash
sudo apt install wireguard-tools libayatana-appindicator3-dev pkg-config gcc libwebkit2gtk-4.1-dev
```

Управление сетевым интерфейсом требует повышенных прав.

### macOS

Исходники содержат macOS-путь на базе userspace WireGuard. Создание сетевого интерфейса и маршрутов требует административных прав.

## Использование

### Ссылки серверов

Формат `wdtt://`:

```text
wdtt://<IP>:<DTLS_PORT>:<WG_PORT>:<PROXY_PORT>:<PASSWORD>[:<HASH1>,<HASH2>,...][#название]
```

Поля 1–5 обязательны. Хеши опциональны, передаются через запятую; поддерживается до четырёх значений. `#название` задаёт необязательный псевдоним профиля.

Пример:

```text
wdtt://1.2.3.4:56000:56001:0:mypassword:AbCdEfGh,XyZ12345#Мой сервер
```

Ссылку можно вставить через кнопку `+` или `Ctrl+V` в окне приложения. Поддерживаются также qWDTT-ссылки.

### VK call hashes

Ручной ввод работает на всех платформах: можно вставить сам hash или полную ссылку `vk.com/call/join/<hash>`; приложение сохраняет нормализованное значение в профиле.

На Windows текущий `main` также умеет автоматически создавать VK-звонки: в окне Hash войдите в VK, затем используйте `Создать 1 хеш` или `Заполнить свободные`. Авторизация выполняется в отдельном профиле Microsoft Edge; access token остаётся в Go backend и хранится через Windows DPAPI.

## Архитектура

PWDTT состоит из Go backend и интерфейса Wails/React. Сетевой путь разделён на локальный WireGuard-интерфейс, worker-соединения через VK TURN/DTLS и удалённый `wdtt-server`, который принимает туннельный трафик и выпускает его в интернет.

## Диагностика

Если соединение работает некорректно:

1. Откройте настройки приложения.
2. Нажмите `Отчёт`.
3. Создайте [Issue](../../issues/new) и приложите полученный отчёт вместе с описанием проблемы.

Отчёт содержит сведения о системе, версии приложения, используемой на Windows модели elevation и логи текущей сессии. Перед публикацией всё равно проверьте текст отчёта и удалите данные, которые не хотите размещать публично.

## Сборка

Зависимости для разработки:

- Go 1.26+;
- Node.js 22+;
- Wails v2.

```bash
git clone https://github.com/Regstar2/PWDTT.git
cd PWDTT
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

Linux amd64:

```bash
wails build -platform linux/amd64 -tags webkit2_41 -o pwdtt-linux-amd64
```

Windows amd64:

```bash
wails build -platform windows/amd64 -o pwdtt-windows-amd64.exe
```

macOS Universal необходимо собирать на macOS:

```bash
wails build -platform darwin/universal -o pwdtt-macos
```

GitHub Actions workflow `.github/workflows/build.yml` содержит отдельные сборки для Linux/Windows и macOS.

## Документация

- [Релизы форка](../../releases) — опубликованные версии, изменения и готовые артефакты.
- [Issues форка](../../issues) — известные проблемы и текущие задачи.
- [Upstream PWDTT](https://github.com/luminescq/PWDTT) — исходный desktop-проект.
- [proxy-turn-vk-android](https://github.com/amurcanov/proxy-turn-vk-android) — исходный Android-проект, на базе которого появился PWDTT.

## Дорожная карта

Windows-приёмка автоматической генерации и централизованного управления VK call hashes завершена: реальные хеши создаются, проверяются через VK/TURN/WRAP/DTLS и подтверждены рабочим подключением с трафиком.

## Обратная связь

Для ошибок и предложений используйте [GitHub Issues](../../issues). Для проблем подключения приложите отчёт из приложения и укажите платформу, способ подключения и воспроизводимые шаги.

## Происхождение и благодарности

`Regstar2/PWDTT` является GitHub-форком [luminescq/PWDTT](https://github.com/luminescq/PWDTT). Upstream PWDTT создан как desktop-адаптация [amurcanov/proxy-turn-vk-android](https://github.com/amurcanov/proxy-turn-vk-android).

Общие исправления форка по возможности могут отправляться обратно в upstream отдельными pull request; дополнительные изменения и релизы этого репозитория ведутся независимо.

## Ограничения

- Для работы нужен собственный настроенный WDTT/qWDTT-сервер и рабочие VK call hashes.
- Поведение зависит от внешней инфраструктуры VK и протокола qWDTT; их изменения могут потребовать обновления клиента.
- Набор готовых бинарных файлов зависит от конкретного релиза; актуальные артефакты смотрите на странице Releases.
- Автоматическое создание VK call hashes доступно на Windows и требует Microsoft Edge; ручной ввод хешей сохраняется на всех поддерживаемых платформах.
- Проект не является официальным продуктом VK.

> [!IMPORTANT]
> PWDTT — технический инструмент для туннелирования собственного трафика через настроенный пользователем сервер. Пользователь самостоятельно отвечает за соблюдение применимого законодательства и правил используемых сервисов.

## Лицензия

Проект распространяется по лицензии [GNU General Public License v3.0](LICENSE).