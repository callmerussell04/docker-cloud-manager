# Эксплуатация: обновление и резервное копирование

Документ описывает регулярные задачи владельца сервера после [развёртывания](deployment-single-vm.md): остановку, обновление DCM и резервное копирование. Все Compose-команды выполняются из корня репозитория.

> [!IMPORTANT]
> DCM находится в разработке. До первого выпуска конфигурация, API и схема базы данных могут меняться несовместимо. Перед каждым обновлением делайте резервную копию.

## Где хранятся данные

| Том Compose | Содержимое |
| --- | --- |
| `core_db_data` | PostgreSQL Core: контейнеры, тома, образы, сборки, проекты, операции, квоты |
| `sso_db_data` | PostgreSQL SSO: пользователи, роли, сессии |
| `core_config_data` | Файл рабочих настроек Core (страница «Настройки системы») |
| `clickhouse_data` | Отчёты и журнал аудита |
| `registry_data` | Образы, собранные пользователями |
| `minio_data` | Исходники, ожидающие сборки или развёртывания, и журналы сборок |
| `rabbitmq_data` | Очереди операций и сборок |
| `builder_storage`, `builder_logs` | Рабочие каталоги и результаты сборок, ещё не доставленные в Core |

Помимо томов Compose, данные пользователей лежат в Docker-томах, которые создаёт Core. Их можно найти по метке:

```bash
docker volume ls --filter label=managed_by=docker-cloud-manager
```

Вне Docker хранятся:

- `backend/.env` — все секреты и настройки запуска;
- каталог `REGISTRY_CERTS_DIR` (по умолчанию `/etc/dcm/registry`) и `ca.key`, если вы создавали свою CA;
- `/etc/docker/daemon.json`, `/etc/docker/certs.d/registry:5000/ca.crt` и запись `registry` в `/etc/hosts`;
- `/etc/systemd/system/dcm-tenants.slice`, `/etc/modules-load.d/dcm.conf` и `/etc/sysctl.d/90-dcm.conf`, если вы их создавали.

## Остановка и запуск

```bash
docker compose --env-file backend/.env down    # остановить службы DCM, данные сохраняются
docker compose --env-file backend/.env up -d   # запустить снова
```

> [!CAUTION]
> `down -v` безвозвратно удаляет тома Compose из таблицы выше.

Пользовательские контейнеры создаёт Core, а не этот Compose-файл, поэтому `down` их не останавливает.

## Обновление DCM

