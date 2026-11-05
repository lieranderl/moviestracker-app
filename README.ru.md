# Moviestracker

[English](README.md) · **Русский**

[![CI](https://github.com/lieranderl/moviestracker-app/actions/workflows/ci.yml/badge.svg)](https://github.com/lieranderl/moviestracker-app/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/lieranderl/moviestracker-app?sort=semver)](https://github.com/lieranderl/moviestracker-app/releases/latest)
[![License: AGPL-3.0](https://img.shields.io/badge/license-AGPL--3.0-blue)](LICENSE)

Самостоятельно размещаемый каталог фильмов и сериалов со встроенным
воспроизведением. Он показывает, что сейчас популярно (трейлеры, актёры,
сезоны, рейтинги), находит источники для фильма и воспроизводит их в браузере
или на телевизоре через [TorrServer](https://github.com/YouROK/TorrServer),
который запускает сам. Локальному приложению облачная учётная запись не нужна,
а его страницы никогда не показывают адрес вашего TorrServer и ключи.

- **Открывайте новое:** популярные фильмы и сериалы, поиск, трейлеры, актёры,
  сезоны, рейтинги IMDb.
- **Находите источники:** фильтры по качеству и HDR, затем по трекеру и
  озвучке.
- **Смотрите где угодно:** прямые потоки или HLS, который конвертирует
  GStreamer, так что MKV/AC3/DTS играют в любом браузере; плейлисты и ссылки
  для VLC и Smart TV.
- **Несколько пользователей:** локальные учётные записи (администраторы и
  зрители) и живая панель с тем, что сейчас играет.
- **Приложения TorrServer:** шлюз с авторизацией позволяет TorrServe, Lampa и
  подобным приложениям пользоваться тем же TorrServer.
- **Русский и английский** интерфейс, а метаданные TMDB — на любом из двух
  языков.

> Moviestracker не хранит, не загружает, не раздаёт и не распространяет
> медиафайлы. Используйте его только для контента, на который у вас есть
> права. Этот продукт использует API TMDB, но не одобрен и не
> сертифицирован TMDB.

## Скачать

| Где | Файл |
| --- | --- |
| **macOS 13+** (Apple Silicon, Intel), приложение в строке меню | `Moviestracker-<version>.dmg` |
| **Windows 10/11** x64 (предварительная версия), значок в трее | `Moviestracker-Setup-<version>-x64.exe` |
| **Linux, NAS, домашний сервер** (`amd64`, `arm64`) | `ghcr.io/lieranderl/moviestracker` |
| **Любой браузер**, облачная версия | [moviestracker.net](https://moviestracker.net) |

Файлы лежат в [последнем релизе](https://github.com/lieranderl/moviestracker-app/releases/latest)
вместе с `checksums.txt` и аттестациями сборки:

```bash
gh attestation verify Moviestracker-<version>.dmg -R lieranderl/moviestracker-app
```

### Быстрый старт

- **macOS / Windows:** установите и откройте приложение (установщики пока не
  подписаны, поэтому система один раз попросит подтверждение), затем создайте
  учётную запись администратора на открывшейся странице настройки. TorrServer
  уже внутри. На macOS для воспроизведения MKV в браузере установите
  GStreamer в **Настройки → Источники**; в сборке для Windows он уже есть.
- **Docker:** сохраните [compose.yaml](compose.yaml) в папку, затем:

  ```bash
  docker compose up -d
  docker compose logs moviestracker   # одноразовый код настройки
  ```

  Откройте `http://<хост>:8095`, введите код и создайте администратора.
  Данные лежат в томе `/data`. Обновление:
  `docker compose pull && docker compose up -d`.

Другие устройства в локальной сети открывают адрес из меню приложения
(например, `http://192.168.1.20:8095`) и входят с учётной записью.

- **Веб-версия:** вход через Google; избранное и настройки хранятся в вашей
  учётной записи. Своего TorrServer у неё нет: браузер обращается к вашему
  TorrServer напрямую, и он должен быть по адресу `https://` или на том же
  компьютере (`http://localhost:8090`).

## Технологии

| Слой | Стек |
| --- | --- |
| Язык | Go 1.27, стандартный `net/http` (`ServeMux`) |
| Представления | [Templ](https://templ.guide), типобезопасные компоненты, компилируемые в Go |
| Интерактивность | [Datastar](https://data-star.dev) v1: состояние на сервере через SSE, без клиентского фреймворка |
| Стили | [Tailwind CSS](https://tailwindcss.com) v4, [DaisyUI](https://daisyui.com) v5 |
| Иконки | Lucide, отрисовываются на сервере |
| Собственный JS | Только там, где Datastar не справится: плеер на hls.js (`frontend/player.js`), обращения браузера к TorrServer в веб-версии (`frontend/torrserver.js`), тема (`frontend/theme.js`) |
| Сборка фронтенда | Bun (CSS, JS-бандлы, генерация иконок) |
| Воспроизведение | TorrServer (дочерний процесс) с GStreamer для HLS |
| Источники данных | TMDB (метаданные), JacRed (источники), сервис рейтингов IMDb |
| Локальное хранилище | Один JSON-файл состояния, доступный только владельцу, заменяется атомарно |
| Облачная версия | `cmd/web` в Cloud Run, вход через Google, Firestore (`internal/store`, `internal/releases`) |
| Настольные оболочки | приложение в строке меню macOS на Swift (`macos/`), приложение в трее Windows на Go с `fyne.io/systray` (`cmd/tray`) |
| Упаковка | образ Docker (`linux/amd64`, `linux/arm64`, работает от uid 1000), DMG для macOS, установщик для Windows |
| CI/CD | GitHub Actions: тесты, линтеры, CodeQL, релиз с аттестациями |

## Как это работает

- Сервер рендерит HTML через Templ и обновляет страницы, передавая патчи
  HTML/сигналов по Server-Sent Events. Общие темы (`internal/live`) опрашивают
  TorrServer один раз на тему, а не на каждого зрителя.
- Авторизация локальная: учётные записи с хэшами bcrypt, сохраняемые сессии
  (хранятся только хэши токенов), ограничение частоты входов, список
  разрешённых хостов против DNS rebinding и строгая CSP.
- TorrServer работает как управляемый дочерний процесс, слушает
  `127.0.0.1` под сгенерированным паролем и перезапускается с задержкой после
  сбоя. Отдельный шлюз (порты 8090/8091) открывает его другим приложениям с
  отдельным логином для каждого.
- Ссылки для просмотра подписаны HMAC и действуют 7 дней.
- Локальное приложение и облачная веб-версия рендерят одни и те же
  компоненты `internal/views` и используют один плеер; различаются только
  адаптеры (авторизация, хранилище, обращения браузера к TorrServer).
- Тексты интерфейса в коде английские и переводятся при отрисовке
  (`internal/i18n`); тест падает, если нет русского перевода.

```
cmd/server   локальное приложение     internal/handlers, views   HTTP + интерфейс Templ
cmd/tray     приложение в трее Windows internal/engine, gateway  TorrServer
macos/       строка меню macOS        internal/update            проверка релизов
cmd/web      облачное приложение      internal/tmdb, jacred      источники данных
frontend/    плеер, тема, иконки      internal/auth, store       учётные записи, Firestore
```

## Разработка

Нужны Go 1.27, [Bun](https://bun.sh) и [Air](https://github.com/air-verse/air)
(для `make dev`). Templ, golangci-lint, staticcheck, gosec и govulncheck
запускаются через `go tool`. Docker нужен только для образа.

```bash
make dev        # сервер разработки с горячей перезагрузкой Air
make web-dev    # облачное веб-приложение на http://localhost:8080
make test       # go test -race ./...
make lint       # vet, staticcheck, golangci-lint, gofmt
make ci         # полный конвейер: формат, линтеры, templ, ассеты, тесты, безопасность, сборка
make build      # самодостаточный бинарник bin/server
```

`make firestore` запускает эмулятор Firestore для `make test-store`. Все
настройки и переменные окружения описаны в
[docs/CONFIGURATION.md](docs/CONFIGURATION.md); заметки по устройству — в
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md), [docs/DATASTAR.md](docs/DATASTAR.md),
[docs/TESTING.md](docs/TESTING.md) и [docs/MAINTAINING.md](docs/MAINTAINING.md).
Изменения вносятся через pull request из отдельной ветки; `main` защищена и
сливается squash после прохождения CI. См. [CONTRIBUTING.md](CONTRIBUTING.md).

## Приватность и данные

TMDB, JacRed и сервис рейтингов IMDb видят названия, которые вы ищете, а
TorrServer общается с BitTorrent-пирами во время воспроизведения. Локальное
приложение также раз в день спрашивает у GitHub, нет ли нового релиза (это
отключается в **Настройки → Обновления**), а приложение для Mac скачивает
GStreamer, когда вы об этом попросите. Больше никуда обращений нет: ни
аналитики, ни телеметрии. Данные хранятся в
`~/Library/Application Support/moviestracker` (macOS),
`%LOCALAPPDATA%\Moviestracker` (Windows) или в томе `/data` (Docker).

О проблемах безопасности сообщайте приватно: см. [SECURITY.md](SECURITY.md).

## Лицензия

[AGPL-3.0-only](LICENSE). О встроенном ПО см. [NOTICE](NOTICE). TorrServer,
поставляемый с приложениями и образом Docker, — отдельная программа под
GPL-3.0; её лицензия и ссылка на исходный код приложены.
