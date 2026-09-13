# Бенчмарк API-шлюзов, сентябрь 2026

Сравнение `api-gateway` с Traefik, Nginx, Envoy и Kong: накладные расходы на
проксирование, стоимость аутентификации, цена встроенных функций `api-gateway`,
режимы доставки вебхуков и задержка Docker service discovery.

Пакет фиксирует один прогон на одном железе и одном наборе образов. Это
историческое свидетельство: не меняйте файлы этого каталога — для новой
кампании создавайте новый датированный каталог.

- Канонические числа: [`results/summary.csv`](results/summary.csv)
- Конфигурации: [`configs/`](configs)
- Скрипты воспроизведения: [`scripts/`](scripts)
- Вспомогательные сервисы: [`helpers/`](helpers)

## Краткое резюме

- **Проксирование.** На 300 соединениях `api-gateway` держит **24 068 req/s** —
  на **11.6 %** ниже Nginx (27 216 req/s) и на 3–5 % ниже Traefik (24 811),
  Envoy (25 272) и Kong (25 322). На 50 соединениях разрыв с Nginx — 6.1 %.
- **Память.** Пик `api-gateway` под c300 — **121.5 MiB**: больше Nginx (15.8) и
  Envoy (29.1), но заметно меньше Traefik (174.8) и Kong (314.1).
- **Встроенный JWT.** Стоит `api-gateway` **15.1 %** пропускной способности
  (24 304 → 20 641 req/s). Envoy `jwt_authn` — 19.7 %, Kong JWT-плагин — 28.9 %.
- **Внешняя синхронная аутентификация.** Nginx `auth_request` — **−89.5 %**,
  Traefik `forwardAuth` — **−93.8 %**: один сетевой хоп на каждый запрос
  обрушивает throughput на порядок.
- **Функции `api-gateway`.** RBAC, кэш прав и batched HTTP-вебхуки практически
  бесплатны (≤ 2.7 %); прямой HTTP-вебхук на запрос стоит 68.6 %, отключённый
  кэш прав — 93.3 %.
- **Discovery.** Реакция `api-gateway` симметрична: появление ~720 мс, исчезновение
  ~650 мс. Traefik быстрее подхватывает (~310 мс), но убирает маршрут намного
  дольше (~2210 мс).

## Область измерений

- Тест **proxy-only**: один маршрут `/ → backend`, без TLS и без дополнительного
  middleware сверх встроенных значений по умолчанию. Аутентификация, RBAC,
  rate limiting, permissions и вебхуки в базовом сценарии выключены, чтобы
  сравнивать «голый» обратный прокси.
- Отдельные сценарии измеряют **стоимость аутентификации** (у каждого шлюза свой
  no-auth baseline) и **инкрементальную цену функций** `api-gateway`.
- Backend отвечает мгновенно (~200 байт, `traefik/whoami`), поэтому числа
  показывают накладные расходы самого шлюза, а не реальной сети или приложения.
- Discovery — управляющий слой: на горячем пути проксирования он не работает и
  пропускную способность/память не меняет.

## Окружение

| Параметр | Значение |
|---|---|
| Хост | Linux, 12 vCPU, 15 GiB RAM |
| Ядро | 6.8.0-40-generic |
| Docker | 28.3.2 |
| Нагрузочный клиент | `wrk` 4.1.0 (debian/4.1.0-4build2, epoll) |
| Go (сборка) | go1.25.11 linux/amd64 |
| Backend | `traefik/whoami:latest` |
| `api-gateway` | сборка из коммита `6963f00` (merge `feat/config-hardening`, 2026-09-13) |
| Traefik | `traefik:v3.1` |
| Nginx | `nginx:1.27-alpine` |
| Envoy | `envoyproxy/envoy:v1.31-latest`, `--concurrency 2` |
| Kong | `kong:3.7`, DB-less |

## Методология

- Каждый шлюз запускается **по одному** в отдельном контейнере с
  `--cpuset-cpus=0-1 --memory=768m` (жёстко 2 ядра и лимит памяти).
- Backend закреплён на ядрах `8-11`, нагрузочный клиент — на `2-7`; пересечения
  с ядрами шлюза нет.