1. Сделайте [резервную копию](#резервное-копирование).
2. Получите новую версию:

   ```bash
   git pull
   ```

3. Сравните свой `backend/.env` с шаблоном и добавьте новые обязательные переменные:

   ```bash
   diff <(grep -oE '^[A-Z_]+' backend/.env.example | sort) <(grep -oE '^[A-Z_]+' backend/.env | sort)
   ```

   Строки с `<` — переменные, которых нет в вашем файле.

4. Проверьте конфигурацию и пересоберите стек:

   ```bash
   docker compose --env-file backend/.env config --quiet
   docker compose --env-file backend/.env up -d --build
   ```

   Миграции PostgreSQL и ClickHouse применяются автоматически перед запуском служб. Если `migrate_core`, `migrate_sso` или `migrate_reports` завершились с ненулевым кодом, службы DCM не запустятся: прочитайте журнал миграции (`docker compose --env-file backend/.env logs migrate_core`) и при необходимости восстановите резервную копию.

5. Проверьте состояние, как после [первого запуска](deployment-single-vm.md#9-запустите-стек).

Рабочие настройки сохраняются при обновлении. Новая рабочая настройка при первом запуске получает значение из одноимённой переменной окружения или значение по умолчанию (см. [справочник конфигурации](configuration.md#начальные-значения-рабочих-настроек)).

### Если обновление меняет сети

Compose не может изменить параметры уже существующей сети. Если в примечаниях к обновлению сказано, что изменились `build_net` или `proxy_net`, пересоздайте их вручную:

- **`build_net`**: дождитесь, пока в мониторинге не останется выполняющихся сборок, и сразу остановите подключённые к сети службы (`docker compose --env-file backend/.env stop builder registry-edge build-egress`), удалите сеть (`docker network rm build_net`) и запустите стек снова (`up -d`).
- **`proxy_net`**: остановите стек, отключите от сети опубликованные контейнеры (`docker network disconnect -f proxy_net <container>`) и удалите её (`docker network rm proxy_net`). После `up -d` Compose создаст её с фиксированными bridge и подсетью; затем подключите контейнеры снова (`docker network connect proxy_net <container>`).

Список подключённых контейнеров покажет `docker network inspect proxy_net --format '{{range .Containers}}{{.Name}} {{end}}'`.

### Обновление Docker Engine и ОС

Docker Engine обновляется отдельно от DCM; версия не ниже 29.5.1 (см. [требования](deployment-single-vm.md#1-требования)). Перезапуск Docker останавливает все контейнеры, включая пользовательские. После него убедитесь, что `build-network-policy` снова в состоянии `healthy` (`docker compose --env-file backend/.env ps`): пока политика не восстановлена, Builder не запускает сборки. Затем проверьте состояние пользовательских контейнеров в административном разделе ресурсов.

Пересборка (`up -d --build`) устанавливает текущие пакеты ОС закреплённой ветки Alpine, поэтому периодическая пересборка получает исправления безопасности образов служб.

## Резервное копирование

DCM не содержит встроенного средства резервного копирования. Базы данных, образы и Docker-тома пользователей связаны: запись о контейнере ссылается на образ в реестре и тома в Docker. Поэтому согласованная копия — это копия всего каталога данных Docker, снятая при остановленном Docker.

### Полная копия (рекомендуется)

Копия включает тома Compose, тома пользователей с их метками, образы и контейнеры.

```bash
DATA_ROOT=$(docker info --format '{{.DockerRootDir}}')
BACKUP=/backup/dcm-$(date +%F)

sudo systemctl stop docker docker.socket
sudo mkdir -p "$BACKUP"
sudo rsync -aHAX --numeric-ids "$DATA_ROOT/" "$BACKUP/docker/"
sudo systemctl start docker
```

Отдельно скопируйте файлы вне Docker из [списка выше](#где-хранятся-данные). `backend/.env`, ключи сертификатов и `ca.key` содержат секреты: храните копию зашифрованной и с ограниченным доступом.

Остановка Docker прерывает работу пользователей; выбирайте окно обслуживания. Если каталог данных Docker лежит на LVM, ZFS или btrfs, вместо остановки на всё время копирования можно сделать снимок файловой системы при остановленном Docker и копировать уже снимок.

### Логические копии баз данных

Между полными копиями можно снимать копии PostgreSQL без остановки. Они не заменяют полную копию: тома пользователей и образы в них не входят.

```bash
mkdir -p backup
docker compose --env-file backend/.env exec -T core_db sh -c 'pg_dump -U "$POSTGRES_USER" -Fc "$POSTGRES_DB"' > backup/core_db.dump
docker compose --env-file backend/.env exec -T sso_db sh -c 'pg_dump -U "$POSTGRES_USER" -Fc "$POSTGRES_DB"' > backup/sso_db.dump
```

Копия SSO позволяет восстановить пользователей и роли. Восстановленная отдельно база Core расходится с фактическим состоянием Docker: ресурсы, которых в Docker уже нет, Core пометит `missing`.

### Восстановление

Полная копия восстанавливается на сервер с той же версией Docker Engine, тем же драйвером хранилища и тем же путём каталога данных:

1. Подготовьте сервер по [инструкции по развёртыванию](deployment-single-vm.md) до шага 4 и восстановите файлы вне Docker (`backend/.env`, `REGISTRY_CERTS_DIR`, `/etc/docker/…`, `/etc/hosts`).
2. Остановите Docker, верните каталог данных и запустите Docker:

   ```bash
   sudo systemctl stop docker docker.socket
   sudo rsync -aHAX --numeric-ids --delete /backup/dcm-<дата>/docker/ "$DATA_ROOT/"
   sudo systemctl start docker
   ```

3. Запустите стек (`docker compose --env-file backend/.env up -d`) и проверьте его, как после [первого запуска](deployment-single-vm.md#9-запустите-стек).

Логическую копию PostgreSQL восстанавливайте при остановленных Core и SSO:

```bash
docker compose --env-file backend/.env stop core sso gateway builder
docker compose --env-file backend/.env exec -T core_db sh -c 'pg_restore -U "$POSTGRES_USER" -d "$POSTGRES_DB" --clean --if-exists' < backup/core_db.dump
docker compose --env-file backend/.env up -d
```

Проверяйте восстановление на отдельном сервере до того, как оно понадобится.

[К оглавлению документации](README.md)
