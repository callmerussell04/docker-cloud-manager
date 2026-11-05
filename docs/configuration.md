# Справочник конфигурации

DCM использует два уровня настроек:

1. **Начальные переменные окружения** — секреты, строки подключения к базам данных, адреса служб, порты, пути и настройки браузера или OIDC. После изменения требуется перезапуск, а после изменения аргументов сборки клиентской части — повторная сборка.
2. **Настройки, изменяемые во время работы** — рабочие ограничения Core. Хранятся в `core_config_data`, изменяются администратором и применяются без общего перезапуска.

Публичный шаблон начальных переменных: [`backend/.env.example`](../backend/.env.example).

## Обозначения

| Пометка | Значение |
| --- | --- |
| Secret | Значение нельзя логировать или публиковать |
| Restart | Перезапустите соответствующие services |
| Rebuild | Пересоберите frontend/container image |
| Runtime | Меняется через System settings или `/api/v1/admin/config` |

## Bootstrap env

### PostgreSQL и bootstrap admin

| Переменная | Значение в шаблоне | Назначение | Применение |
| --- | --- | --- | --- |
| `POSTGRES_USER` | `postgres` | Пользователь PostgreSQL | Restart |
| `POSTGRES_PASSWORD` | `change_me_*` | Общий пароль PostgreSQL | Secret; менять только до инициализации либо по плану смены пароля |
| `CORE_POSTGRES_DB` | `dcm_core_db` | База Core | Restart |
| `SSO_POSTGRES_DB` | `dcm_sso_db` | База SSO | Restart |
| `JWT_SECRET` | `change_me_*` | Подпись токенов DCM tokens | Secret |
| `INTERNAL_SERVICE_TOKEN` | `change_me_*` | Доверие между службами DCM services | Secret, restart всех серверных служб |
| `SSO_ADMIN_USERNAME` | `admin` | Bootstrap admin login | SSO restart; применяется при начальной настройке |
| `SSO_ADMIN_EMAIL` | `admin@example.local` | Bootstrap admin email | SSO restart |
| `SSO_ADMIN_PASSWORD` | `change_me_*` | Bootstrap admin password | Secret, SSO restart |

### Пулы соединений PostgreSQL

Настройки задаются отдельно для Core и SSO и требуют перезапуска службы:

| Переменные | По умолчанию | Назначение |
| --- | --- | --- |
| `CORE_DB_MAX_OPEN`, `SSO_DB_MAX_OPEN` | `20` | Общий предел открытых соединений службы |
| `CORE_DB_MAX_LIFETIME`, `SSO_DB_MAX_LIFETIME` | `30m` | Максимальное время жизни соединения |
| `CORE_DB_MAX_IDLE_TIME`, `SSO_DB_MAX_IDLE_TIME` | `5m` | Максимальное время простоя соединения |

Core резервирует одно соединение из `MAX_OPEN` для advisory locks (admission и Registry); остальные доступны запросам и workers, в том числе во время удержания блокировки. Ожидающие блокировки и записи контейнеров с owner/capacity lock используют отдельный пул locks. Минимальный общий бюджет Core — `2`, SSO — `1`. Простаивающие соединения закрываются по истечении `MAX_IDLE_TIME`. Длительности задаются в формате Go (`30m`, `1h`); нулевые, отрицательные и некорректные значения запрещены. Неограниченные пулы не поддерживаются. Бюджеты не входят в runtime config.

Закреплённые транзакции Registry cleanup/delete также используют пул locks; короткий ClaimBuild проверяет Registry lock без ожидания в пуле queries.

Блокировки admission привязаны к транзакции и освобождаются при завершении операции или отмене контекста. При выборе бюджета PostgreSQL учитывайте сумму лимитов всех подключённых служб и отдельные подключения миграций, Keycloak и администраторов.

Core и SSO пишут `postgres pool statistics` при запуске и каждые 30 секунд в существующий структурированный журнал. Поля `pool`, `max_open`, `open`, `in_use`, `idle`, `wait_count`, `wait_duration_ms`, `max_idle_closed`, `max_idle_time_closed`, `max_lifetime_closed` показывают заполнение пула, накопленное ожидание и закрытие соединений. Core сообщает отдельно `queries` и `locks`; для общего числа соединений сложите их значения. DSN и учётные данные в статистику не входят.

Core проверяет обязательные зависимости build admission при создании службы: репозитории сборок, образов, томов, контейнеров и staged objects, пользователей, runtime config и метрики RAM/CPU/диска. Отсутствующая зависимость (включая typed nil) приводит к ошибке запуска; блокировка capacity, счётчики очереди и резервы ресурсов не отключаются из-за неполного адаптера. Object storage по-прежнему подключается лениво; его недоступность не препятствует запуску платформы.

