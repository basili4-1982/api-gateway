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

Гейтвей умеет находить бэкенды по labels контейнеров (аналог Docker-провайдера
Traefik). Включается секцией `discovery` в конфиге; socket монтируется read-only.

Маркер: `gateway.enable=true`. Таргет: `gateway.name` (по умолчанию —
`com.docker.compose.service`), `gateway.port`, `gateway.scheme`, `gateway.timeout`,
`gateway.health`, `gateway.weight`.

Роутеры: короткая форма `gateway.path_prefix`/`gateway.host`/`gateway.auth.required`
(роутер `default`) или именованная `gateway.router.<id>.<field>` для нескольких
правил на сервис. Поля: `host`, `path_prefix`, `methods`, `strip_path`,
`auth.required`, `auth.roles`, `auth.strip_token`, `rate_limit.rps`,
`rate_limit.burst`.

Пример:

    labels:
      gateway.enable: "true"
      gateway.name: "blog"
      gateway.port: "8085"
      gateway.router.api.path_prefix: "/api/blog"
      gateway.router.api.auth.required: "false"
      gateway.router.admin.path_prefix: "/api/admin/blog"
      gateway.router.admin.auth.required: "true"
      gateway.router.admin.auth.roles: "admin"

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
