# Развёртывание Docker Cloud Manager на одной машине

Инструкция описывает установку полного набора служб DCM через корневой `docker-compose.yml`. Все приложения, базы данных, очередь, объектное хранилище, реестр образов и Traefik запускаются на одной машине с Linux.

Шаги 1–11 обязательны и выполняются по порядку. Раздел [«Рекомендуемая настройка сервера»](#рекомендуемая-настройка-сервера) необязателен, но нужен рабочему серверу: он ограничивает память, диск и тома пользователей.

> [!WARNING]
> Текущий Compose принимает внешний трафик по HTTP на порту `80`. HTTPS/ACME не настроены.

## Ограничения текущего варианта

- Один Docker Engine является единственной средой выполнения контейнеров.
- Внешняя точка входа — HTTP `:80`.
- Registry публикуется только на `127.0.0.1:5000` и рассчитан на тот же сервер Docker.
- PostgreSQL, ClickHouse, RabbitMQ и MinIO запускаются на той же машине.
- Высокая доступность и автоматическое восстановление узла не предусмотрены.

## 1. Требования

Подготовьте:

- Linux-машину с публичным IPv4 или IPv6;
- Docker Engine 29.5.1 или новее с актуальными исправлениями безопасности и плагин Docker Compose;
- iptables backend Docker (цепочка `DOCKER-USER`): на нём работает сетевая политика сборок;
- для ограничения памяти пользователей — systemd и cgroup v2 (рекомендуется);
- пользователя с доступом к Docker daemon и правами `sudo`;
- свободный входящий TCP-порт `80`;
- домен панели, например `dcm.example.com`;
- отдельный wildcard-домен приложений, например `*.apps.example.com`;
- `git`, `curl`, `openssl`, Python 3 и утилиту `dig` либо аналог для проверки DNS.

Проверьте окружение:

```bash
docker version
docker compose version
docker info
sudo ss -ltnp '( sport = :80 )'
```

Последняя команда не должна показывать процесс, занимающий `0.0.0.0:80` или `[::]:80`.

Docker Engine обновляется отдельно от DCM. Версия 29.5.1 исправляет [уязвимости копирования архивов в контейнер](https://github.com/moby/moby/security/advisories/GHSA-x86f-5xw2-fm2r), а Builder передаёт исходники сборки в контейнер Kaniko именно этим способом, поэтому более старый Engine использовать нельзя.

## 2. Настройте DNS

Создайте записи, ведущие на адрес сервера:

```text
dcm.example.com       A/AAAA  <server-ip>
*.apps.example.com    A/AAAA  <server-ip>
```

Проверьте обе записи:

```bash
dig +short dcm.example.com
dig +short test.apps.example.com
```

Оба имени должны возвращать адрес сервера. `DCM_HOST` будет равен `dcm.example.com`, а `BASE_DOMAIN` — `apps.example.com`.

## 3. Подготовьте ядро и Docker

### Фильтрация трафика мостов

Служба `build-network-policy` изолирует сеть сборок и опубликованные контейнеры правилами iptables, которые действуют на трафик Docker-мостов только при включённом `br_netfilter`. Без него политика не запускается, а вместе с ней Builder и Core.

```bash
echo br_netfilter | sudo tee /etc/modules-load.d/dcm.conf
sudo modprobe br_netfilter
printf 'net.bridge.bridge-nf-call-iptables = 1\nnet.bridge.bridge-nf-call-ip6tables = 1\n' | sudo tee /etc/sysctl.d/90-dcm.conf
sudo sysctl --system
```

Файлы в `/etc/modules-load.d/` и `/etc/sysctl.d/` сохраняют настройки после перезагрузки.

### Пул адресов Docker-сетей

DCM создаёт отдельную bridge-сеть для каждого пользователя. Стандартного пула Docker может не хватить уже после нескольких десятков сетей.

Добавьте свободный адресный диапазон в `/etc/docker/daemon.json`. Не используйте диапазон, пересекающийся с LAN, VPN, cloud VPC, существующими Docker networks или фиксированными сетями DCM: `172.30.240.0/21` (`PROXY_NETWORK_SUBNET`), `172.30.251.0/24` (`GATEWAY_INGRESS_SUBNET`) и `172.30.252.0/24` (сеть сборок).

```json
{
  "default-address-pools": [
    {
      "base": "10.10.0.0/16",
      "size": 24
    }
  ]
}
```

Если файл уже содержит настройки, добавьте ключ в существующий JSON-объект. Затем проверьте JSON и перезапустите Docker:

```bash
python3 -m json.tool /etc/docker/daemon.json >/dev/null
sudo systemctl restart docker
docker info
```

Перезапуск Docker останавливает работающие контейнеры; на используемом сервере предварительно согласуйте окно обслуживания. Если вы планируете [ограничение корневой файловой системы контейнеров](#ограничение-корневой-файловой-системы-контейнеров), внесите его настройки в `daemon.json` сейчас: смена драйвера хранилища после установки скрывает созданные образы и контейнеры.

## 4. Получите исходный код

```bash
git clone https://github.com/callmerussell04/docker-cloud-manager.git
cd docker-cloud-manager
```

Все следующие Compose-команды выполняются из корня репозитория.

## 5. Создайте env-файл

Скопируйте публичный шаблон:

```bash
cp backend/.env.example backend/.env
chmod 600 backend/.env
```

Сгенерируйте независимые значения для секретов:

```bash
openssl rand -hex 32
```

Выполните команду отдельно для каждого секрета и замените значения `change_me_*`. Как минимум задайте уникальные значения для:

- `POSTGRES_PASSWORD`;
- `JWT_SECRET`;
- `GATEWAY_SERVICE_TOKEN`, `CORE_SERVICE_TOKEN` и `BUILDER_SERVICE_TOKEN` — у каждой службы свои учётные данные; при одинаковых значениях Core и SSO не запускаются;
- `SSO_ADMIN_PASSWORD`;
- `RABBITMQ_PASSWORD`;
- `MINIO_ROOT_PASSWORD` и совпадающего с ним `OBJECT_STORAGE_SECRET_KEY`;
- `CLICKHOUSE_PASSWORD`;
- `REGISTRY_HTTP_SECRET`.

Переменные Keycloak нужны только при запуске дополнительного Keycloak Compose-файла, но оставлять опубликованные значения `change_me_*` небезопасно.

`BUILDER_INSTANCE_ID` обязателен; шаблон задаёт `builder-1`. Не меняйте его при пересоздании контейнера: при запуске Builder удаляет оставшиеся контейнеры сборки только со своим идентификатором. Подробнее — в [справочнике конфигурации](configuration.md#пути-сервера-и-экземпляр-builder).

## 6. Настройте домены и браузерный доступ

Для примера выше установите:

```env
SSO_ADMIN_EMAIL=admin@example.com
DCM_HOST=dcm.example.com
BASE_DOMAIN=apps.example.com
FRONTEND_API_URL=http://dcm.example.com/api/v1
CORS_ALLOWED_ORIGINS=http://dcm.example.com
GATEWAY_AUTH_REDIRECT_ALLOWED_ORIGINS=http://dcm.example.com
COOKIE_SECURE=false
COOKIE_SAMESITE=lax
```

`FRONTEND_API_URL` и `BASE_DOMAIN` встраиваются в клиентскую часть во время сборки. После изменения этих значений её необходимо пересобрать.

Для реестра образов на одной машине оставьте значения шаблона:

```env
REGISTRY_API_URL=https://registry-backend:5000
REGISTRY_URL=registry:5000
REGISTRY_PUBLIC_URL=registry:5000
```

OIDC по умолчанию выключен (`SSO_OIDC_ENABLED=false`); его настройка описана в [документе об аутентификации](authentication.md).

Если Docker хранит данные не в `/var/lib/docker`, задайте `DOCKER_DATA_ROOT` равным значению `Docker Root Dir` из `docker info` (см. [«Каталог данных Docker»](#каталог-данных-docker)).

Полное назначение переменных описано в [справочнике конфигурации](configuration.md).

## 7. Подготовьте сертификаты реестра образов

Реестр образов, Core и Builder общаются только по TLS, а Core подписывает токены доступа к реестру отдельным ключом. Все файлы лежат в каталоге `REGISTRY_CERTS_DIR` (в шаблоне — `/etc/dcm/registry`):

| Файл | Назначение |
| --- | --- |
| `ca.crt` | CA, которой доверяют Core, Builder, Kaniko и Docker daemon |
| `registry.crt`, `registry.key` | TLS-сертификат входного listener Core на `registry:5000`; SAN содержит `registry` |
| `backend.crt`, `backend.key` | TLS-сертификат самого Registry; SAN содержит `registry-backend` |
| `signing.crt`, `signing.key` | Отдельная RSA-пара (не меньше 2048 бит), которой Core подписывает токены Registry. Закрытый ключ подключается только в Core |

Если у вас есть собственный центр сертификации, выпустите `registry.crt` и `backend.crt` в нём. Иначе создайте отдельную CA для реестра:

```bash
work=$(mktemp -d) && cd "$work" && umask 077

openssl req -x509 -newkey rsa:4096 -nodes -days 3650 -subj '/CN=DCM Registry CA' \
  -keyout ca.key -out ca.crt
for pair in registry:registry backend:registry-backend; do
  name=${pair%%:*}; san=${pair##*:}
  openssl req -newkey rsa:2048 -nodes -subj "/CN=$san" -keyout "$name.key" -out "$name.csr"
  printf 'subjectAltName=DNS:%s\nextendedKeyUsage=serverAuth\nkeyUsage=digitalSignature,keyEncipherment\n' "$san" > "$name.ext"
  openssl x509 -req -in "$name.csr" -CA ca.crt -CAkey ca.key -CAcreateserial \
    -days 825 -sha256 -extfile "$name.ext" -out "$name.crt"
done
openssl req -x509 -newkey rsa:2048 -nodes -days 825 -subj '/CN=DCM Registry Token Signing' \
  -keyout signing.key -out signing.crt

sudo install -d -m 755 /etc/dcm/registry
sudo install -m 644 ca.crt registry.crt backend.crt signing.crt /etc/dcm/registry/
sudo install -m 600 registry.key backend.key signing.key /etc/dcm/registry/
```

Сохраните `ca.key` вне сервера (он нужен только для перевыпуска сертификатов) и удалите временный каталог. Сертификаты действуют 825 дней; перевыпустите их до истечения срока и перезапустите стек.

Docker daemon загружает образы пользователей из реестра по имени `registry`, поэтому сервер должен знать это имя и доверять CA:

```bash
sudo install -d /etc/docker/certs.d/registry:5000
sudo install -m 644 /etc/dcm/registry/ca.crt /etc/docker/certs.d/registry:5000/ca.crt
grep -qE '^[^#]*[[:space:]]registry([[:space:]]|$)' /etc/hosts || echo '127.0.0.1 registry' | sudo tee -a /etc/hosts
```

Kaniko и Docker daemon должны достигать реестра по одному и тому же имени. Если вы меняете имя `registry`, согласованно обновите SAN сертификата, `/etc/hosts`, `REGISTRY_URL`, `REGISTRY_PUBLIC_URL` и сетевой псевдоним в Compose.

> [!NOTE]
> Для личной машины то же самое делает `make dev-certs` (из `backend/`, нужен `sudo`): создаёт одноразовую CA, сертификаты, устанавливает `ca.crt` для Docker и добавляет `registry` в `/etc/hosts`. Не используйте его на рабочем сервере.

Как устроены авторизация реестра и изоляция сети сборок, описано в [модели безопасности](security-model.md#реестр-образов).

## 8. Проверьте конфигурацию

```bash
docker compose --env-file backend/.env config --quiet
```

Успешная команда ничего не выводит и завершается с кодом `0`. Ошибка вида `VARIABLE is required` означает, что обязательное значение отсутствует.

## 9. Запустите стек

```bash
docker compose --env-file backend/.env up -d --build
```

Compose последовательно запускает базы данных и инфраструктурные службы, применяет миграции PostgreSQL и ClickHouse, устанавливает сетевую политику, а затем запускает службы DCM. Первая сборка может занять несколько минут.

Проверьте состояние:

```bash
docker compose --env-file backend/.env ps --all
```

Ожидаемый результат:

- `core_db`, `sso_db`, `clickhouse`, `rabbitmq`, `minio`, `build-network-policy` и `frontend` имеют состояние `healthy`;
- `gateway`, `sso`, `core`, `builder`, `traefik`, `registry`, `registry-edge` и `build-egress` находятся в состоянии `running`;
- `migrate_core`, `migrate_sso`, `migrate_reports` завершились с кодом `0`.

При ошибке перейдите к разделу [«Службы не запускаются или проверка готовности возвращает 503»](troubleshooting.md#службы-не-запускаются-или-проверка-готовности-возвращает-503).

## 10. Проверьте доступность

На сервере проверьте маршрутизацию с правильным HTTP-заголовком `Host`:

```bash
curl -fsS -H 'Host: dcm.example.com' http://127.0.0.1/health/live
curl -fsS -H 'Host: dcm.example.com' http://127.0.0.1/health/ready
```

С внешней машины:

```bash
curl -fsS http://dcm.example.com/health/live
curl -fsS http://dcm.example.com/health/ready
curl -I http://dcm.example.com/
```

Проверка жизнеспособности должна вернуть `status: ok`. Проверка готовности возвращает HTTP `200`, только когда Gateway видит Core по gRPC и HTTP и SSO по gRPC.

Реестр образов должен отвечать только с аутентификацией:

```bash
curl -sS -o /dev/null -w '%{http_code}\n' --cacert /etc/dcm/registry/ca.crt https://registry:5000/v2/
```

Ожидается `401`.

## 11. Выполните первый вход

Откройте `http://dcm.example.com/` и войдите с значениями из `backend/.env`:

- имя пользователя — `SSO_ADMIN_USERNAME`;
- пароль — `SSO_ADMIN_PASSWORD`.

После входа:

1. Откройте системный мониторинг и убедитесь, что ресурсы сервера доступны.
2. Создайте тестового пользователя и назначьте квоты.
3. Выполните [минимальную проверку основных возможностей](user-guide.md#минимальная-проверка-основных-возможностей) из руководства пользователя.
4. Выполните [проверку сетевой политики хоста](#проверка-сетевой-политики-хоста).
5. Настройте [резервное копирование](operations.md#резервное-копирование).

## Рекомендуемая настройка сервера

### Каталог данных Docker

Core видит с сервера только каталог данных Docker: Compose подключает `DOCKER_DATA_ROOT` (по умолчанию `/var/lib/docker`) только для чтения по пути `/host/var/lib/docker`. Остальная файловая система сервера в контейнер Core не попадает. По этому каталогу Core измеряет свободное место для дискового порога и мониторинга и размеры томов.

Узнайте каталог данных Docker:

```bash
docker info --format '{{.DockerRootDir}}'
```

Если он отличается от `/var/lib/docker`, задайте его в `backend/.env`, например `DOCKER_DATA_ROOT=/srv/docker`. Если значение не совпадает с каталогом Docker, Core не запускается и пишет в журнал ошибку с именем `DOCKER_DATA_ROOT`.

Проверьте подключение после запуска стека. В первой строке укажите значение `DOCKER_DATA_ROOT` из `backend/.env`:

```bash
DATA_ROOT=/var/lib/docker
docker inspect --format '{{range .Mounts}}{{.Source}} -> {{.Destination}} (rw={{.RW}}){{println}}{{end}}' "$(docker compose --env-file backend/.env ps -q core)"
docker compose --env-file backend/.env exec core df -h "/host$DATA_ROOT"
docker compose --env-file backend/.env exec core ls /host
```

Ожидаемый результат: среди подключений есть `$DATA_ROOT -> /host$DATA_ROOT (rw=false)` и нет подключения `/` к `/host`; `df` показывает файловую систему, на которой лежат данные Docker; в `/host` виден только первый каталог пути `DATA_ROOT` (для `/var/lib/docker` — `var`, для `/srv/docker` — `srv`). Свободное место на этой файловой системе совпадает с `disk_free_bytes` в мониторинге.

### Ограничение памяти пользовательских контейнеров

Лимит памяти контейнера больше его резерва, поэтому вместе пользовательские контейнеры могут запросить больше памяти, чем есть на сервере. Core останавливает контейнеры, превысившие резерв, но проверяет память периодически. Родительская cgroup с ограничением памяти гарантирует, что нехватка останется внутри неё и не затронет PostgreSQL, Docker и службы DCM.

Пока она не задана, Core при каждом запуске пишет предупреждение `TENANT_CGROUP_PARENT is not set`. Без неё пользовательские контейнеры и контейнеры сборки защищены только тем, что создаются с `oom_score_adj` 500: при нехватке памяти OOM killer ядра выбирает их раньше служб платформы.

На сервере с systemd и cgroup v2 создайте slice. `MemoryMax` равен памяти сервера за вычетом `reserved_system_memory_bytes`; для сервера с 16 ГБ и значением по умолчанию 2 ГБ это 14 ГБ:

```ini
# /etc/systemd/system/dcm-tenants.slice
[Unit]
Description=DCM user and build containers

[Slice]
MemoryMax=14G
MemorySwapMax=0
```

```bash
sudo systemctl daemon-reload
sudo systemctl start dcm-tenants.slice
```

Укажите slice в env-файле и перезапустите Core и Builder:

```dotenv
TENANT_CGROUP_PARENT=dcm-tenants.slice
```

Настройка действует на контейнеры, созданные после перезапуска. Проверьте размещение нового контейнера:

```bash
docker inspect --format '{{.HostConfig.CgroupParent}}' <container>
systemctl status dcm-tenants.slice
```

DCM не создаёт и не изменяет slice. При изменении `reserved_system_memory_bytes` или объёма памяти сервера обновите `MemoryMax` вручную. Docker должен использовать cgroup driver `systemd` (`docker info`); с драйвером `cgroupfs` вместо имени slice указывается путь cgroup.

### Ограничение корневой файловой системы контейнеров

Настройка `container_disk_quota` (по умолчанию `1G`) передаётся Docker как параметр хранилища `size` при создании контейнера. Docker применяет его не на всех серверах: на части конфигураций он принимает параметр без ошибки и ничего не ограничивает. Тогда контейнер может заполнить диск сервера записью в свою файловую систему (тома ограничиваются отдельно).

Core при запуске читает сведения о хранилище Docker и показывает результат в мониторинге (`rootfs_quota_status`):

| Хранилище Docker | Состояние |
|---|---|
| `overlay2` на XFS | `enforced` — Docker сам отказывает в создании контейнера, если XFS смонтирована без `pquota` |
| `btrfs`, `zfs`, `devicemapper` | `enforced` |
| `overlay2` на другой файловой системе (например ext4) | `not_enforced` |
| хранилище образов containerd (`driver-type: io.containerd.snapshotter.v1`) | `not_enforced` |
| любое другое хранилище или сведения не прочитаны | `unknown` |

Если состояние не `enforced`, Core пишет предупреждение в журнал при каждом запуске; работе DCM это не мешает.

Чтобы ограничение действовало, до установки подготовьте каталог данных Docker (`/var/lib/docker`):

1. Разместите его на XFS, смонтированной с параметром `pquota` (для корневой файловой системы — через параметр ядра `rootflags=pquota`), например в `/etc/fstab`:

   ```text
   /dev/sdb1  /var/lib/docker  xfs  defaults,pquota  0 0
   ```

2. Используйте драйвер хранилища `overlay2` без хранилища образов containerd. В `/etc/docker/daemon.json`:

   ```json
   {
     "storage-driver": "overlay2",
     "features": { "containerd-snapshotter": false }
   }
   ```

3. Перезапустите Docker и проверьте:

   ```bash
   docker info --format '{{.Driver}} {{json .DriverStatus}}'
   ```

   Ожидается `overlay2` и `["Backing Filesystem","xfs"]` без `driver-type`.

4. Перезапустите Core и убедитесь, что в мониторинге ограничение показано как действующее.

Смена драйвера хранилища скрывает уже загруженные образы и созданные контейнеры. Выполняйте её до установки DCM.

### Ограничение размеров томов

У каждого тома есть размер, который резервируется из дисковой квоты владельца. По умолчанию размер проверяется периодически (измеряемый режим): том может превысить его в промежутке между двумя измерениями, после чего контейнеры, пишущие в этот том, останавливаются. Этот режим работает на любой файловой системе.

Настройка `volume_size_enforcement` (по умолчанию `false`) включает ограничение ядром: размер нового тома передаётся локальному драйверу томов Docker (`--opt size=...`), и запись сверх него завершается ошибкой внутри контейнера. Для этого каталог томов Docker (`/var/lib/docker/volumes`) должен находиться на файловой системе с проектными квотами, например на XFS с параметром `pquota` (подготовка — как в шагах 1 и 3 предыдущего раздела; драйвер хранилища образов для томов значения не имеет). На ext4 без проектных квот Docker отвечает `quota size requested but no quota support`.

Проверьте поддержку до включения настройки:

```bash
docker volume create --opt size=16M dcm-quota-check && docker volume rm dcm-quota-check
```

Команда должна завершиться без ошибки. Поведение Core при включении и при потере поддержки описано в [справочнике конфигурации](configuration.md#размеры-томов-и-дисковая-квота).

Ручная проверка ограничения на подготовленном сервере (из `backend/`, Docker должен быть доступен текущему пользователю, нужен образ `busybox`):

```bash
DCM_TEST_VOLUME_QUOTA_DOCKER=1 GOTOOLCHAIN=go1.26.11 GOCACHE=/tmp/go-build-cache \
  go test ./internal/core/infrastructure/docker/ -run TestDockerAdapterEnforcesVolumeSizeSmoke -v
```

Тест создаёт том размером 16 MiB, записывает в него из контейнера сначала 4 MiB (должно пройти), затем ещё 32 MiB (должно завершиться ошибкой), и удаляет за собой контейнер, сеть и том. Без переменной `DCM_TEST_VOLUME_QUOTA_DOCKER=1` тест пропускается, в том числе в `make test` и CI.

### Проверка сетевой политики хоста

`build-network-policy` устанавливает правила iptables/ip6tables, которые изолируют сеть сборок и опубликованные контейнеры разных пользователей (см. [модель безопасности](security-model.md#сеть-сборок)). `make test` и CI эту проверку не выполняют: она собирает образ политики и меняет правила хоста. Выполните её на целевом сервере перед эксплуатацией и после изменения `deployment/registry/network-policy*` или правил firewall хоста. Нужны Go, root-доступ к Docker, `CAP_NET_ADMIN` и host network:

```bash
cd backend
GOTOOLCHAIN=go1.26.11 CORE_TEST_NETWORK_POLICY=1 \
  go test -count=1 -timeout=3m -run TestNetworkPolicy ./tests/integration/registry_isolation
```

Тест проверяет сеть сборки и изоляцию `proxy_net`: два контейнера в proxy bridge не могут подключиться друг к другу, а контейнер с адресом Traefik достигает обоих; готовность политики проверяется её healthcheck-скриптом. Тест использует уникальные временные bridges и chains и удаляет их после завершения.

Согласуйте действующий firewall хоста с этими правилами. Правила сохраняются при остановке контейнера политики; после перезапуска Docker политика устанавливается заново до возобновления сборок.

## Остановка

Безопасная остановка с сохранением данных:

```bash
docker compose --env-file backend/.env down
```

> [!CAUTION]
> Не добавляйте `-v`, если не требуется безвозвратно удалить базы данных, object storage, registry и очереди.

Пользовательские контейнеры создаёт Core через Docker API, а не этот Compose-файл, поэтому `down` их не останавливает. Обновление и резервное копирование описаны в [руководстве по эксплуатации](operations.md).

## Следующие шаги

- [Руководство администратора](admin-guide.md)
- [Обновление и резервное копирование](operations.md)
- [Аутентификация и OIDC](authentication.md)
- [Диагностика](troubleshooting.md)

[К оглавлению документации](README.md)