Служба проектов также проверяется при создании: репозитории проектов (включая выборку проектов для lifecycle coordinator) и ресурсов проекта, Docker, lifecycle контейнеров и томов, capacity checker и runtime config обязательны; отсутствующая зависимость, включая typed nil, приводит к ошибке запуска. Запуск проекта всегда выполняет preflight RAM/CPU для остановленных и завершившихся контейнеров и не пропускает его из-за неполного адаптера. Очистка пользовательской сети и отмена Compose-развёртывания остаются необязательными.

Координатор Compose-развёртываний проверяется при создании отдельно от приёма исходников: хранилище состояния развёртываний, сборки и клиент сборок, контейнеры и список контейнеров проекта, тома, Docker, очистка образов, освобождение staged-исходника и runtime config обязательны; отсутствующая зависимость, включая typed nil, приводит к ошибке запуска. Ожидание готовности управляемых томов и повторное использование уже созданных контейнеров при возобновлении не отключаются из-за неполного адаптера. Аудит публикации сервисов остаётся необязательным.

### Локальная аутентификация и OIDC

| Переменная | Значение по умолчанию | Назначение |
| --- | --- | --- |
| `SSO_LOCAL_LOGIN_ENABLED` | `true` | Показывать и принимать локальный вход |
| `SSO_LOCAL_REGISTER_ENABLED` | `true` | Разрешить самостоятельную регистрацию |
| `SSO_OIDC_ENABLED` | `false` | Включить поток входа через Keycloak |
| `SSO_OIDC_ISSUER_URL` | Область Keycloak на localhost | URL издателя и метаданных OIDC |
| `SSO_OIDC_CLIENT_ID` | `dcm` | Идентификатор клиента OIDC |
| `SSO_OIDC_CLIENT_SECRET` | `change_me_*` | Секрет конфиденциального клиента |
| `SSO_OIDC_REDIRECT_URL` | Обратный вызов на localhost | Точный URL обратного вызова Gateway |
| `SSO_OIDC_ADMIN_GROUPS` | `/dcm-admins,dcm-admin` | Группы или роли, назначающие администратора |
| `SSO_OIDC_DEFAULT_ROLE` | `user` | Роль нового пользователя OIDC без сопоставления с администратором |

Все изменения требуют перезапуска SSO; изменения браузерной маршрутизации также требуют перезапуска Gateway.

### Optional development Keycloak

| Переменная | Значение по умолчанию | Назначение |
| --- | --- | --- |
| `KEYCLOAK_POSTGRES_USER` | `keycloak` | Пользователь Keycloak DB |
| `KEYCLOAK_POSTGRES_PASSWORD` | `change_me_*` | Secret Keycloak DB |
| `KEYCLOAK_POSTGRES_DB` | `keycloak` | Имя Keycloak DB |
| `KEYCLOAK_ADMIN_USERNAME` | `admin` | Keycloak admin console login |
| `KEYCLOAK_ADMIN_PASSWORD` | `change_me_*` | Secret Keycloak admin |
| `KEYCLOAK_HOSTNAME` | `http://keycloak.localhost` | Canonical Keycloak URL |

Эти значения используются только с `backend/docker-compose.keycloak.yml`.

### RabbitMQ, MinIO, Registry и ClickHouse

| Переменная | Значение по умолчанию | Назначение |
| --- | --- | --- |
| `RABBITMQ_USER` | `dcm` | Пользователь брокера сообщений |
| `RABBITMQ_PASSWORD` | `change_me_*` | Секрет брокера сообщений |
| `MINIO_ROOT_USER` | `dcm_minio` | Начальный пользователь MinIO |
| `MINIO_ROOT_PASSWORD` | `change_me_*` | Начальный секрет MinIO |
| `OBJECT_STORAGE_ENDPOINT` | `minio:9000` | Адрес S3-совместимого хранилища |
| `OBJECT_STORAGE_BUCKET` | `dcm-builds` | Бакет архивов и журналов |
| `OBJECT_STORAGE_ACCESS_KEY` | `dcm_minio` | Ключ доступа S3 |
| `OBJECT_STORAGE_SECRET_KEY` | `change_me_*` | Секретный ключ S3 |
| `OBJECT_STORAGE_USE_SSL` | `false` | TLS между Core, Builder и объектным хранилищем |
| `REGISTRY_API_URL` | `https://registry-backend:5000` | Адрес API реестра образов для Core |
| `REGISTRY_URL` | `registry:5000` | Адрес реестра образов для Builder |
| `REGISTRY_PUBLIC_URL` | `registry:5000` | Адрес реестра, с которого сервер Docker загружает образы |
| `CLICKHOUSE_DB` | `dcm_reports` | База данных отчётов |
| `CLICKHOUSE_USER` | `dcm_reports` | Пользователь базы данных отчётов |
| `CLICKHOUSE_PASSWORD` | `change_me_*` | Секрет базы данных отчётов |

