# Развёртывание Docker Cloud Manager на одной машине

Инструкция описывает установку полного набора служб DCM через корневой `docker-compose.yml`. Все приложения, базы данных, очередь, объектное хранилище, реестр образов и Traefik запускаются на одной машине с Linux.

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
- Docker Engine 29.5.1 или новее с актуальными исправлениями безопасности и Docker Compose plugin;
- пользователя с доступом к Docker daemon;
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

Обновление Go SDK в DCM не обновляет Docker daemon на сервере. Версия Engine 29.5.1 исправляет [уязвимости копирования архивов в контейнер](https://github.com/moby/moby/security/advisories/GHSA-x86f-5xw2-fm2r), включая связанные гонки при обработке путей. Builder передаёт несжатый tar в ещё не запущенный контейнер Kaniko; исходные ZIP/TAR.GZ не отправляются daemon напрямую. Сохраняйте этот порядок и обновляйте Engine отдельно от DCM.

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

## 3. Расширьте пул Docker-сетей

DCM создаёт отдельную bridge-сеть для каждого пользователя. Стандартного пула Docker может не хватить уже после нескольких десятков сетей.

Перед первым запуском добавьте свободный адресный диапазон в `/etc/docker/daemon.json`. Не используйте диапазон, пересекающийся с LAN, VPN, cloud VPC или существующими Docker networks.

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

Перезапуск Docker останавливает работающие контейнеры; на используемом сервере предварительно согласуйте окно обслуживания.

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
- `INTERNAL_SERVICE_TOKEN`;
- `SSO_ADMIN_PASSWORD`;
- `RABBITMQ_PASSWORD`;
- `MINIO_ROOT_PASSWORD` и совпадающего с ним `OBJECT_STORAGE_SECRET_KEY`;
- `CLICKHOUSE_PASSWORD`.

Переменные Keycloak обязательны только при запуске дополнительного Keycloak Compose-файла, но оставлять опубликованные значения `change_me_*` небезопасно.

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

Для реестра образов на одной машине оставьте:

```env
REGISTRY_API_URL=https://registry-backend:5000
REGISTRY_URL=registry:5000
REGISTRY_PUBLIC_URL=registry:5000
```

OIDC по умолчанию выключен:

```env
SSO_OIDC_ENABLED=false
```

Полное назначение переменных описано в [справочнике конфигурации](configuration.md).

Перед первым запуском выполните раздел «TLS и изоляция Registry» ниже.

## 7. Проверьте конфигурацию

```bash
docker compose --env-file backend/.env config --quiet
```

Успешная команда ничего не выводит и завершается с кодом `0`. Ошибка вида `VARIABLE is required` означает, что обязательное значение отсутствует.

## 8. Запустите стек

```bash
docker compose --env-file backend/.env up -d --build
```

Compose последовательно запускает базы данных и инфраструктурные службы, применяет миграции PostgreSQL и ClickHouse, а затем запускает службы DCM. Первая сборка может занять несколько минут.

Проверьте состояние:

```bash
docker compose --env-file backend/.env ps --all
```

Ожидаемый результат:

- `core_db`, `sso_db`, `clickhouse`, `rabbitmq`, `minio` и `frontend` имеют состояние `healthy`;
- `gateway`, `sso`, `core`, `builder`, `traefik` и `registry` находятся в состоянии `running`;
- `migrate_core`, `migrate_sso`, `migrate_reports` завершились с кодом `0`.

При ошибке перейдите к разделу [«Службы не запускаются или проверка готовности возвращает 503»](troubleshooting.md#службы-не-запускаются-или-проверка-готовности-возвращает-503).

## 9. Проверьте доступность

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

## 10. Выполните первый вход

Откройте:

```text
http://dcm.example.com/
```

Используйте значения из `backend/.env`:

- имя пользователя — `SSO_ADMIN_USERNAME`;
- пароль — `SSO_ADMIN_PASSWORD`.

После входа:

1. Откройте системный мониторинг и убедитесь, что ресурсы сервера доступны.
2. Создайте тестового пользователя и назначьте квоты.
3. Выполните [минимальную проверку основных возможностей](user-guide.md#минимальная-проверка-основных-возможностей) из руководства пользователя.

## Остановка

Безопасная остановка с сохранением данных:

```bash
docker compose --env-file backend/.env down
```

> [!CAUTION]
> Не добавляйте `-v`, если не требуется безвозвратно удалить базы данных, object storage, registry и очереди.

## Ограничение памяти пользовательских контейнеров

Этот шаг необязателен, но рекомендуется для рабочего сервера. Лимит памяти контейнера больше его резерва, поэтому вместе пользовательские контейнеры могут запросить больше памяти, чем есть на сервере. Core останавливает контейнеры, превысившие резерв, но проверяет память периодически. Родительская cgroup с ограничением памяти гарантирует, что нехватка останется внутри неё и не затронет PostgreSQL, Docker и службы DCM.

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

## Ограничение корневой файловой системы контейнеров

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

## Ограничение размеров томов

У каждого тома есть размер, который резервируется из дисковой квоты владельца. По умолчанию размер проверяется периодически (измеряемый режим): том может превысить его в промежутке между двумя измерениями, после чего контейнеры, пишущие в этот том, останавливаются. Этот режим работает на любой файловой системе.

Настройка `volume_size_enforcement` (env `VOLUME_SIZE_ENFORCEMENT`, по умолчанию `false`) включает ограничение ядром: размер нового тома передаётся локальному драйверу томов Docker (`--opt size=...`), и запись сверх него завершается ошибкой внутри контейнера. Для этого каталог томов Docker (`/var/lib/docker/volumes`) должен находиться на файловой системе с проектными квотами, например на XFS, смонтированной с параметром `pquota` (подготовка — как в шагах 1 и 3 предыдущего раздела; драйвер хранилища образов для томов значения не имеет). На ext4 без проектных квот Docker отвечает `quota size requested but no quota support`.

Проверьте поддержку до включения настройки:

```bash
docker volume create --opt size=16M dcm-quota-check && docker volume rm dcm-quota-check
```

Команда должна завершиться без ошибки. При включении настройки Core выполняет ту же проверку и отклоняет изменение, если сервер не поддерживает размеры томов. Если настройка включена, а при запуске Core проверка не проходит (например, после переноса каталога Docker), Core запускается, пишет ошибку в журнал, создаёт новые тома в измеряемом режиме и показывает в мониторинге `volume_size_mode = unavailable`. Включение не меняет уже созданные тома: они остаются измеряемыми.

Ручная проверка ограничения на подготовленном сервере (из `backend/`, Docker должен быть доступен текущему пользователю, нужен образ `busybox`):

```bash
DCM_TEST_VOLUME_QUOTA_DOCKER=1 GOTOOLCHAIN=go1.26.10 GOCACHE=/tmp/go-build-cache \
  go test ./internal/core/infrastructure/docker/ -run TestDockerAdapterEnforcesVolumeSizeSmoke -v
```

Тест создаёт том размером 16 MiB, записывает в него из контейнера сначала 4 MiB (должно пройти), затем ещё 32 MiB (должно завершиться ошибкой), и удаляет за собой контейнер, сеть и том. Без переменной `DCM_TEST_VOLUME_QUOTA_DOCKER=1` тест пропускается, в том числе в `make test` и CI.

## Следующие шаги

- [Руководство администратора](admin-guide.md)
- [Аутентификация и OIDC](authentication.md)
- [Диагностика](troubleshooting.md)

[К оглавлению документации](README.md)

## TLS и изоляция Registry

Перед запуском подготовьте `REGISTRY_CERTS_DIR` (например `/etc/dcm/registry`) с правами, ограничивающими чтение private keys:

- `ca.crt` — CA для TLS;
- `registry.crt`, `registry.key` — TLS certificate/key для Core edge, SAN включает `registry`;
- `backend.crt`, `backend.key` — TLS certificate/key backend, SAN включает `registry-backend`;
- `signing.crt`, `signing.key` — отдельный RSA signing certificate/key Core. Используйте отдельный self-signed certificate либо certificate, непосредственно доверенный в `signing.crt` Registry. Приватный signing key монтируется только в Core.

Для локальной проверки на личной машине `make dev-certs` (из `backend/`, нужен `sudo`) создаёт одноразовые сертификаты, устанавливает `ca.crt` для Docker и добавляет `registry` в `/etc/hosts`; не используйте его в production.

Создайте сертификаты своей CA, задайте `REGISTRY_HTTP_SECRET` независимо от остальных секретов. Включите `br_netfilter` и `net.bridge.bridge-nf-call-iptables=1`, `net.bridge.bridge-nf-call-ip6tables=1`. Compose требует iptables backend Docker и устанавливает ограничения для отдельных bridges `br-dcm-build`/`br-dcm-egress`; не используйте эти имена для других сетей. Диапазон `172.30.252.0/24` должен быть свободен.

Для single-server конфигурации установите `REGISTRY_URL=registry:5000` и `REGISTRY_PUBLIC_URL=registry:5000`. Добавьте на Docker host `registry` в `/etc/hosts` с адресом `127.0.0.1` и установите `ca.crt` в `/etc/docker/certs.d/registry:5000/ca.crt`. Docker должен доверять этой CA. Канонический TLS realm должен быть достижим по одному имени и из Kaniko, и из daemon. Для другого DNS имени обновите SAN, host DNS, оба адреса и network alias согласованно.

Порт `127.0.0.1:5000` принадлежит TCP forwarder `registry-edge`, который передаёт TLS только в аутентифицированный listener Core. Core не подключается к сети сборки. Сам Registry не публикует порты и находится в отдельной внутренней сети. Не добавляйте его в `build_net`, `dcm_net` или Traefik. Запрос без credentials к `https://registry:5000/v2/` должен вернуть `401` и Bearer challenge; plaintext HTTP и Core management paths на этом порту недоступны.

`build-network-policy` требует `CAP_NET_ADMIN` и host network для установки host firewall. `build-egress` обеспечивает доступ Dockerfile к внешним пакетам по HTTP(S) через `HTTP_PROXY`/`HTTPS_PROXY`; клиенты, игнорирующие proxy, не получают прямой интернет-доступ. Builder ждёт готовности policy. Для обновления существующего стенда остановите новые сборки, дождитесь завершения текущих, затем пересоздайте `build_net` как internal network. Согласуйте действующий host firewall с этими правилами; не снимайте ограничения при остановке worker.

Двухвладельческая проверка изоляции Registry входит в `make test` (и в CI) вместе с Docker smoke-тестами: `make test` задаёт `CORE_TEST_REGISTRY_ISOLATION=1` и использует свою disposable test database. Ей нужны Docker и доступ к pinned-образам Registry и Kaniko. Отдельный запуск (только disposable test database):

```bash
cd backend
GOTOOLCHAIN=go1.26.10 CORE_TEST_REGISTRY_ISOLATION=1 CORE_TEST_DATABASE_URL='postgres://.../dcm_core_test?sslmode=disable' \
  go test -count=1 -timeout=5m ./tests/integration/registry_isolation
```

Тест создаёт собственные Registry storage, bridge и execution containers, проверяет реальные Dockerfile RUN с прочитанным job credential и удаляет свои fixtures. Он не устанавливает host firewall и не проверяет policy container; перед эксплуатацией отдельно проверьте сетевую политику из execution network.

Проверка host firewall остаётся ручной проверкой развёртывания: `make test` и CI её не запускают, потому что она собирает policy-образ и меняет правила iptables/ip6tables хоста (нужны root-доступ к Docker, `CAP_NET_ADMIN` и host network). Выполните её на целевом хосте перед эксплуатацией и после изменения `deployment/registry/network-policy*` или host firewall. Тест использует уникальные временные bridges/chains и удаляет их после завершения:

```bash
cd backend
GOTOOLCHAIN=go1.26.10 CORE_TEST_NETWORK_POLICY=1 \
  go test -count=1 -timeout=3m -run TestBuildNetworkPolicy ./tests/integration/registry_isolation
```

## Go toolchain and build inputs

DCM pins Go **1.26.10**, a supported patch release on the 1.26 line; check the
[Go release policy and patch notes](https://go.dev/doc/devel/release) and
[official downloads](https://go.dev/dl/?mode=json) when updating. It fixes
[GO-2026-6809](https://pkg.go.dev/vuln/GO-2026-6809),
a `crypto/x509` denylist of compromised intermediate certificates. The production
scan reaches `Certificate.Verify` through `crypto/tls` handshakes, and every service
binary links that verifier for its HTTPS/TLS clients, so the advisory is patched
rather than accepted.

`backend/go.mod` sets the minimum to 1.26.10. Canonical Make targets export exactly
`GOTOOLCHAIN=go1.26.10`; the `go` directive alone is a minimum and does not prevent a
newer installed compiler from being selected. For direct commands, export
`GOTOOLCHAIN=go1.26.10` explicitly. Keep the module version and all four Dockerfile
builder pins synchronized when updating. Toolchain downloads use Go's checksum
database; do not disable checksum verification.

All four service Dockerfiles use these verified multi-platform image indexes:

| Input | Pinned reference |
| --- | --- |
| Compiler | `golang:1.26.10-alpine3.24@sha256:a2012b4a1d1f9f7e9f44e0d39b805a901e856b1dd80b4e6ee4338db547050308` |
| Runtime | `alpine:3.24@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6` |

Docker sets `GOTOOLCHAIN=local`, so module changes cannot silently download another
compiler. Module downloads are checked against committed `go.sum` and verified
with `go mod verify`. Local and Docker service builds use `CGO_ENABLED=0`,
`-mod=readonly -trimpath -buildvcs=false`. Core and Gateway additionally install
`ca-certificates` and `git`. APK verifies signed packages. Base images are pinned by
digest, but APK package versions are not: a rebuild installs the current packages
of the pinned Alpine branch, so it picks up OS patches and images are not
byte-repeatable.

The Registry deployment images under `deployment/registry/` use the same runtime
index and the same unpinned APK installs: `socat` (edge), `squid` (egress) and
`iptables` (network policy). Check `squid.conf` with `squid -k parse` after a base
image update. Compose and the Registry isolation test run the upstream
Distribution image as
`registry:2.8.3@sha256:a3d8aaa63ed8681a604f1dea0aa03f100d5895b6a58ace528858a7b332415373`.

The other third-party infrastructure images in `docker-compose.yml`,
`backend/docker-compose.test.yml` and `backend/docker-compose.keycloak.yml` are pinned
to exact release tags without digests: `postgres:17.10-alpine3.23`,
`migrate/migrate:v4.19.1`, `clickhouse/clickhouse-server:25.5.11.15`,
`rabbitmq:4.3.1-management`, `traefik:v3.6.20`, `quay.io/keycloak/keycloak:26.0.8`
and `pgsty/minio:RELEASE.2026-08-04T00-00-00Z`.
An exact tag stops silent minor or major upgrades, but a publisher can still
rebuild it, so these pulls are not byte-reproducible. Update a tag deliberately
and keep it identical across the Compose files.

Object storage uses `pgsty/minio`, a community MinIO fork, because the upstream
`minio/minio` image is no longer pullable from Docker Hub or Quay. It keeps the
upstream image's entrypoint, `minio` command, `MINIO_ROOT_*` variables, `/data`
volume and bundled `curl` health check, so the Compose service is unchanged. The
DCM storage client uses only basic S3 object and bucket calls, so it works with
other S3-compatible servers, but the Compose services are MinIO-specific: another
server needs its own command, credentials variables and health check in both
Compose files.

No automated check enforces these pins, and no CI job builds the service or Registry
images, so keep pins in review. `make vulncheck` scans Go binaries only, so
it does not apply to these Alpine images or the upstream Registry image. They
receive no OS-package CVE scan, which matches the backend runtime images.

From `backend/`, verify with:

```bash
make build
make test-fast
make vet
make test-race
make test
make vulncheck
```

`make vulncheck` installs **govulncheck v1.9.0** in a versioned ignored tools
folder, scans `./cmd/...` (production roots, excluding test-only roots), and scans
`bin/builder`, `bin/core`, `bin/gateway`, and `bin/sso` separately.
It runs all scans even if one reports findings, then applies the reviewed
applicability policy in `backend/vulncheck/policy.json`. Each accepted advisory
names the scans it covers, a review date, a rationale and evidence links. The
target fails on any symbol-level finding not accepted for that scan, on an
accepted entry a scan no longer reports, and on an incomplete entry.
Module/package-only reports are listed by govulncheck but do not fail the gate,
matching govulncheck's own exit semantics. Add an entry only after reviewing the
advisory against DCM's callers; prefer an upgrade when a fixed version exists.
The vulnerability database is live; the target prints its modification time,
and CI rescans weekly. Record the scan date, compiler, scanner version,
GOOS/GOARCH, and build settings when comparing results. The equivalent manual
scans are `CGO_ENABLED=0 govulncheck ./cmd/...` and
`govulncheck -mode=binary bin/<service>` after `make build`; compiler and settings
are inspectable with `go version -m`. Binary symbol inclusion is conservative evidence,
not proof that an untrusted request reaches a vulnerable operation. Third-party
advisories require separate dependency and transport-specific assessment.

### Standard-library applicability

- [GO-2026-4869](https://pkg.go.dev/vuln/GO-2026-4869): untrusted tar uploads reach
  Builder's `archive/tar.Reader.Next` through its extractor and storage adapter.
  The fix landed in Go 1.25.9 / 1.26.2 and is included in the pinned toolchain.
  This is a relevant parser boundary, without claiming a demonstrated DCM exploit.
- [GO-2026-6608](https://pkg.go.dev/vuln/GO-2026-6608): Core accepts build and
  Compose multipart uploads through `Request.MultipartReader` and `Reader.NextPart`,
  which parses MIME headers. The advisory describes an arbitrarily long line
  allocation when the remaining parser memory limit is below 400 bytes; fixed
  in Go 1.26.9 / 1.27.2. Upload limits alone do not establish non-applicability.
  The pinned toolchain includes the fix; a reachable parser is not proof of a
  successful attack against every HTTP service.
- The pinned toolchain also includes the TLS, X.509, HTTP, URL, MIME and textproto
  fixes listed in the Go release notes. Platform-specific and unused protocol
  reports must be assessed against Linux service builds and actual callers,
  without blanket exclusions.

### Network dependencies and compatibility

Network modules are pinned at these versions:

| Module | Version | Reason |
| --- | --- | --- |
| `google.golang.org/grpc` | 1.83.2 | HTTP/2 frame buffering/flood protections and missing-authority rejection |
| `golang.org/x/net` | 0.60.0 | HTTP/2 trailer, window, flow-control and HPACK fixes; earlier IDNA fixes |
| `github.com/rabbitmq/amqp091-go` | 1.15.0 | Frame/field bounds, SASL credential handling and reconnect corrections |
| `github.com/quic-go/quic-go` | 0.59.1 | HTTP/3 trailer validation security backport |

The gRPC and x/net module requirements also determine the x/crypto, x/sys, x/text,
x/sync, OpenTelemetry API/metric/trace and genproto RPC versions. Protobuf is
1.36.11, matching the pinned code generator. These modules support the pinned
Go 1.26.10 toolchain.

Review these upstream notes when updating:
[gRPC releases](https://github.com/grpc/grpc-go/releases),
[AMQP 1.13](https://github.com/rabbitmq/amqp091-go/releases/tag/v1.13.0),
[1.14](https://github.com/rabbitmq/amqp091-go/releases/tag/v1.14.0),
[1.15](https://github.com/rabbitmq/amqp091-go/releases/tag/v1.15.0), and
[QUIC 0.59.1](https://github.com/quic-go/quic-go/releases/tag/v0.59.1).

- DCM uses gRPC's native HTTP/2 client/server transport. Internal authentication
  does not eliminate pre-authentication frame-parser exposure. The pinned gRPC fixes
  [GO-2026-6061](https://pkg.go.dev/vuln/GO-2026-6061),
  [GO-2026-6348](https://pkg.go.dev/vuln/GO-2026-6348), and
  [GO-2026-6443](https://pkg.go.dev/vuln/GO-2026-6443). DCM does not configure xDS
  or its RBAC engine; those portions of the reports do not describe its typed
  scope/token authorization. Strict RPC paths remain enabled. Default write/read
  buffer pooling is exercised by client/server and race checks. TLS authority
  validation uses the leaf certificate; configure service names against that
  certificate. Unexpected upstream gRPC errors pass through DCM's safe transport
  error mapping.
- Go's standard library and x/net are patched separately. The pinned x/net
  fixes GO-2026-4918, GO-2026-5026, GO-2026-6603, GO-2026-6610,
  GO-2026-6611, GO-2026-6612 and GO-2026-6617. Assess HTTP/2 server and client
  exposure separately; an HTTP/1 deployment is not an exclusion for gRPC.
- AMQP parsing fixes apply to a malicious or compromised broker, not a direct
  unauthenticated HTTP request. DCM retains persistent JSON publications,
  confirms, durable quorum queues/DLX, manual ACK/NACK and `prefetch=1`.
  Short/long string bounds reject invalid encodings; DCM's queue names, routing
  keys and message fields are within them. URI-derived AMQPS has an explicit
  TLS 1.2 minimum; development Compose still uses AMQP. Version 1.15
  preserves caller SASL credentials while redacting the connection's copy.
  AMQP connections leave `Config.Recovery` nil: automatic recovery is disabled
  and DCM continues to own reconnect/topology handling. Core shares one publisher
  across outboxes. Each publication has a 10-second application budget covering
  cancelable acquisition, TCP/TLS/AMQP connection setup, topology declaration,
  writing and confirmation; an earlier caller deadline takes precedence.
  Cancellation closes the transport to interrupt synchronous AMQP I/O. Failed
  attempts discard their connection and confirmation stream, so late ACKs cannot
  confirm subsequent messages. Shutdown cancels active and queued publications
  before waiting for the publisher. Only a broker ACK permits marking an outbox
  record published; failures remain retryable and may redeliver a message whose
  ACK was lost.
- Gin imports HTTP/3 support, but DCM starts ordinary HTTP servers, proxies
  HTTP/SSE/WebSocket and uses native gRPC; it does not start a QUIC listener or
  use an HTTP/3 client transport. The pinned quic-go backport patches
  GO-2026-5676 regardless. Gorilla WebSocket 1.5.3 already contains the reported
  masking fix.

### Accepted and residual advisories

`make vulncheck` passes only because GO-2026-4883 and GO-2026-4887 are recorded as
reviewed acceptances in `backend/vulncheck/policy.json`, scoped to the scans that
report them: production roots and the Builder and Core binaries. Any
other symbol-level finding fails it. Gateway and SSO have no symbol-level findings;
their module-only reports still require applicability review.

Residual applicability is deliberately narrower than a blanket module exclusion:

- GO-2026-4883 concerns Docker daemon plugin-install privilege validation;
  GO-2026-4887 concerns daemon AuthZ plugins and oversized bodies. DCM links the
  Docker client/types, not the daemon plugin authorization handlers, and does
  not expose plugin installation. Module-wide unreviewed reports flag client
  symbols too. This excludes those server operations from DCM binaries, not the
  independently installed host daemon from assessment. No Docker SDK release
  carries a fix; patch the host Engine separately. See the upstream
  [plugin privilege advisory](https://github.com/moby/moby/security/advisories/GHSA-pxq6-2prw-chj9)
  and [AuthZ advisory](https://github.com/moby/moby/security/advisories/GHSA-x744-4wpc-v9h2).
- GO-2026-6253 concerns archive extraction and is fixed by `moby/go-archive`
  0.3.3. Builder directly calls only `go-archive.TarWithOptions` to create an
  uncompressed workspace tar; uploads use DCM's separate extractor, not
  `go-archive.Untar`. See the
  [extraction advisory](https://github.com/moby/go-archive/security/advisories/GHSA-hfg8-hc9c-6c3h).
  GO-2026-5841 (compress/s2 `NewDict`) is fixed by `klauspost/compress` 1.18.7.
- Module/package-only findings are not silently waived. Docker daemon archive-copy
  advisories GO-2026-5617/5668/5746 concern Engine code that is not in DCM's
  production graph. Builder still sends tar streams through the daemon's
  archive-copy API, so the host Engine must be 29.5.1 or newer (see the
  requirements in section 1); absence from DCM's call graph does not patch an older
  deployed Engine. GO-2026-5932 concerns x/crypto/openpgp, which is not imported
  by DCM's production graph; local password authentication uses bcrypt.
  These exclusions apply to Linux/amd64 service builds only; other platforms or
  new protocol/copy/authentication callers require reassessment.