- Профиль нагрузки: c50 — `wrk -t2 -c50 -d15s`, c300 — `wrk -t4 -c300 -d20s`.
  Перед каждым сценарием — прогрев 5 с (не измеряется).
- Пропускная способность — **лучший из пяти** прогонов; пик памяти снимается
  параллельно с c300 через `docker stats` каждые 0.4 с и берётся максимум.
- Наблюдаемый разброс между прогонами — примерно **±10 %**; сравнивать стоит
  относительные разрывы, а не абсолютные значения.
- Встроенные логи доступа выключены у всех шлюзов (`access_log off` у Nginx,
  `access_log: []` у Envoy, `KONG_PROXY_ACCESS_LOG=/dev/null` у Kong,
  `accessLog: null` у Traefik, `access_log: false` у `api-gateway`).
- Nginx запускается с `worker_processes 2`, Envoy — с `--concurrency 2`, Kong —
  с `KONG_NGINX_WORKER_PROCESSES=2`: число рабочих процессов соответствует двум
  выделенным ядрам.
- Все проценты считаются от **собственного baseline сценария**, а не от общего
  знаменателя; в таблицах baseline указан явно.

## Проксирование: пропускная способность и память

Сценарий `proxy-only` / `proxy-memory`. Baseline — сам сценарий без функций.
Скрипт: [`scripts/run-throughput.sh`](scripts/run-throughput.sh);
конфиги: [`configs/api-gateway/baseline.yaml`](configs/api-gateway/baseline.yaml),
[`configs/traefik/static.yaml`](configs/traefik/static.yaml) +
[`configs/traefik/dynamic.yaml`](configs/traefik/dynamic.yaml),
[`configs/nginx/baseline.conf`](configs/nginx/baseline.conf),
[`configs/envoy/baseline.yaml`](configs/envoy/baseline.yaml),
[`configs/kong/baseline.yaml`](configs/kong/baseline.yaml).

| Шлюз | c50, req/s | c300, req/s | Пик памяти @c300, MiB |
|---|---:|---:|---:|
| Nginx | 27 135 | **27 216** | **15.8** |
| Kong | 24 988 | 25 322 | 314.1 |
| Envoy | 24 847 | 25 272 | 29.1 |
| Traefik | 24 092 | 24 811 | 174.8 |
| `api-gateway` | 25 493 | 24 068 | 121.5 |

- Разрыв `api-gateway` с Nginx: **11.6 %** на c300 (`(27216 − 24068) / 27216`) и
  **6.1 %** на c50.
- Отставание от Traefik/Envoy/Kong на c300 — в пределах 3.0–5.0 %.
- По памяти `api-gateway` легче Traefik на 30 % и Kong в 2.6 раза, но тяжелее
  Nginx в 7.7 раза и Envoy в 4.2 раза.

## Аутентификация: встроенная против внешней

Сценарий `auth` при c300. У каждого шлюза **свой** no-auth baseline, поэтому
дельты не смешивают разные прогоны. Скрипт:
[`scripts/run-auth.sh`](scripts/run-auth.sh).

| Шлюз | Механизм | No-auth, req/s | С auth, req/s | Δ |
|---|---|---:|---:|---:|
| `api-gateway` | встроенный HS256 JWT | 24 304 | **20 641** | **−15.1 %** |
| Envoy | `jwt_authn` (локальный JWKS) | 25 272 | 20 290 | −19.7 % |
| Kong | плагин `jwt` | 25 793 | 18 331 | −28.9 % |
| Nginx | `auth_request` (внешний хоп) | 27 216 | 2 849 | −89.5 % |
| Traefik | `forwardAuth` (внешний хоп) | 24 811 | 1 541 | −93.8 % |

Конфиги: [`configs/api-gateway/jwt.yaml`](configs/api-gateway/jwt.yaml),
[`configs/envoy/jwt.yaml`](configs/envoy/jwt.yaml),
[`configs/kong/jwt.yaml`](configs/kong/jwt.yaml),
[`configs/nginx/external-auth.conf`](configs/nginx/external-auth.conf),
[`configs/traefik/dynamic-forward-auth.yaml`](configs/traefik/dynamic-forward-auth.yaml).
Внешний auth-сервис — `traefik/whoami` на `:80`, ровно тот ответчик, что
использовался в прогоне.