### Domains, frontend и Traefik

| Переменная | Значение по умолчанию | Назначение | Применение |
| --- | --- | --- | --- |
| `DCM_HOST` | `localhost` | Имя сервера панели и API | Перезапуск маршрутов Traefik |
| `BASE_DOMAIN` | `localhost` | Суффикс доменов опубликованных приложений | Перезапуск Core и пересборка клиентской части |
| `FRONTEND_API_URL` | `http://localhost/api/v1` | URL API, встроенный в одностраничное приложение | Пересборка клиентской части |
| `TRAEFIK_DASHBOARD_ENABLED` | `false` | Локальная панель на `127.0.0.1:8080` | Перезапуск Traefik |
| `GATEWAY_INGRESS_SUBNET` | `172.30.251.0/24` | Отдельная внутренняя сеть Traefik–Gateway | Повторное создание сети и контейнеров |
| `TRAEFIK_GATEWAY_IP` | `172.30.251.2` | Адрес Traefik и доверенный прокси Gateway в Compose | Повторное создание Traefik и Gateway |
| `TRAEFIK_LOG_LEVEL` | `INFO` | Уровень журналирования Traefik | Перезапуск Traefik |
| `COMPOSE_LOG_MAX_SIZE` | `10m` | Размер файла журнала Docker до ротации | Повторное создание контейнеров |
| `COMPOSE_LOG_MAX_FILE` | `3` | Количество файлов журнала | Повторное создание контейнеров |

`TRAEFIK_ACME_EMAIL` и `TRAEFIK_ACME_STORAGE` оставлены как задел на будущее и не включают HTTPS в текущем Compose.

### Gateway и файлы cookie

Gateway доверяет `X-Forwarded-For` только от Traefik на отдельной внутренней сети `dcm_gateway_ingress`. Compose использует `TRAEFIK_GATEWAY_IP` (по умолчанию `172.30.251.2`) и для статического адреса Traefik, и для `GATEWAY_TRUSTED_PROXIES`. При конфликте подсетей измените `GATEWAY_INGRESS_SUBNET` и выберите свободный адрес Traefik внутри неё. Не подключайте пользовательские контейнеры к этой сети. Traefik не доверяет входящим forwarded-заголовкам внешних клиентов.

При запуске Gateway вне Compose задайте `GATEWAY_TRUSTED_PROXIES` списком IP/CIDR реальных прокси через запятую; пустой список отключает доверие. Некорректные адреса, неопределённые адреса, multicast и сети, содержащие неопределённый адрес (включая `/0`), отклоняются при запуске без вывода значения настройки. Не доверяйте всей сети пользовательских контейнеров или внешних клиентов. Лимиты аутентификации и загрузок, а также аудит используют один и тот же проверенный адрес клиента.

| Группа | Переменные | Назначение |
| --- | --- | --- |
| Источники | `CORS_ALLOWED_ORIGINS`, `GATEWAY_AUTH_REDIRECT_ALLOWED_ORIGINS`, `GATEWAY_AUTH_CALLBACK_PATH` | Разрешённые источники браузерных страниц и обратный вызов одностраничного приложения |
| Доверие к прокси | `GATEWAY_TRUSTED_PROXIES` | Явный список доверенных обратных прокси-серверов |
| Ограничения тела и частоты запросов | `GATEWAY_MAX_JSON_BODY_BYTES`, `GATEWAY_AUTH_RATE_LIMIT_REQUESTS`, `GATEWAY_AUTH_RATE_LIMIT_WINDOW_SECONDS`, `GATEWAY_UPLOAD_RATE_LIMIT_REQUESTS`, `GATEWAY_UPLOAD_RATE_LIMIT_WINDOW_SECONDS` | Ограничения на внешней границе API |
| Время ожидания HTTP | `GATEWAY_READ_HEADER_TIMEOUT_SECONDS`, `GATEWAY_READ_TIMEOUT_SECONDS`, `GATEWAY_WRITE_TIMEOUT_SECONDS`, `GATEWAY_IDLE_TIMEOUT_SECONDS`, `GATEWAY_SHUTDOWN_TIMEOUT_SECONDS`, `GATEWAY_MAX_HEADER_BYTES` | Ограничения публичного HTTP-сервера |
| Передача через прокси | `GATEWAY_PROXY_DIAL_TIMEOUT_SECONDS`, `GATEWAY_PROXY_TLS_HANDSHAKE_TIMEOUT_SECONDS`, `GATEWAY_PROXY_RESPONSE_HEADER_TIMEOUT_SECONDS`, `GATEWAY_PROXY_IDLE_CONN_TIMEOUT_SECONDS`, `GATEWAY_PROXY_EXPECT_CONTINUE_TIMEOUT_SECONDS`, `GATEWAY_PROXY_MAX_IDLE_CONNS`, `GATEWAY_PROXY_MAX_IDLE_CONNS_PER_HOST` | Передача HTTP-запросов к внутренним службам |
| gRPC | `GATEWAY_GRPC_REQUEST_TIMEOUT_SECONDS`, `GATEWAY_GRPC_MIN_CONNECT_TIMEOUT_SECONDS`, `GATEWAY_GRPC_TLS_ENABLED`, `GATEWAY_GRPC_TLS_CA_FILE`, `GATEWAY_GRPC_TLS_CERT_FILE`, `GATEWAY_GRPC_TLS_KEY_FILE`, `GATEWAY_GRPC_TLS_SERVER_NAME` | Соединения gRPC от Gateway к внутренним службам |
| Состояние и потоки | `GATEWAY_READINESS_TIMEOUT_SECONDS`, `GATEWAY_TELEMETRY_TICKET_TTL_SECONDS` | Время ожидания проверки готовности и срок действия билета доступа |
| Файл cookie | `COOKIE_NAME`, `COOKIE_PATH`, `COOKIE_DOMAIN`, `COOKIE_SECURE`, `COOKIE_HTTP_ONLY`, `COOKIE_SAMESITE` | Атрибуты файла cookie с токеном обновления |

