# Миграция сервиса со статического конфига на labels

Цель — перевести бэкенд с ручных `targets`/`routing.rules` в
`api-gateway-config.yaml` на self-describing labels контейнера, не уронив
маршрутизацию. Миграция безопасна, потому что обнаруженное **дополняет**
статику: пока статический таргет с тем же именем на месте, он выигрывает, и
поведение не меняется.

## Предпосылки

- В конфиге включён `discovery` (см. README, раздел «Service discovery»).
- Контейнеру гейтвея смонтирован `docker.sock` (или `podman.sock`) read-only.
- Известны имя сервиса (обычно `com.docker.compose.service`), порт, пути и
  настройки auth/rate-limit из статического конфига.

## Шаги

1. **Добавить labels сервису в compose**, ничего не удаляя из статики.
   Перенесите поля статического таргета и правил 1:1:

   | Статика | Label |
   |---|---|
   | `targets[].name` | `gateway.name` (или compose-имя сервиса) |
   | `targets[].url` порт | `gateway.port` |
   | `targets[].timeout` | `gateway.timeout` |
   | `targets[].health_check` | `gateway.health` |
   | `routing.rules[].path_prefix` | `gateway.router.<id>.path_prefix` |
   | `routing.rules[].host` | `gateway.router.<id>.host` |
   | `routing.rules[].methods` | `gateway.router.<id>.methods` |
   | `routing.rules[].strip_path` | `gateway.router.<id>.strip_path` |
   | `routing.rules[].auth.required` | `gateway.router.<id>.auth.required` |
   | `routing.rules[].auth.roles` | `gateway.router.<id>.auth.roles` |
   | `routing.rules[].rate_limit.*` | `gateway.router.<id>.rate_limit.*` |

   Пример: [`examples/docker-compose.labels.yml`](../examples/docker-compose.labels.yml).

2. **Задеплоить** сервис (`docker compose up -d <service>`), затем убедиться,
   что гейтвей перечитал конфиг (в логах `Configuration reloaded`) и в логах
   нет предупреждений `discovery:`. Статика пока выигрывает — маршруты не
   изменились.

3. **Сверить** обнаруженный таргет/роуты со статикой: имя, URL (`scheme://name:port`),
   `path_prefix`, `host`, `auth.required`, `strip_path`. При расхождении —
   поправить labels и повторить шаг 2.

4. **Убрать статические** `targets[]` и `routing.rules[]` этого сервиса из
   `api-gateway-config.yaml`.

5. **Перезагрузить** гейтвей (SIGHUP или рестарт) и проверить маршруты
   (публичные и защищённые) — теперь их отдаёт discovery.

6. **Откат**: вернуть статические записи и перезагрузить. Статика снова
   перекроет обнаруженное.

## Чеклист

- [ ] Labels добавлены, статика не тронута
- [ ] Гейтвей перечитал конфиг, `discovery:`-предупреждений нет
- [ ] Имя/URL/пути/auth обнаруженного таргета совпадают со статикой
- [ ] Статические `targets`/`rules` сервиса удалены
- [ ] Публичные и защищённые маршруты проверены после удаления статики
- [ ] Откат-план понятен (вернуть статику + reload)

## Замечания

- **Приоритет статики.** Конфликт таргета — по имени; конфликт правила — по
  паре `(host, path_prefix)`. Обнаруженное правило, чей таргет был пропущен из-за
  коллизии имени, тоже отбрасывается.
- **Реплики.** Таргет = `http://<name>:<port>`; несколько контейнеров одного
  сервиса дают один таргет, балансировку делает DNS Docker/Podman.
- **Порт.** Если `gateway.port` не задан, берётся единственный exposed TCP-порт;
  если портов несколько — контейнер пропускается с предупреждением.
- **Health.** `gateway.health` — путь (`/health`) или полный URL. Health-проверки
  выполняются только при `application.health_check: true`.
