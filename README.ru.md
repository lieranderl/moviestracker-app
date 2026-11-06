# Moviestracker

[English](README.md) · **Русский**

[![CI](https://github.com/lieranderl/moviestracker-app/actions/workflows/ci.yml/badge.svg)](https://github.com/lieranderl/moviestracker-app/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/lieranderl/moviestracker-app?sort=semver)](https://github.com/lieranderl/moviestracker-app/releases/latest)
[![License: AGPL-3.0](https://img.shields.io/badge/license-AGPL--3.0-blue)](LICENSE)
[![OpenSSF Best Practices](https://www.bestpractices.dev/projects/16384/badge)](https://www.bestpractices.dev/projects/16384)

Самостоятельно размещаемый каталог фильмов и сериалов со встроенным
воспроизведением. Найдите, что посмотреть, выберите источник и смотрите в
браузере или на телевизоре. Moviestracker сам запускает
[TorrServer](https://github.com/YouROK/TorrServer), не требует облачной учётной
записи и не показывает на страницах адрес вашего TorrServer и ключи.

- **Открывайте новое:** популярные фильмы и сериалы, поиск, трейлеры, актёры,
  сезоны и рейтинги IMDb.
- **Находите источники:** фильтры по качеству и HDR, затем по трекеру и озвучке.
- **Смотрите где угодно:** прямые потоки или HLS, который конвертирует GStreamer,
  так что MKV, AC3 и DTS играют в любом браузере. Плейлисты и ссылки работают с
  VLC и Smart TV.
- **Смотрите всей семьёй:** локальные учётные записи для администраторов и
  зрителей и живая панель с тем, что сейчас играет.
- **Подключайте приложения TorrServer:** шлюз с авторизацией позволяет TorrServe,
  Lampa и подобным приложениям пользоваться тем же TorrServer.
- **Русский и английский:** интерфейс и метаданные TMDB на любом из двух языков.

> Moviestracker не хранит, не загружает, не раздаёт и не распространяет
> медиафайлы. Используйте его только для контента, на который у вас есть
> права. Этот продукт использует API TMDB, но не одобрен и не
> сертифицирован TMDB.

## Скачать

| Где | Что скачать |
| --- | --- |
| **macOS 13+** (Apple Silicon, Intel), приложение в строке меню | [`Moviestracker-<version>.dmg`](https://github.com/lieranderl/moviestracker-app/releases/latest) |
| **Windows 10/11** x64 (предварительная версия), значок в трее | [`Moviestracker-Setup-<version>-x64.exe`](https://github.com/lieranderl/moviestracker-app/releases/latest) |
| **Linux, NAS, домашний сервер** (`amd64`, `arm64`) | [Образ Docker](#docker): `ghcr.io/lieranderl/moviestracker` |
| **Любой браузер**, облачная версия | [moviestracker.net](https://moviestracker.net) |

### macOS и Windows

1. Установите и откройте приложение. Установщики пока не подписаны, поэтому
   система один раз попросит подтверждение.
2. Создайте учётную запись администратора на открывшейся странице настройки.

TorrServer уже внутри. На macOS для воспроизведения MKV в браузере установите
GStreamer в **Настройки → Источники**; в сборке для Windows он уже есть.

### Docker

Сохраните [compose.yaml](compose.yaml) в папку и выполните:

```bash
docker compose up -d
docker compose logs moviestracker   # показывает одноразовый код настройки
```

Откройте `http://<хост>:8095`, введите код и создайте администратора. Данные
лежат в томе `/data`. Обновление:

```bash
docker compose pull && docker compose up -d
```

### Другие устройства дома

Откройте адрес из меню приложения (например, `http://192.168.1.20:8095`) и
войдите с учётной записью.

### Веб-версия

Войдите через Google на [moviestracker.net](https://moviestracker.net). Избранное
и настройки хранятся в вашей учётной записи. Своего TorrServer у веб-версии нет:
браузер обращается к вашему TorrServer напрямую, и он должен быть по адресу
`https://` или на том же компьютере (`http://localhost:8090`).

### Проверка загрузки

В каждом [релизе](https://github.com/lieranderl/moviestracker-app/releases/latest) есть `checksums.txt` и аттестации сборки:

```bash
gh attestation verify Moviestracker-<version>.dmg -R lieranderl/moviestracker-app
gh attestation verify oci://ghcr.io/lieranderl/moviestracker:<version> -R lieranderl/moviestracker-app
```

## Приватность и данные

В локальном приложении нет аналитики и телеметрии. Оно обращается только к этим
сервисам:

- **TMDB, JacRed и сервис рейтингов IMDb** видят названия, которые вы ищете.
- **YouTube** отдаёт вашему браузеру трейлеры и их миниатюры.
- **BitTorrent-пиры** соединяются с TorrServer во время воспроизведения.
- **GitHub** раз в день проверяется на наличие нового релиза. Это отключается в
  **Настройки → Обновления**.
- **Сайт загрузки GStreamer** использует приложение для Mac, только когда вы об
  этом попросите.

| Приложение | Где хранятся данные |
| --- | --- |
| macOS | `~/Library/Application Support/moviestracker` |
| Windows | `%LOCALAPPDATA%\Moviestracker` |
| Docker | том `/data` |

Веб-версия хранит данные в Google Cloud Firestore под идентификатором вашей
учётной записи Google: см. [политику конфиденциальности](https://moviestracker.net/privacy).

## Помощь и участие

- **Ошибки и идеи:** [создайте issue](https://github.com/lieranderl/moviestracker-app/issues/new/choose).
- **Проблемы безопасности:** сообщайте приватно, как описано в [SECURITY.md](SECURITY.md).
- **Код:** сначала прочитайте [CONTRIBUTING.md](CONTRIBUTING.md).

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
make test       # линтеры, затем тесты фронтенда и Go с детектором гонок
make lint       # сгенерированные файлы, vet, staticcheck, golangci-lint, gofmt
make ci         # полный конвейер: формат, линтеры, templ, ассеты, тесты, безопасность, сборка
make build      # линтеры, затем самодостаточный бинарник bin/server
```

`make firestore` запускает эмулятор Firestore для `make test-store`. Все
настройки и переменные окружения описаны в
[docs/CONFIGURATION.md](docs/CONFIGURATION.md); заметки по устройству — в
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md), [docs/DATASTAR.md](docs/DATASTAR.md),
[docs/TESTING.md](docs/TESTING.md) и [docs/MAINTAINING.md](docs/MAINTAINING.md).
Изменения вносятся через pull request из отдельной ветки; `main` защищена и
сливается squash после прохождения CI. См. [CONTRIBUTING.md](CONTRIBUTING.md).

## Лицензия

[AGPL-3.0-only](LICENSE). О встроенном ПО см. [NOTICE](NOTICE). TorrServer,
поставляемый с приложениями и образом Docker, — отдельная программа под
GPL-3.0; её лицензия и ссылка на исходный код приложены.