Срок refresh-cookie вычисляется из срока сессии SSO (30 дней всего, 7 дней без обновления); `COOKIE_MAX_AGE_SECONDS` удалён. Для cookie-запросов refresh/logout обязателен разрешённый `Origin` или `Referer`; список берётся из `CORS_ALLOWED_ORIGINS`.

Пустое значение `GATEWAY_TRUSTED_PROXIES` безопаснее неизвестного широкого диапазона CIDR. Переменные с путями к файлам TLS используются только при `GATEWAY_GRPC_TLS_ENABLED=true`.

### Начальные ограничения Telemetry

| Переменная | Значение в шаблоне | Назначение |
| --- | --- | --- |
| `TELEMETRY_MAX_GLOBAL_LOG_STREAMS` | `100` | Глобальный предел SSE logs |
| `TELEMETRY_MAX_GLOBAL_TERMINAL_SESSIONS` | `50` | Глобальный предел terminal sessions |
| `TELEMETRY_READ_HEADER_TIMEOUT_SECONDS` | `5` | HTTP header timeout |
| `TELEMETRY_IDLE_TIMEOUT_SECONDS` | `120` | HTTP idle timeout |
| `TELEMETRY_SHUTDOWN_TIMEOUT_SECONDS` | `15` | Graceful shutdown timeout |
| `TELEMETRY_MAX_HEADER_BYTES` | `1048576` | Header size limit |
| `TELEMETRY_GRPC_REQUEST_TIMEOUT_SECONDS` | `10` | Core RPC timeout |
| `TELEMETRY_GRPC_MIN_CONNECT_TIMEOUT_SECONDS` | `5` | gRPC connect timeout |

### Начальные безопасные значения ресурсов из `.env.example`

Следующие переменные окружения используются при создании рабочих настроек Core: `DEFAULT_CPU_RESERVATION_MILLICORES`, `RESERVED_SYSTEM_CPU_MILLICORES`, `CPU_OVERCOMMIT_FACTOR`, `MAX_CPU_BURST_MULTIPLIER`, `CONTAINER_CPU_PERIOD`, `IMAGE_BUILDS_ENABLED`, `BUILD_CANCEL_POLL_INTERVAL_SECONDS`, `REPORTS_USAGE_SNAPSHOT_INTERVAL_SECONDS`, `MAX_STAGED_SOURCE_BYTES_PER_USER`, `MAX_QUEUED_BUILDS_PER_USER`, `CONTAINER_CREATE_WORKER_COUNT`, `CONTAINER_CREATE_MAX_ATTEMPTS`, `CONTAINER_CREATE_TIMEOUT_MINUTES`, `MAX_QUEUED_CONTAINER_CREATES_PER_USER`, `CONTAINER_CREATE_OUTBOX_INTERVAL_SECONDS`, `CONTAINER_CREATE_OUTBOX_BATCH_SIZE`, `MAX_QUEUED_COMPOSE_DEPLOYS_PER_USER`, `COMPOSE_COORDINATOR_INTERVAL_SECONDS`, `HOST_MIN_FREE_DISK_BYTES`, `GIT_SOURCES_ENABLED`, `GIT_ALLOWED_HOSTS`, `GIT_CLONE_TIMEOUT_SECONDS`, `GIT_MAX_REPOSITORY_BYTES`.

