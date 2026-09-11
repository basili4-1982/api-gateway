# API Gateway

Реверс-прокси / API-шлюз с JWT-аутентификацией по маршрутам, ролевым доступом и рейт-лимитингом.

## Возможности

- **Маршрутизация по префиксу пути** на несколько бэкенд-сервисов, с поддержкой маршрутизации по `Host` (в т.ч. wildcard-домены `*.example.com`)
- **JWT-аутентификация по маршруту** — часть маршрутов требует токен, часть публична
- **Ролевой доступ** — проверка ролей пользователя из claims JWT для конкретного маршрута
- **Rate limiting по маршруту** — token bucket на IP, плюс глобальный лимит
- **Health checks и circuit breaker** — автоматическая проверка доступности таргетов, разрыв цепи только на ошибках транспорта
- **CORS** — настраиваемый allowlist источников, отдельное поведение для dev-режима
- **Basic Auth** — с constant-time сравнением хэшей, для служебных путей
- **Проброс claims в заголовки** — маппинг полей JWT в HTTP-заголовки для бэкендов
- **Раздача статики / SPA** — с fallback на `index.html` и поддержкой flat-HTML экспорта (Next.js)
- **Service discovery** — обнаружение бэкендов по labels контейнеров (Docker/Podman), в стиле Traefik
- **Вебхуки/NATS** — публикация событий `on_request`/`on_response`
- **Graceful shutdown**, структурированное логирование (Zap), метрики, трейсинг (OpenTelemetry)

## Быстрый старт

```bash
# Скопировать конфиг
cp config.local.example.yaml config.local.yaml
# Отредактировать config.local.yaml — указать свои сервисы и секрет для JWT

# Запуск
go run ./cmd/ -config config.local.yaml
```

## Конфигурация

Полный список опций — в [`config.local.example.yaml`](config.local.example.yaml).

### Пример маршрутов

| Путь | Таргет | Auth | Роли |
|---|---|---|---|
| `POST /api/v1/auth/login` | auth-api | Нет | — |
| `POST /api/v1/auth/register` | auth-api | Нет | — |
| `POST /api/v1/auth/refresh` | auth-api | Нет | — |
| `/api/v1/auth/*` | auth-api | Да | — |
| `/api/v1/client/*` | client-api | Да | user, admin |
| `/api/v1/admin/*` | admin-api | Да | admin |
| `/api/v1/storage/public/*` | storage-api | Нет | — |
| `/api/v1/storage/*` | storage-api | Да | — |

## Service discovery (Docker/Podman)

Гейтвей умеет находить бэкенды по labels контейнеров — как Docker-провайдер
Traefik, только без отдельного auth-сервиса и плагинного маркетплейса. Он
подключается к Docker/Podman API через unix socket, находит контейнеры с
маркером `gateway.enable=true`, строит из их labels таргеты и правила роутинга
и применяет их к уже работающему прокси. Обнаруженное **дополняет** статический
конфиг; при конфликте имён/путей приоритет у статики.

Таргет адресуется по имени сервиса (`http://<name>:<port>`), поэтому несколько
реплик одного сервиса раскидывает встроенный DNS Docker/Podman — балансировка
внутри гейтвея не нужна.

### Конфигурация

```yaml
discovery:
  enabled: true
  provider: docker            # docker | podman (один и тот же клиент)
  host: "unix:///var/run/docker.sock"
  api_version: "v1.41"        # "" = запросы без версии
  label_prefix: "gateway"
  service_name_labels:
    - "com.docker.compose.service"
    - "io.podman.compose.service"
  network: ""                 # учитывать только контейнеры этой сети
  debounce: 500ms             # дебаунс событий контейнеров
  resync_interval: 5m         # периодический ре-синк
  default_timeout: 30s
```

| поле | по умолчанию | смысл |
|---|---|---|
| `enabled` | `false` | включить discovery |
| `provider` | `docker` | `docker` или `podman` |
| `host` | `unix:///var/run/docker.sock` | socket Docker/Podman API |
| `api_version` | `v1.41` | префикс версии API; `""` — без версии |
| `label_prefix` | `gateway` | префикс labels |
| `service_name_labels` | compose-сервис | цепочка фолбэков имени таргета |
| `network` | `""` | фильтр по сети (пусто — все) |
| `debounce` | `500ms` | задержка перед ре-синком по событиям |
| `resync_interval` | `5m` | период полного ре-синка |
| `default_timeout` | `30s` | таймаут таргета по умолчанию |

