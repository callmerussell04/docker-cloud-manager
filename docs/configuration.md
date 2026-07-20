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
| `REGISTRY_API_URL` | `registry:5000` | Адрес API реестра образов для Core |
| `REGISTRY_URL` | `registry:5000` | Адрес реестра образов для Builder |
| `REGISTRY_PUBLIC_URL` | `localhost:5000` | Адрес реестра, с которого сервер Docker загружает образы |
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
| `TRAEFIK_LOG_LEVEL` | `INFO` | Уровень журналирования Traefik | Перезапуск Traefik |
| `COMPOSE_LOG_MAX_SIZE` | `10m` | Размер файла журнала Docker до ротации | Повторное создание контейнеров |
| `COMPOSE_LOG_MAX_FILE` | `3` | Количество файлов журнала | Повторное создание контейнеров |

`TRAEFIK_ACME_EMAIL` и `TRAEFIK_ACME_STORAGE` оставлены как задел на будущее и не включают HTTPS в текущем Compose.

### Gateway и файлы cookie

| Группа | Переменные | Назначение |
| --- | --- | --- |
| Источники | `CORS_ALLOWED_ORIGINS`, `GATEWAY_AUTH_REDIRECT_ALLOWED_ORIGINS`, `GATEWAY_AUTH_CALLBACK_PATH` | Разрешённые источники браузерных страниц и обратный вызов одностраничного приложения |
| Доверие к прокси | `GATEWAY_TRUSTED_PROXIES` | Явный список доверенных обратных прокси-серверов |
| Ограничения тела и частоты запросов | `GATEWAY_MAX_JSON_BODY_BYTES`, `GATEWAY_AUTH_RATE_LIMIT_REQUESTS`, `GATEWAY_AUTH_RATE_LIMIT_WINDOW_SECONDS`, `GATEWAY_UPLOAD_RATE_LIMIT_REQUESTS`, `GATEWAY_UPLOAD_RATE_LIMIT_WINDOW_SECONDS` | Ограничения на внешней границе API |
| Время ожидания HTTP | `GATEWAY_READ_HEADER_TIMEOUT_SECONDS`, `GATEWAY_READ_TIMEOUT_SECONDS`, `GATEWAY_WRITE_TIMEOUT_SECONDS`, `GATEWAY_IDLE_TIMEOUT_SECONDS`, `GATEWAY_SHUTDOWN_TIMEOUT_SECONDS`, `GATEWAY_MAX_HEADER_BYTES` | Ограничения публичного HTTP-сервера |
| Передача через прокси | `GATEWAY_PROXY_DIAL_TIMEOUT_SECONDS`, `GATEWAY_PROXY_TLS_HANDSHAKE_TIMEOUT_SECONDS`, `GATEWAY_PROXY_RESPONSE_HEADER_TIMEOUT_SECONDS`, `GATEWAY_PROXY_IDLE_CONN_TIMEOUT_SECONDS`, `GATEWAY_PROXY_EXPECT_CONTINUE_TIMEOUT_SECONDS`, `GATEWAY_PROXY_MAX_IDLE_CONNS`, `GATEWAY_PROXY_MAX_IDLE_CONNS_PER_HOST` | Передача HTTP-запросов к внутренним службам |
| gRPC | `GATEWAY_GRPC_REQUEST_TIMEOUT_SECONDS`, `GATEWAY_GRPC_MIN_CONNECT_TIMEOUT_SECONDS`, `GATEWAY_GRPC_TLS_ENABLED`, `GATEWAY_GRPC_TLS_CA_FILE`, `GATEWAY_GRPC_TLS_CERT_FILE`, `GATEWAY_GRPC_TLS_KEY_FILE`, `GATEWAY_GRPC_TLS_SERVER_NAME` | Соединения gRPC от Gateway к внутренним службам |
| Состояние и потоки | `GATEWAY_READINESS_TIMEOUT_SECONDS`, `GATEWAY_TELEMETRY_TICKET_TTL_SECONDS` | Время ожидания проверки готовности и срок действия билета доступа |
| Файл cookie | `COOKIE_NAME`, `COOKIE_PATH`, `COOKIE_DOMAIN`, `COOKIE_MAX_AGE_SECONDS`, `COOKIE_SECURE`, `COOKIE_HTTP_ONLY`, `COOKIE_SAMESITE` | Атрибуты файла cookie с токеном обновления |

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
| `max_build_time_minutes`, `max_concurrent_builds` | `10`, `2` | Время ожидания и количество одновременных сборок |
| `max_upload_size_bytes`, `max_archive_size_bytes`, `max_unpacked_size_bytes` | 50 MiB, 50 MiB, 500 MiB | Ограничения размера сборки |
| `max_build_log_size_bytes` | 5 MiB | Ограничение размера сохранённого журнала |
| `max_staged_source_bytes_per_user`, `max_queued_builds_per_user` | 1 GiB, `10` | Ограничения для одного пользователя |
| `host_min_free_disk_bytes` | 1 GiB | Минимальный прогнозируемый остаток места на диске |
| `git_sources_enabled` | `true` | Включает источники Git |
| `git_allowed_hosts` | GitHub/GitLab/Bitbucket | Точный список разрешённых имён серверов |
| `git_clone_timeout_seconds`, `git_max_repository_bytes` | `60`, 200 MiB | Ограничения клонирования |

### Фоновые обработчики, Compose, отчёты и телеметрия

- Обработчики сборок: `build_cancel_poll_interval_seconds`, `build_outbox_interval_seconds`, `build_outbox_batch_size`, `stale_build_timeout_minutes`.
- Жизненный цикл контейнеров: `container_create_worker_count`, `container_create_max_attempts`, `container_create_timeout_minutes`, `max_queued_container_creates_per_user`, `container_create_outbox_interval_seconds`, `container_create_outbox_batch_size`.
- Сверка состояния и очистка: `ttl_worker_interval_seconds`, `gc_worker_interval_minutes`, `event_sync_interval_seconds`, `event_reconnect_delay_seconds`.
- Compose: `compose_upload_max_bytes`, `compose_pipeline_timeout_minutes`, `compose_deploy_worker_count`, `compose_outbox_interval_seconds`, `compose_outbox_batch_size`, `compose_deploy_max_attempts`, `max_queued_compose_deploys_per_user`, `compose_build_poll_interval_seconds`, `compose_dependency_wait_timeout_minutes`, `compose_dependency_poll_interval_seconds`, `compose_coordinator_interval_seconds`.
- Отчёты: `reports_usage_snapshot_interval_seconds`, допустимый диапазон `60..86400` секунд, значение по умолчанию `300`.
- Телеметрия: `telemetry_max_log_tail_lines`, `telemetry_max_log_streams_per_user`, `telemetry_max_terminal_sessions_per_user`, `telemetry_terminal_idle_timeout_seconds`, `telemetry_terminal_max_duration_seconds`, `telemetry_allowed_exec_commands`, `telemetry_max_command_args`, `telemetry_max_command_arg_bytes`, `telemetry_ws_read_limit_bytes`.

Все поля количества, интервала, времени ожидания и размера должны быть положительными, кроме явно разрешённых нулевых ограничений. Разрешённые серверы, команды и префиксы должны быть непустыми и уникальными.

[Аутентификация](authentication.md) · [Руководство администратора](admin-guide.md) · [К оглавлению](README.md)