`CORE_HOST_DISK_PATH=/host` указывает путь подключения, по которому Core измеряет свободное место. Необязательная переменная `BUILDER_INSTANCE_ID` позволяет задать постоянный идентификатор обработчика сборок.

После первого запуска runtime JSON становится источником рабочих значений; изменение исходного env не обязано перезаписать уже сохранённый config.

## Runtime config

Эти настройки читаются и изменяются через административную панель или `/api/v1/admin/config`. При обновлении отправляется полный объект, поэтому клиент API должен сначала получить текущую конфигурацию, изменить нужные поля и вернуть все остальные без потери.

### Контейнеры и ресурсы сервера

| Поля | Значение при первом запуске | Ограничение или назначение |
| --- | --- | --- |
| `base_domain` | `BASE_DOMAIN` | Непустой домен без пробелов и символов пути |
| `default_memory_reservation_bytes` | 256 MiB | `> 0` |
| `reserved_system_memory_bytes` | 2 GiB | `>= 0` |
| `overcommit_factor` | `1.5` | `> 0`, `<= 10` |
| `max_burst_multiplier` | `4` | `1..10` |
| `default_cpu_reservation_millicores` | `250` | `> 0` |
| `reserved_system_cpu_millicores` | `500` | `>= 0` |
| `cpu_overcommit_factor` | `4` | `> 0`, `<= 10` |
| `max_cpu_burst_multiplier` | `4` | `1..10` |
| `container_cpu_period` | `100000` | `> 0` |
| `default_cpu_shares`, `high_load_cpu_shares` | `1024`, `512` | `> 0` |
| `high_load_container_count` | `5` | `> 0` |
| `container_stop_timeout` | `10` секунд | `> 0` |
| `container_ttl_hours` | `24` | `0` отключает TTL, отрицательные запрещены |
| `container_pids_limit` | `256` | `> 0` |
| `container_memory_swap_multiplier` | `2` | `1..10` |
| `max_log_size`, `max_log_files` | `10m`, `3` | Размер Docker и положительное количество файлов |
| `container_disk_quota` | `1G` | Формат размера Docker |
| `max_volumes_per_user`, `max_containers_per_user` | `5`, `10` | `> 0` |
| `reserved_domain_prefixes` | системные имена | Допустимые уникальные префиксы |
| `blocked_domain_prefix_patterns` | пусто | Уникальные допустимые регулярные выражения |

### Сборки, Git и безопасность

| Поля | Значение по умолчанию | Назначение |
| --- | --- | --- |
| `image_builds_enabled` | `true` | Глобально включает сборки |
| `build_memory_bytes`, `build_cpu_quota`, `build_cpu_period` | 512 MiB, `100000`, `100000` | Резервы и ограничения сборки |
| `build_memory_swap_multiplier`, `build_pids_limit` | `2`, `512` | Изоляция среды выполнения |
| `kaniko_image` | последняя версия Kaniko | Образ обработчика сборок |
| `max_build_time_minutes`, `max_concurrent_builds` | `10`, `2` | Бюджет выполнения после захвата задачи и количество одновременных сборок |
| `max_upload_size_bytes`, `max_archive_size_bytes`, `max_unpacked_size_bytes` | 50 MiB, 50 MiB, 500 MiB | Ограничения размера сборки |
| `max_build_log_size_bytes` | 5 MiB | Лимит исходного вывода и отдельно сохранённого журнала (включая системные сообщения) |
| `max_staged_source_bytes_per_user`, `max_queued_builds_per_user` | 1 GiB, `10` | Ограничения для одного пользователя |
| `host_min_free_disk_bytes` | 1 GiB | Минимальный прогнозируемый остаток места на диске |
| `git_sources_enabled` | `true` | Включает источники Git |
| `git_allowed_hosts` | GitHub/GitLab/Bitbucket | Точный список разрешённых имён серверов |
| `git_clone_timeout_seconds`, `git_max_repository_bytes` | `60`, 200 MiB | Ограничения клонирования |

`max_build_log_size_bytes` ограничивает поток до сборки строк и редактирования секретов; скрытый или сокращённый вывод также расходует этот бюджет. Builder читает не более лимита плюс один проверочный байт. Строки без перевода строки используют память, ограниченную этим бюджетом; строка, превышающая оставшийся бюджет, не сохраняется частично. Отдельно ограничивается число записанных байтов: маркеры редактирования и системные сообщения не могут увеличить локальный журнал сверх лимита. Превышение любого лимита останавливает Kaniko и завершает сборку как `failed`. Ошибки чтения, записи, короткая запись и ошибка закрытия журнала дают `failed_internal`; ошибка загрузки журнала в объектное хранилище также не позволяет завершить сборку как `success`. Явная отмена и тайм-аут сохраняют свои конечные состояния.