**Вывод.** Встроенная проверка JWT у `api-gateway` дешевле, чем у Envoy и Kong,
и в разы дешевле внешних синхронных схем Nginx/Traefik. Nginx и Traefik не
имеют встроенной JWT-аутентификации: их результат отражает цену архитектуры с
внешним auth-сервисом, а не слабость самих прокси.

## Инкрементальная цена функций `api-gateway`

Сценарий `feature-cost` при c300. У каждой функции свой baseline (no-auth или
встроенный JWT). Скрипт: [`scripts/run-features.sh`](scripts/run-features.sh).

| Функция | Baseline | Throughput, req/s | Δ |
|---|---|---:|---:|
| RBAC по ролям из JWT | JWT (20 641) | 20 548 | −0.45 % |
| Route rate limit (token bucket) | no-auth (24 304) | 23 710 | −2.4 % |
| Кэш прав (TTL) | JWT (20 641) | 20 653 | ~0 % |
| Права без кэша (запрос на каждый вызов) | JWT (20 641) | 1 391 | −93.3 % |
| HTTP-вебхук на каждый запрос | no-auth (24 304) | 7 630 | −68.6 % |
| HTTP-вебхуки батчами | no-auth (24 304) | 23 638 | −2.7 % |
| Вебхуки в NATS | no-auth (24 304) | 21 680 | −10.8 % |

Конфиги: [`configs/api-gateway/rbac.yaml`](configs/api-gateway/rbac.yaml),
[`configs/api-gateway/ratelimit.yaml`](configs/api-gateway/ratelimit.yaml),
[`configs/api-gateway/permissions-cache.yaml`](configs/api-gateway/permissions-cache.yaml),
[`configs/api-gateway/permissions-no-cache.yaml`](configs/api-gateway/permissions-no-cache.yaml),
[`configs/api-gateway/webhook-direct.yaml`](configs/api-gateway/webhook-direct.yaml),
[`configs/api-gateway/webhook-batch.yaml`](configs/api-gateway/webhook-batch.yaml),
[`configs/api-gateway/webhook-nats.yaml`](configs/api-gateway/webhook-nats.yaml).
Вспомогательные сервисы: [`helpers/auth/main.go`](helpers/auth/main.go) (permission
service) и [`helpers/webhook/main.go`](helpers/webhook/main.go) (sink).

**Вывод.** RBAC и кэш прав не дают измеримой стоимости; лимитер — около 2 %.
Отключение кэша прав превращает внешний вызов в синхронный хоп на каждый запрос
и даёт ту же деградацию на порядок, что и внешняя аутентификация. Кэш —
ключевой архитектурный элемент, а не оптимизация.

## Режимы доставки вебхуков

Выделено из `feature-cost`; baseline — no-auth 24 304 req/s при c300.
Скрипт: [`scripts/run-features.sh`](scripts/run-features.sh).

| Режим | Throughput, req/s | Δ к no-auth |
|---|---:|---:|
| Прямой HTTP POST на каждый запрос | 7 630 | −68.6 % |
| HTTP батчами (очередь 1000 / 100 мс) | 23 638 | −2.7 % |
| Публикация в NATS | 21 680 | −10.8 % |

**Вывод.** Асинхронная очередь с батчингом и backpressure убирает почти всю
цену вебхуков; NATS дешевле прямого HTTP, но дороже батчинга. Прямой HTTP-вызов
на каждый запрос — антипаттерн для горячего пути.

## Задержка service discovery

Сценарий `discovery`. Медиана по 3 циклам create/destroy; `up` — время до
первого `200` на `/pod`, `gone` — до первого `404` после остановки контейнера.
Скрипт: [`scripts/run-discovery.sh`](scripts/run-discovery.sh); конфиги:
[`configs/api-gateway/discovery.yaml`](configs/api-gateway/discovery.yaml),
[`configs/traefik/docker-provider.yaml`](configs/traefik/docker-provider.yaml).

| Шлюз | Up, мс | Gone, мс |
|---|---:|---:|
| `api-gateway` | ~720 | ~650 |
| Traefik | ~310 | ~2210 |