### Labels

Маркер: `gateway.enable=true` (`true`/`1`/`yes`, регистронезависимо).

Таргет (один на контейнер):

| label | обяз. | смысл | по умолчанию |
|---|---|---|---|
| `gateway.enable` | да | опт-ин контейнера | — |
| `gateway.name` | нет | имя таргета | `service_name_labels`, иначе имя контейнера |
| `gateway.port` | да* | порт | *единственный exposed-порт TCP |
| `gateway.scheme` | нет | `http`/`https` | `http` |
| `gateway.timeout` | нет | таймаут запроса к цели | `discovery.default_timeout` |
| `gateway.health` | нет | health-путь или полный URL | — |
| `gateway.weight` | нет | вес таргета | `0` |

Роутеры (0..N на контейнер). Поля задаются коротко (`gateway.<field>` → роутер
`default`) или именованно (`gateway.router.<id>.<field>`):

| поле | смысл | по умолчанию |
|---|---|---|
| `host` | host-правило (wildcard `*.example.com`) | — |
| `path_prefix` | префикс пути | — |
| `methods` | HTTP-методы через запятую | все |
| `strip_path` | срезать префикс при проксировании | `false` |
| `auth.required` | требовать JWT | глобальный `jwt.required` |
| `auth.roles` | роли через запятую | — |
| `auth.strip_token` | удалять `Authorization` | глобальный |
| `rate_limit.rps` | запросов/сек (token bucket) | — |
| `rate_limit.burst` | burst | — |

Роутер без `host` и без `path_prefix` пропускается (нечем матчить).

### Примеры

Один роут (короткая форма):

```yaml
labels:
  gateway.enable: "true"
  gateway.port: "8085"
  gateway.path_prefix: "/api/blog"
```

Несколько роутов с auth и rate-limit (именованная форма):

```yaml
labels:
  gateway.enable: "true"
  gateway.name: "blog"
  gateway.port: "8085"
  gateway.health: "/health"

  gateway.router.public.path_prefix: "/api/blog"
  gateway.router.public.auth.required: "false"

  gateway.router.admin.path_prefix: "/api/admin/blog"
  gateway.router.admin.strip_path: "true"
  gateway.router.admin.methods: "GET,POST"
  gateway.router.admin.auth.required: "true"
  gateway.router.admin.auth.roles: "admin"
  gateway.router.admin.rate_limit.rps: "20"
  gateway.router.admin.rate_limit.burst: "40"
```

Полный пример compose — в [`examples/docker-compose.labels.yml`](examples/docker-compose.labels.yml).
Пошаговый перевод сервиса со статики на labels — в
[`docs/service-discovery-migration.md`](docs/service-discovery-migration.md).

### Podman

Podman отдаёт Docker-совместимый REST API, поэтому используется тот же клиент:

```yaml
discovery:
  provider: podman
  host: "unix:///run/podman/podman.sock"              # rootful
  # host: "unix:///run/user/1000/podman/podman.sock"  # rootless
```

`podman-compose` кладёт имя сервиса в `io.podman.compose.service` — он уже в
`service_name_labels` по умолчанию.

### Безопасность

Socket монтируется **read-only**, клиент делает только `GET` (список контейнеров
и события). Но read-only socket всё равно даёт широкий доступ к Docker daemon;
для чувствительных окружений используйте socket-proxy или удалённый API по TLS.
В labels не храните секреты — они видны через `docker inspect`.

## Docker

```bash
docker build -t api-gateway .
docker run -p 8080:8080 \
  -v $(pwd)/config.local.yaml:/etc/proxy/config.yaml \
  api-gateway
```

## Разработка

```bash
# Установить зависимости
go mod download

# Запуск с локальным конфигом
go run ./cmd/ -config config.local.yaml

# Линт
make lint

# Тесты
make test
```

## Лицензия

MIT