Ожидание в очереди не расходует `max_build_time_minutes`: Builder начинает отсчёт после успешного захвата задачи Core. В базе `created_at` фиксирует постановку в очередь, а `started_at` сохраняется атомарно с переходом `pending -> running` (включая выдачу разрешения Registry). До захвата `started_at` отсутствует; API возвращает `0`, интерфейс показывает прочерк. Длительность в интерфейсе считается только от начала выполнения. Списки сборок упорядочены по времени создания.

Сборки `pending` не имеют срока истечения по возрасту: при недоступности Builder они ожидают восстановления или явной отмены. Число задач ограничивают квоты очереди; резервы RAM/CPU и исходных файлов сохраняются до терминального состояния. `stale_build_timeout_minutes` (по умолчанию 30 минут) — отдельный порог восстановления зависших `running` задач, отсчитываемый от `started_at`, а не `created_at`. GC периодически завершает такие задачи как `failed_timeout`, сохраняя защиту отменённых и других терминальных состояний и освобождая резервы обычным путём завершения. Этот порог следует задавать больше `max_build_time_minutes`, оставляя запас для завершения и доставки результата. Он также остаётся возрастным порогом очистки осиротевших исходных файлов; источники активных `pending/running` задач исключены из такой очистки.

Builder сохраняет результат выполнения в `BUILD_STORAGE_PATH/results` перед отправкой завершения в Core. Каталог должен переживать перезапуск; Compose использует том `builder_storage`. Все обработчики одной очереди должны иметь доступ к одному журналу результатов. При временной недоступности Core повторная доставка продолжает завершение без повторного запуска Kaniko. До подтверждения Core сохраняются журнал результата и локальный лог; после подтверждения удаляются временные файлы и архив, а Core освобождает резерв источника. Доставка для ещё выполняющейся сборки без сохранённого результата остаётся в очереди.

### Фоновые обработчики, Compose, отчёты и телеметрия

- Обработчики сборок: `build_cancel_poll_interval_seconds`, `build_outbox_interval_seconds`, `build_outbox_batch_size`, `stale_build_timeout_minutes`.
- Жизненный цикл контейнеров: `container_create_worker_count`, `container_create_max_attempts`, `container_create_timeout_minutes`, `max_queued_container_creates_per_user`, `container_create_outbox_interval_seconds`, `container_create_outbox_batch_size`.
- Сверка состояния и очистка: `ttl_worker_interval_seconds`, `gc_worker_interval_minutes`, `event_sync_interval_seconds`, `event_reconnect_delay_seconds`.
- Учёт занятости томов: `volume_usage_interval_seconds` (по умолчанию `60`), `volume_usage_cycle_budget_seconds` (по умолчанию `15`), `volume_usage_scan_entry_budget` (по умолчанию `50000`, допустимый диапазон `1..10000000`).
- Очистка Registry: `registry_cleanup_interval_seconds` (по умолчанию `5`) и `registry_cleanup_job_timeout_seconds` (по умолчанию `20`); оба значения должны быть положительными.
- Compose: `compose_upload_max_bytes`, `compose_pipeline_timeout_minutes`, `compose_deploy_worker_count`, `compose_outbox_interval_seconds`, `compose_outbox_batch_size`, `compose_deploy_max_attempts`, `max_queued_compose_deploys_per_user`, `compose_build_poll_interval_seconds`, `compose_dependency_wait_timeout_minutes`, `compose_dependency_poll_interval_seconds`, `compose_coordinator_interval_seconds`. Параметры `compose_dependency_wait_timeout_minutes` и `compose_dependency_poll_interval_seconds` сейчас ни на что не влияют: координатор проверяет условия `depends_on` на каждом такте `compose_coordinator_interval_seconds` и не ограничивает ожидание зависимости по времени.
- Отчёты: `reports_usage_snapshot_interval_seconds`, допустимый диапазон `60..86400` секунд, значение по умолчанию `300`.
- Телеметрия: `telemetry_max_log_tail_lines`, `telemetry_max_log_streams_per_user`, `telemetry_max_terminal_sessions_per_user`, `telemetry_terminal_idle_timeout_seconds`, `telemetry_terminal_max_duration_seconds`, `telemetry_allowed_exec_commands`, `telemetry_max_command_args`, `telemetry_max_command_arg_bytes`, `telemetry_ws_read_limit_bytes`.