**Вывод.** Traefik быстрее обнаруживает контейнер, но `api-gateway` убирает
исчезнувший маршрут примерно в 3.4 раза быстрее. Для сценариев с частыми
перезапусками реплик важна именно скорость удаления: «залипший» маршрут даёт
ошибки дольше, чем короткое окно до появления нового.

## Сильные стороны конкурентов

- **Nginx** — самый быстрый и самый экономичный по памяти в proxy-only тесте, с
  самой зрелой экосистемой и статикой; его слабое место в этом сравнении —
  отсутствие встроенной JWT-аутентификации, из-за чего внешний `auth_request`
  стоит 89.5 % throughput.
- **Traefik** — быстрее всех обнаруживает новые контейнеры (~310 мс) и даёт
  богатый набор middleware и dashboard; удаление исчезнувшего маршрута занимает
  ~2210 мс, а `forwardAuth` — самый дорогой из измеренных auth-путей.
- **Envoy** — близок к лидерам по throughput, лёгок по памяти и имеет самый
  развитый набор фильтров (rate limit, RBAC/OPA, xDS); встроенный `jwt_authn`
  заметно дешевле внешних схем, но дороже встроенной проверки `api-gateway`.
- **Kong** — большая экосистема плагинов и Admin API, DB-less режим упрощает
  эксплуатацию; плагин `jwt` и большой baseline памяти (314.1 MiB) — плата за
  функциональность.
- **`api-gateway`** — не выигрывает ни одну из «железных» метрик, но даёт
  встроенные JWT, RBAC, кэш прав и вебхуки без внешних сервисов и плагинов при
  памяти ниже Traefik и Kong.

## Оговорки и ограничения

- **Разброс ±10 %.** Числа — из ограниченного числа прогонов на одном железе;
  абсолютные значения нестабильны, сравнивайте относительные разрывы.
- **Best-of-five.** Пропускная способность — лучший из пяти прогонов, поэтому
  это оптимистичная оценка; медиана была бы ниже.
- **Backend с near-zero latency.** Тест измеряет накладные расходы шлюза, а не
  поведение под реальным приложением или сетью.
- **Только proxy-only.** Функциональные сценарии изолированы, но не моделируют
  комбинированную нагрузку (auth + RBAC + вебхуки одновременно).
- **Одно железо, один набор версий.** Результаты зависят от ядра, Docker, образа
  и настроек хоста; не переносите их на другой стенд без повторного прогона.
- **Nginx и Traefik** в auth-сценарии используют внешний синхронный хоп — это
  честное отражение их архитектуры, но не эквивалент встроенной валидации.

## Воспроизведение

Требования: `docker`, `wrk`, `taskset`, `curl`; для функций — `go`.
Скрипты неинтерактивны, проверяют зависимости, за собой убирают только ресурсы с
префиксом бенчмарка.

```bash
cd benchmarks/2026-09-gateway-comparison

# проверка окружения без запуска нагрузки
scripts/run-throughput.sh --preflight

# сценарии (лучший из 5 прогонов, ~15–20 минут на скрипт)
scripts/run-throughput.sh          # proxy-only + память
scripts/run-auth.sh                # аутентификация
scripts/run-features.sh            # функции api-gateway + вебхуки
scripts/run-discovery.sh           # service discovery

# убрать оставшиеся контейнеры/сеть/бинарники бенчмарка
scripts/cleanup.sh
```

Все настройки (префикс, CPU-наборы, лимит памяти, профиль `wrk`, число повторов,
образы) переопределяются через переменные окружения; значения по умолчанию —
точный сентябрьский сетап и описаны в [`scripts/common.sh`](scripts/common.sh).
Скрипты никогда не пишут в [`results/summary.csv`](results/summary.csv): для лога
задайте `RESULTS=/tmp/...`.

## Прослеживаемость

Каждое число в таблицах выше соответствует строке в
[`results/summary.csv`](results/summary.csv) (колонки `scenario`, `gateway`,
`variant`, `concurrency`, `value`, `unit`, `baseline_value`, `delta_percent`).
Производные проценты (разрыв с Nginx, отношения по памяти) вычисляются из этих
строк и помечены в тексте. Файлы конфигураций и вспомогательных сервисов
соответствуют именно тем сценариям, которые указаны в ссылках.