Core выделяет отдельный отменяемый контекст каждой подписке Docker Events. Ошибка потока или закрытие любого из его каналов отменяет предыдущую подписку до повторного подключения через `event_reconnect_delay_seconds`, даже если канал сообщений Docker SDK остаётся открытым. Периодическая сверка через `event_sync_interval_seconds` продолжается при разрывах; остановка приложения отменяет активную подписку и ожидание переподключения.

Учёт занятости томов выполняется отдельно от сверки Docker Events: цикл запускается через `volume_usage_interval_seconds` после окончания предыдущего и сканирует файлы томов не дольше `volume_usage_cycle_budget_seconds`. За цикл в одном томе обрабатывается не больше `volume_usage_scan_entry_budget` файлов и каталогов. Тома обходятся по кругу; том, прерванный ограничением времени после других томов, начинает следующий цикл и получает весь лимит, а том, прерванный в начале цикла, пропускает вперёд остальные, поэтому ни один том не задерживается бесконечно. Незавершённое сканирование продолжается в следующем цикле с места остановки; после перезапуска Core оно начинается заново.

Занятое место тома (`used_bytes`, `usage_observed_at`) обновляется только по завершённому сканированию; сканирование, растянутое на несколько циклов, не является снимком одного момента. Пока измерение не опубликовано, проверка дисковой квоты обработчиком учитывает большее из последнего измерения и уже подсчитанного объёма; файлы, удалённые после подсчёта, могут временно завышать это значение. Проверки при создании и запуске ресурсов используют последнее опубликованное значение, а превышение, найденное при сканировании, обработчик устраняет остановкой контейнеров с томами на запись.

Ограничения: стоимость сканирования растёт с числом файлов и каталогов, а не с объёмом данных — при прогретом кэше порядка 3–5 мкс на запись (50000 записей за 0,15–0,4 с), при холодном диске значительно больше. Каталог читается целиком и не прерывается посреди чтения, поэтому цикл может превысить лимит на время чтения одного каталога, а `volume_usage_cycle_budget_seconds` должен превышать время чтения самого большого каталога тома. Каждое продолжение заново читает каталоги на пути к месту остановки: каталог со 100000 файлов при лимите 50000 обходится примерно в 1,5 раза дороже, и стоимость растёт пропорционально размеру каталога, делённому на лимит.

Все поля количества, интервала, времени ожидания и размера должны быть положительными, кроме явно разрешённых нулевых ограничений. Разрешённые серверы, команды и префиксы должны быть непустыми и уникальными.

[Аутентификация](authentication.md) · [Руководство администратора](admin-guide.md) · [К оглавлению](README.md)

## Авторизация Registry

Registry использует native Distribution token auth (`registry:2.8.3`, digest-pinned in `docker-compose.yml`) на всех адресах. Core предоставляет отдельный TLS listener `:5000`: только `/token` и `/v2/`. Не публикуйте прямой порт контейнера Registry. Его TLS listener находится в отдельной внутренней сети `registry_net`; API продолжает проверять подпись и права даже при прямом обращении.

Bootstrap-параметры (секреты не входят в runtime config):

| Переменная | Назначение |
| --- | --- |
| `REGISTRY_URL` | Канонический адрес для Kaniko и token realm, по умолчанию `registry:5000` |
| `REGISTRY_PUBLIC_URL` | Адрес для Docker daemon, в штатной конфигурации также `registry:5000` |
| `REGISTRY_BACKEND_URL` | Внутренний HTTPS backend, Compose задаёт `https://registry-backend:5000` |
| `REGISTRY_CERTS_DIR` | Абсолютный каталог сертификатов на сервере |
| `REGISTRY_CA_FILE` | CA для соединений Core/Builder, в Compose `/run/dcm-registry/ca.crt` |
| `REGISTRY_TLS_CERT_FILE`, `REGISTRY_TLS_KEY_FILE` | TLS identity отдельного listener Core |
| `REGISTRY_SIGNING_CERT_FILE`, `REGISTRY_SIGNING_KEY_FILE` | Отдельная RSA signing identity Core (минимум 2048 бит) |
| `REGISTRY_TOKEN_ISSUER` | Issuer, одинаковый у Core и Registry; `dcm-core` |
| `REGISTRY_HTTP_SECRET` | Независимый секрет Registry для состояния resumable uploads |

После успешного durable claim Core выдаёт единственный случайный exchange credential, сохраняя только SHA-256. Он действует до сохранённого execution deadline. Kaniko получает его в `/kaniko/.docker/config.json`, вне build context и snapshot образа; считайте, что Dockerfile может прочитать его. В очереди, аргументах сборки и публичных DTO его нет. Credentials удаляются вместе с execution container. Используется Kaniko v1.24.0, закреплённый по digest: `gcr.io/kaniko-project/executor@sha256:4e7a52dd1f14872430652bb3b027405b8dfd17c4538751c620ac005741ef9698`, TLS verification включена.

Build credential получает `pull,push` только для своего destination repository и `pull` для других доступных образов своего владельца. Удаление, catalog и чужие repositories запрещены. Права действуют на все теги repository. Core выдаёт Docker daemon отдельный renewable credential с правом чтения одного repository, сроком 30 минут; он не попадает в application container. Metadata/cleanup используют отдельный Core principal с exact-repository `pull,delete`, подписываемый внутри доверенного процесса.

Bearer TTL — 60 секунд; Distribution допускает ещё 60 секунд clock skew. Edge ограничивает чтение, запись и контекст запроса одной минутой. Отмена немедленно запрещает renewal и ставит durable cleanup, который ждёт 181 секунду от последнего issuance. Cleanup сохраняется после удаления build/image rows и повторяется при недоступности Registry. Worker очистки читает настройки перед каждым циклом: ждёт `registry_cleanup_interval_seconds` между опросами и даёт каждой job не больше `registry_cleanup_job_timeout_seconds`. Ошибка опроса или job записывается в лог один раз с ключами `operation` и `error` (для job — также `cleanup_id`), без credentials, tokens и object keys; job остаётся в базе и повторяется в следующем цикле. Fence 181 секунда и срок pull credential 30 минут — политика сервиса, а не runtime-настройки; repository получает их как параметры. Удаление успешного образа выполняется worker после такой же fence: worker ждёт завершения сборок соседних тегов того же repository и окончания fence, не удерживая соединение пула locks, затем берёт блокировку repository, повторно проверяет fence без ожидания и держит блокировку только на время ограниченного удаления в Registry. Если за время ожидания была запущена новая сборка этого repository, операция удаления возвращается в очередь и использует одну попытку; пока блокировка удерживается, новые сборки repository не запускаются. Нужны синхронизированные часы; компрометация signing key требует ротации Registry trust.

При удалении образа запись с владельцем сохраняется в состоянии `deleting`, пока Registry не подтвердит удаление manifest или его отсутствие (`404`). Ошибки lookup/delete, отсутствие digest и некорректный ответ Registry сохраняют операцию в `pending` для повторной обработки в пределах настроенного числа попыток. Таймауты и недоступность возвращаются как безопасные ошибки приложения; детали ответа Registry пользователю не передаются. После исчерпания попыток операция и образ переходят в `failed`/`error`, но запись владельца сохраняется.

Каждая попытка чтения metadata или удаления manifest в Core ограничена 60 секундами, включая получение внутреннего разрешения, ожидание HTTP-заголовков и чтение тела ответа. Более короткий deadline и отмена вызывающего контекста сохраняются; следующая попытка получает новый бюджет. Отдельного runtime/bootstrap-параметра нет. Таймаут соединения или ответа, HTTP `408` и `504` классифицируются как `timeout`; сетевые сбои, `429` и остальные `5xx` — как `unavailable`; `401`/`403` — как `forbidden`; прочие `4xx` — как `bad_request`, кроме `404` (отсутствие manifest, успешное идемпотентное удаление). Причины ошибок сохраняются для внутренней классификации, но endpoint, credentials и тело ответа Registry пользователю не передаются.

Сеть сборки `172.30.252.0/24` должна быть свободна. Она внутренняя; доступны только TCP forwarder Registry edge (`172.30.252.2:5000`), который передаёт TLS только в отдельный listener Core и egress proxy (`172.30.252.3:3128`). Proxy разрешает внешние HTTP/HTTPS endpoints и запрещает private/link-local адреса. `build-network-policy` устанавливает IPv4/IPv6 правила в Docker forwarding и host INPUT; Builder запускается только после успешной установки. Требуются Linux Docker с iptables backend, `DOCKER-USER`, `br_netfilter` и обе bridge-nf-call настройки, равные `1`. Правила сохраняются при остановке policy container. Не отключайте policy и не подключайте execution containers к другим сетям. При перезапуске Docker перезапустите policy до возобновления Builder. Это изменение deployment требует пересоздания сети сборок при отсутствии активных сборок.

Cleanup и claim новой сборки используют одну блокировку repository в PostgreSQL. Отложенное удаление проверяет активных publishers и оставшееся время всех write tokens repository, поскольку bearer даёт права на все его теги. Удаление одного тега ждёт завершения уже работающих сборок соседних тегов; новые claims повторяются после освобождения блокировки. Если появился доступный replacement того же тега, устаревшая cleanup job завершается без удаления его manifest.

Builder проверяет internal bridge и health policy container перед durable claim и перед запуском executor. Healthcheck проверяет сами IPv4/IPv6 правила, а не только файл готовности. Если policy ещё не восстановилась после перезапуска Docker, сообщение остаётся retryable и executor не запускается.
